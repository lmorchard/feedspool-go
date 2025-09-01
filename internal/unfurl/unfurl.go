package unfurl

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/httpclient"
	"github.com/otiai10/opengraph/v2"
	"golang.org/x/net/html"
)

const (
	descriptionMeta = "description"
)

// Unfurler handles metadata extraction from URLs.
type Unfurler struct {
	client        *httpclient.Client
	robotsChecker *RobotsChecker
}

// NewUnfurler creates a new unfurler with the given HTTP client.
func NewUnfurler(client *httpclient.Client) *Unfurler {
	if client == nil {
		client = httpclient.NewClient(&httpclient.Config{
			UserAgent:       httpclient.DefaultUserAgent,
			Timeout:         httpclient.DefaultTimeout,
			MaxResponseSize: httpclient.MaxResponseSize,
		})
	}
	return &Unfurler{
		client:        client,
		robotsChecker: NewRobotsChecker(client, "feedspool"),
	}
}

// Result contains the extracted metadata from a URL.
type Result struct {
	Title       string                 `json:"title,omitempty"`
	Description string                 `json:"description,omitempty"`
	ImageURL    string                 `json:"image_url,omitempty"`
	FaviconURL  string                 `json:"favicon_url,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Unfurl fetches and extracts metadata from a URL.
func (u *Unfurler) Unfurl(targetURL string) (*Result, error) { //nolint:cyclop // Complex metadata extraction logic
	// Parse URL to ensure it's valid
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	// Check robots.txt
	allowed, err := u.robotsChecker.IsAllowed(targetURL)
	if err == nil && !allowed {
		return nil, fmt.Errorf("robots.txt disallows fetching this URL")
	}
	// If robots.txt check fails, we proceed with the fetch

	// Fetch the page with size limit
	resp, err := u.client.GetLimited(targetURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Read the limited response body
	body, err := io.ReadAll(resp.BodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse OpenGraph metadata
	og := opengraph.New(targetURL)
	if err := og.Parse(bytes.NewReader(body)); err != nil {
		// OpenGraph parsing failed, but we can still try to extract other metadata
		og = nil
	}

	// Parse HTML for additional metadata
	htmlMeta := u.parseHTMLMetadata(bytes.NewReader(body), parsedURL)

	// Combine results
	result := &Result{
		Metadata: make(map[string]interface{}),
	}

	// Prefer OpenGraph data when available
	if og != nil && og.Title != "" {
		result.Title = og.Title
	} else if htmlMeta.Title != "" {
		result.Title = htmlMeta.Title
	}

	if og != nil && og.Description != "" {
		result.Description = og.Description
	} else if htmlMeta.Description != "" {
		result.Description = htmlMeta.Description
	}

	// Handle images
	if og != nil && len(og.Image) > 0 && og.Image[0].URL != "" {
		result.ImageURL = u.makeAbsoluteURL(og.Image[0].URL, parsedURL)
	} else if htmlMeta.ImageURL != "" {
		result.ImageURL = htmlMeta.ImageURL
	}

	// Get favicon
	if htmlMeta.FaviconURL != "" {
		result.FaviconURL = htmlMeta.FaviconURL
	} else {
		// Try default favicon.ico location
		result.FaviconURL = fmt.Sprintf("%s://%s/favicon.ico", parsedURL.Scheme, parsedURL.Host)
	}

	// Store OpenGraph data in metadata
	if og != nil { //nolint:nestif
		if og.Type != "" {
			result.Metadata["og:type"] = og.Type
		}
		if og.URL != "" {
			result.Metadata["og:url"] = og.URL
		}
		if og.SiteName != "" {
			result.Metadata["og:site_name"] = og.SiteName
		}

		// Store image metadata if available
		if len(og.Image) > 0 {
			imageData := make([]map[string]interface{}, 0, len(og.Image))
			for _, img := range og.Image {
				imgMap := make(map[string]interface{})
				if img.URL != "" {
					imgMap["url"] = img.URL
				}
				if img.Width > 0 {
					imgMap["width"] = img.Width
				}
				if img.Height > 0 {
					imgMap["height"] = img.Height
				}
				if img.Alt != "" {
					imgMap["alt"] = img.Alt
				}
				imageData = append(imageData, imgMap)
			}
			result.Metadata["og:images"] = imageData
		}
	}

	return result, nil
}

// htmlMetadata holds metadata extracted from HTML.
type htmlMetadata struct {
	Title       string
	Description string
	ImageURL    string
	FaviconURL  string
}

// parseHTMLMetadata extracts metadata from HTML document.
func (u *Unfurler) parseHTMLMetadata(r io.Reader, baseURL *url.URL) *htmlMetadata { //nolint:cyclop
	meta := &htmlMetadata{}
	doc, err := html.Parse(r)
	if err != nil {
		return meta
	}

	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode { //nolint:nestif
			switch n.Data {
			case "title":
				if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
					meta.Title = strings.TrimSpace(n.FirstChild.Data)
				}
			case "meta":
				var name, property, content string
				for _, attr := range n.Attr {
					switch attr.Key {
					case "name":
						name = strings.ToLower(attr.Val)
					case "property":
						property = strings.ToLower(attr.Val)
					case "content":
						content = attr.Val
					}
				}
				if content != "" {
					if name == descriptionMeta || property == descriptionMeta {
						meta.Description = content
					}
					// Twitter Card fallbacks
					if property == "twitter:image" && meta.ImageURL == "" {
						meta.ImageURL = u.makeAbsoluteURL(content, baseURL)
					}
				}
			case "link":
				var rel, href string
				for _, attr := range n.Attr {
					switch attr.Key {
					case "rel":
						rel = strings.ToLower(attr.Val)
					case "href":
						href = attr.Val
					}
				}
				if href != "" {
					// Check for various favicon types
					if strings.Contains(rel, "icon") || rel == "shortcut icon" || rel == "apple-touch-icon" {
						meta.FaviconURL = u.makeAbsoluteURL(href, baseURL)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}
	traverse(doc)

	return meta
}

// makeAbsoluteURL converts a potentially relative URL to absolute.
func (u *Unfurler) makeAbsoluteURL(href string, base *url.URL) string {
	parsed, err := url.Parse(href)
	if err != nil {
		return href
	}
	return base.ResolveReference(parsed).String()
}

// ToURLMetadata converts unfurl result to database URLMetadata model.
func (u *Unfurler) ToURLMetadata(targetURL string, result *Result, statusCode int,
	fetchError error,
) (*database.URLMetadata, error) {
	metadata := &database.URLMetadata{
		URL:         targetURL,
		LastFetchAt: sql.NullTime{Time: time.Now(), Valid: true},
	}

	if result != nil { //nolint:nestif
		if result.Title != "" {
			metadata.Title = sql.NullString{String: result.Title, Valid: true}
		}
		if result.Description != "" {
			metadata.Description = sql.NullString{String: result.Description, Valid: true}
		}
		if result.ImageURL != "" {
			metadata.ImageURL = sql.NullString{String: result.ImageURL, Valid: true}
		}
		if result.FaviconURL != "" {
			metadata.FaviconURL = sql.NullString{String: result.FaviconURL, Valid: true}
		}

		// Store additional metadata as JSON
		if len(result.Metadata) > 0 {
			metaJSON, err := json.Marshal(result.Metadata)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal metadata: %w", err)
			}
			metadata.Metadata = database.JSON(metaJSON)
		}
	}

	if statusCode > 0 {
		metadata.FetchStatusCode = sql.NullInt64{Int64: int64(statusCode), Valid: true}
	}

	if fetchError != nil {
		metadata.FetchError = sql.NullString{String: fetchError.Error(), Valid: true}
	}

	return metadata, nil
}
