package feedlist

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lmorchard/feedspool-go/internal/opml"
	"github.com/lmorchard/feedspool-go/internal/textlist"
)

// Format represents the supported feed list formats.
type Format string

const (
	FormatOPML Format = "opml"
	FormatText Format = "text"

	FeedTypeRSS    = "rss"
	FeedTypeScrape = "scrape"
)

// Feed describes a feed and the configuration carried by its list entry.
type Feed struct {
	URL            string
	UserAgent      string
	Type           string
	ScrapeSelector string
}

// DeduplicateFeeds preserves first-seen order and rejects conflicting configuration.
func DeduplicateFeeds(feeds []Feed) ([]Feed, error) {
	seen := make(map[string]Feed, len(feeds))
	unique := make([]Feed, 0, len(feeds))
	for _, rawFeed := range feeds {
		feed := normalizeFeed(rawFeed)
		existing, found := seen[feed.URL]
		if !found {
			seen[feed.URL] = feed
			unique = append(unique, feed)
			continue
		}
		if existing.UserAgent != feed.UserAgent {
			return nil, fmt.Errorf("conflicting User-Agent values for feed %s", feed.URL)
		}
		if existing.Type != feed.Type || existing.ScrapeSelector != feed.ScrapeSelector {
			return nil, fmt.Errorf("conflicting parser configuration for feed %s", feed.URL)
		}
	}
	return unique, nil
}

// String returns the string representation of the format.
func (f Format) String() string {
	return string(f)
}

// FeedList interface provides unified access to different feed list formats.
type FeedList interface {
	GetFeeds() []Feed
	GetURLs() []string
	AddFeed(feed Feed) error
	SetUserAgent(url, userAgent string) bool
	SetFeedType(url, feedType, scrapeSelector string) bool
	Title() string
	AddURL(url string) error
	RemoveURL(url string) error
	Save(filename string) error
}

// OPMLFeedList wraps OPML functionality.
type OPMLFeedList struct {
	opml *opml.OPML
}

// TextFeedList uses the text parser.
type TextFeedList struct {
	urls []string
}

// LoadFeedList loads a feed list from a file based on the specified format.
func LoadFeedList(format Format, filename string) (FeedList, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open feed list file %s: %w", filename, err)
	}
	defer file.Close()

	switch format {
	case FormatOPML:
		return loadOPMLFeedList(file)
	case FormatText:
		return loadTextFeedList(file)
	default:
		return nil, fmt.Errorf("unsupported feed list format: %s", format)
	}
}

// NewFeedList creates a new empty feed list of the specified format.
func NewFeedList(format Format) FeedList {
	switch format {
	case FormatOPML:
		return &OPMLFeedList{
			opml: &opml.OPML{
				Version: "2.0",
				Head:    opml.Head{Title: "Feed List"},
				Body:    opml.Body{Outlines: []opml.Outline{}},
			},
		}
	case FormatText:
		return &TextFeedList{
			urls: []string{},
		}
	default:
		// Default to text format if invalid format provided
		return &TextFeedList{
			urls: []string{},
		}
	}
}

// DetectFormat attempts to detect the format based on file extension.
func DetectFormat(filename string) Format {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".opml", ".xml":
		return FormatOPML
	case ".txt", ".text":
		return FormatText
	default:
		// Default to text format
		return FormatText
	}
}

// loadOPMLFeedList loads an OPML feed list from a reader.
func loadOPMLFeedList(reader io.Reader) (FeedList, error) {
	opmlData, err := opml.ParseOPML(reader)
	if err != nil {
		// opml.ParseOPML already returns a "failed to parse OPML: ..." message;
		// wrapping it again here would double it up.
		return nil, err
	}

	return &OPMLFeedList{
		opml: opmlData,
	}, nil
}

// loadTextFeedList loads a text feed list from a reader.
func loadTextFeedList(reader io.Reader) (FeedList, error) {
	urls, err := textlist.ParseTextList(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse text list: %w", err)
	}

	return &TextFeedList{
		urls: urls,
	}, nil
}

// OPMLFeedList methods.

// GetFeeds returns feed URLs and their OPML configuration.
func (ofl *OPMLFeedList) GetFeeds() []Feed {
	feeds := []Feed{}
	extractOPMLFeeds(ofl.opml.Body.Outlines, &feeds)
	return feeds
}

// GetURLs returns all URLs in the OPML feed list.
func (ofl *OPMLFeedList) GetURLs() []string {
	feeds := ofl.GetFeeds()
	urls := make([]string, 0, len(feeds))
	for _, feed := range feeds {
		urls = append(urls, feed.URL)
	}
	return urls
}

// Title returns the OPML head title, or an empty string if it is unset.
func (ofl *OPMLFeedList) Title() string {
	return strings.TrimSpace(ofl.opml.Head.Title)
}

// AddURL adds a URL to the OPML feed list.
func (ofl *OPMLFeedList) AddURL(url string) error {
	return ofl.AddFeed(Feed{URL: url})
}

// AddFeed adds a configured feed to the OPML feed list.
func (ofl *OPMLFeedList) AddFeed(feed Feed) error {
	feed = normalizeFeed(feed)
	// Check if URL already exists
	for _, existingFeed := range ofl.GetFeeds() {
		if existingFeed.URL == feed.URL {
			return nil // URL already exists, no error
		}
	}

	// Add to OPML structure
	outline := opml.Outline{
		Text:      feed.URL,
		Title:     feed.URL,
		Type:      feed.Type,
		XMLURL:    feed.URL,
		UserAgent: feed.UserAgent,
		Selector:  feed.ScrapeSelector,
	}
	ofl.opml.Body.Outlines = append(ofl.opml.Body.Outlines, outline)

	return nil
}

