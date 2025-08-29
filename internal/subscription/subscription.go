package subscription

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/feedlist"
	"github.com/sirupsen/logrus"
)

// Manager handles feed subscription operations.
type Manager struct {
	config *config.Config
}

// New creates a new subscription manager.
func New(cfg *config.Config) *Manager {
	return &Manager{config: cfg}
}

// ResolveFormatAndFilename determines the format and filename to use, applying defaults if needed.
func (m *Manager) ResolveFormatAndFilename(format, filename string) (resultFormat, resultFilename string, err error) {
	resultFormat, resultFilename = format, filename

	if resultFormat == "" || resultFilename == "" {
		if !m.config.HasDefaultFeedList() {
			return "", "", fmt.Errorf("feed list format and filename must be specified " +
				"(use --format and --filename flags or configure defaults)")
		}

		if resultFormat == "" {
			resultFormat, _ = m.config.GetDefaultFeedList()
		}
		if resultFilename == "" {
			_, resultFilename = m.config.GetDefaultFeedList()
		}
	}
	return resultFormat, resultFilename, nil
}

// ValidateFormat validates and converts string format to feedlist.Format.
func (m *Manager) ValidateFormat(format string) (feedlist.Format, error) {
	switch format {
	case string(feedlist.FormatOPML):
		return feedlist.FormatOPML, nil
	case string(feedlist.FormatText):
		return feedlist.FormatText, nil
	default:
		return "", fmt.Errorf("unsupported format: %s (must be 'opml' or 'text')", format)
	}
}

// LoadOrCreateFeedList loads an existing feed list or creates a new one if it doesn't exist.
func (m *Manager) LoadOrCreateFeedList(feedFormat feedlist.Format, filename string) (feedlist.FeedList, bool) {
	list, err := feedlist.LoadFeedList(feedFormat, filename)
	if err != nil {
		list = feedlist.NewFeedList(feedFormat)
		logrus.Debugf("Creating new feed list: %s", filename)
		return list, true // true = newly created
	}
	logrus.Debugf("Loaded existing feed list: %s", filename)
	return list, false // false = loaded existing
}

// SubscribeResult contains the results of a subscription operation.
type SubscribeResult struct {
	CreatedNew bool
	AddedCount int
	TotalURLs  int
	Warnings   []string
}

// Subscribe adds one or more URLs to a feed list.
func (m *Manager) Subscribe(format, filename string, urls []string) (*SubscribeResult, error) {
	feedFormat, err := m.ValidateFormat(format)
	if err != nil {
		return nil, err
	}

	list, createdNew := m.LoadOrCreateFeedList(feedFormat, filename)
	addedCount, warnings := m.addURLsToList(list, urls)

	result := &SubscribeResult{
		CreatedNew: createdNew,
		AddedCount: addedCount,
		TotalURLs:  len(urls),
		Warnings:   warnings,
	}

	if addedCount > 0 {
		if err := list.Save(filename); err != nil {
			return result, fmt.Errorf("failed to save feed list: %w", err)
		}
	}

	return result, nil
}

// UnsubscribeResult contains the results of an unsubscribe operation.
type UnsubscribeResult struct {
	Found   bool
	Removed bool
}

// Unsubscribe removes a URL from a feed list.
func (m *Manager) Unsubscribe(format, filename, targetURL string) (*UnsubscribeResult, error) {
	if _, err := url.Parse(targetURL); err != nil {
		return nil, fmt.Errorf("invalid URL: %s - %w", targetURL, err)
	}

	feedFormat, err := m.ValidateFormat(format)
	if err != nil {
		return nil, err
	}

	list, err := feedlist.LoadFeedList(feedFormat, filename)
	if err != nil {
		return nil, fmt.Errorf("failed to load feed list %s: %w", filename, err)
	}

	logrus.Debugf("Loaded feed list: %s", filename)

	// Check if URL exists
	existingURLs := list.GetURLs()
	exists := false
	for _, existing := range existingURLs {
		if existing == targetURL {
			exists = true
			break
		}
	}

	result := &UnsubscribeResult{Found: exists}

	if !exists {
		return result, nil
	}

	if err := list.RemoveURL(targetURL); err != nil {
		return result, fmt.Errorf("failed to remove URL %s: %w", targetURL, err)
	}

	if err := list.Save(filename); err != nil {
		return result, fmt.Errorf("failed to save feed list: %w", err)
	}

	result.Removed = true
	return result, nil
}

// DiscoverFeeds performs RSS/Atom autodiscovery on an HTML page.
func (m *Manager) DiscoverFeeds(htmlURL string) ([]string, error) {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Fetch the HTML page
	resp, err := client.Get(htmlURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch HTML page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %d %s", resp.StatusCode, resp.Status)
	}

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse for feed links
	feeds := m.parseFeedLinks(string(body))

	// Resolve relative URLs
	baseURL, err := url.Parse(htmlURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	resolvedFeeds := make([]string, 0, len(feeds))
	for _, feed := range feeds {
		resolvedURL, err := baseURL.Parse(feed)
		if err != nil {
			// Skip invalid URLs
			continue
		}
		resolvedFeeds = append(resolvedFeeds, resolvedURL.String())
	}

	return resolvedFeeds, nil
}

func (m *Manager) addURLsToList(list feedlist.FeedList, urlsToAdd []string) (addedCount int, warnings []string) {
	existingURLs := list.GetURLs()
	existingSet := make(map[string]bool)
	for _, url := range existingURLs {
		existingSet[url] = true
	}

	for _, feedURL := range urlsToAdd {
		if existingSet[feedURL] {
			warnings = append(warnings, fmt.Sprintf("Feed URL already exists in list: %s", feedURL))
		} else {
			if err := list.AddURL(feedURL); err != nil {
				warnings = append(warnings, fmt.Sprintf("Failed to add URL %s: %v", feedURL, err))
			} else {
				logrus.Debugf("Added feed: %s", feedURL)
				addedCount++
			}
		}
	}
	return addedCount, warnings
}

// parseFeedLinks extracts RSS/Atom feed URLs from HTML <link> tags.
func (m *Manager) parseFeedLinks(html string) []string {
	// Regular expression to match <link> tags with RSS/Atom types
	linkRegex := regexp.MustCompile(`(?i)<link[^>]*?(?:type\s*=\s*["'](?:application/rss\+xml|` +
		`application/atom\+xml)["'][^>]*?href\s*=\s*["']([^"']+)["']|href\s*=\s*["']([^"']+)["']` +
		`[^>]*?type\s*=\s*["'](?:application/rss\+xml|application/atom\+xml)["'])[^>]*?>`)

	matches := linkRegex.FindAllStringSubmatch(html, -1)

	var feeds []string
	seen := make(map[string]bool)

	for _, match := range matches {
		// The href can be in either capture group 1 or 2 depending on attribute order
		href := match[1]
		if href == "" {
			href = match[2]
		}

		if href != "" && !seen[href] {
			feeds = append(feeds, href)
			seen[href] = true
		}
	}

	return feeds
}