// SetUserAgent updates the User-Agent for an existing OPML feed entry.
func (ofl *OPMLFeedList) SetUserAgent(url, userAgent string) bool {
	return setOPMLUserAgent(ofl.opml.Body.Outlines, url, userAgent)
}

// SetFeedType updates parser configuration for an existing OPML feed entry.
func (ofl *OPMLFeedList) SetFeedType(url, feedType, scrapeSelector string) bool {
	feed := normalizeFeed(Feed{Type: feedType, ScrapeSelector: scrapeSelector})
	return setOPMLFeedType(ofl.opml.Body.Outlines, url, feed.Type, feed.ScrapeSelector)
}

// RemoveURL removes a URL from the OPML feed list.
func (ofl *OPMLFeedList) RemoveURL(url string) error {
	ofl.opml.Body.Outlines = removeOPMLFeed(ofl.opml.Body.Outlines, url)

	return nil
}

// Save saves the OPML feed list to a file.
func (ofl *OPMLFeedList) Save(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create OPML file %s: %w", filename, err)
	}
	defer file.Close()

	data, err := xml.MarshalIndent(ofl.opml, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to encode OPML: %w", err)
	}
	if _, err := file.WriteString(xml.Header); err != nil {
		return fmt.Errorf("failed to write OPML header: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("failed to write OPML: %w", err)
	}
	if _, err := file.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to finish OPML: %w", err)
	}

	return nil
}

// TextFeedList methods.

// GetFeeds returns URL-only entries for a text feed list.
func (tfl *TextFeedList) GetFeeds() []Feed {
	feeds := make([]Feed, 0, len(tfl.urls))
	for _, url := range tfl.urls {
		feeds = append(feeds, Feed{URL: url, Type: FeedTypeRSS})
	}
	return feeds
}

// GetURLs returns all URLs in the text feed list.
func (tfl *TextFeedList) GetURLs() []string {
	return tfl.urls
}

// Title returns an empty string; text feed lists carry no title.
func (tfl *TextFeedList) Title() string {
	return ""
}

// AddURL adds a URL to the text feed list.
func (tfl *TextFeedList) AddURL(url string) error {
	return tfl.AddFeed(Feed{URL: url})
}

// AddFeed adds a URL-only feed to a text feed list.
func (tfl *TextFeedList) AddFeed(feed Feed) error {
	// Check if URL already exists
	for _, existingURL := range tfl.urls {
		if existingURL == feed.URL {
			return nil // URL already exists, no error
		}
	}

	tfl.urls = append(tfl.urls, feed.URL)
	return nil
}

// SetUserAgent reports false because text feed lists carry URLs only.
func (tfl *TextFeedList) SetUserAgent(_, _ string) bool {
	return false
}

// SetFeedType reports false because text feed lists carry URLs only.
func (tfl *TextFeedList) SetFeedType(_, _, _ string) bool {
	return false
}

func extractOPMLFeeds(outlines []opml.Outline, feeds *[]Feed) {
	for i := range outlines {
		outline := &outlines[i]
		if outline.XMLURL != "" {
			*feeds = append(*feeds, normalizeFeed(Feed{
				URL:            outline.XMLURL,
				UserAgent:      outline.UserAgent,
				Type:           outline.Type,
				ScrapeSelector: outline.Selector,
			}))
		}
		extractOPMLFeeds(outline.Outlines, feeds)
	}
}

func setOPMLFeedType(outlines []opml.Outline, url, feedType, scrapeSelector string) bool {
	found := false
	for i := range outlines {
		if outlines[i].XMLURL == url {
			outlines[i].Type = feedType
			outlines[i].Selector = scrapeSelector
			found = true
		}
		if setOPMLFeedType(outlines[i].Outlines, url, feedType, scrapeSelector) {
			found = true
		}
	}
	return found
}

func normalizeFeed(feed Feed) Feed {
	if strings.EqualFold(strings.TrimSpace(feed.Type), FeedTypeScrape) {
		feed.Type = FeedTypeScrape
		feed.ScrapeSelector = strings.TrimSpace(feed.ScrapeSelector)
		return feed
	}
	feed.Type = FeedTypeRSS
	feed.ScrapeSelector = ""
	return feed
}

func setOPMLUserAgent(outlines []opml.Outline, url, userAgent string) bool {
	found := false
	for i := range outlines {
		if outlines[i].XMLURL == url {
			outlines[i].UserAgent = userAgent
			found = true
		}
		if setOPMLUserAgent(outlines[i].Outlines, url, userAgent) {
			found = true
		}
	}
	return found
}

func removeOPMLFeed(outlines []opml.Outline, url string) []opml.Outline {
	kept := make([]opml.Outline, 0, len(outlines))
	for i := range outlines {
		outline := &outlines[i]
		if outline.XMLURL == url {
			continue
		}
		outline.Outlines = removeOPMLFeed(outline.Outlines, url)
		kept = append(kept, *outline)
	}
	return kept
}

// RemoveURL removes a URL from the text feed list.
func (tfl *TextFeedList) RemoveURL(url string) error {
	newURLs := make([]string, 0)
	for _, existingURL := range tfl.urls {
		if existingURL != url {
			newURLs = append(newURLs, existingURL)
		}
	}
	tfl.urls = newURLs
	return nil
}

// Save saves the text feed list to a file.
func (tfl *TextFeedList) Save(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create text file %s: %w", filename, err)
	}
	defer file.Close()

	return textlist.WriteTextList(file, tfl.urls)
}
