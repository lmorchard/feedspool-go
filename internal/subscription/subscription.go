package subscription

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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

// LoadOrCreateFeedList loads an existing feed list or creates one when the file is absent.
func (m *Manager) LoadOrCreateFeedList(
	feedFormat feedlist.Format, filename string,
) (feedlist.FeedList, bool, error) {
	list, err := feedlist.LoadFeedList(feedFormat, filename)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, false, err
		}
		list = feedlist.NewFeedList(feedFormat)
		logrus.Debugf("Creating new feed list: %s", filename)
		return list, true, nil
	}
	logrus.Debugf("Loaded existing feed list: %s", filename)
	return list, false, nil
}

// SubscribeResult contains the results of a subscription operation.
type SubscribeResult struct {
	CreatedNew   bool
	AddedCount   int
	UpdatedCount int
	TotalURLs    int
	Warnings     []string
}

// SubscribeOptions controls metadata applied to subscribed feeds.
type SubscribeOptions struct {
	// UserAgent is nil when the option was not supplied. A non-nil empty
	// string explicitly clears an existing override.
	UserAgent *string
}

// Subscribe adds one or more URLs to a feed list.
func (m *Manager) Subscribe(format, filename string, urls []string) (*SubscribeResult, error) {
	return m.SubscribeWithOptions(format, filename, urls, SubscribeOptions{})
}

// SubscribeWithOptions adds or updates feeds and their list-backed configuration.
func (m *Manager) SubscribeWithOptions(
	format, filename string, urls []string, options SubscribeOptions,
) (*SubscribeResult, error) {
	feedFormat, err := m.ValidateFormat(format)
	if err != nil {
		return nil, err
	}
	if options.UserAgent != nil && feedFormat != feedlist.FormatOPML {
		return nil, fmt.Errorf("per-feed User-Agent is supported only for OPML feed lists")
	}

	list, createdNew, err := m.LoadOrCreateFeedList(feedFormat, filename)
	if err != nil {
		return nil, fmt.Errorf("failed to load feed list %s: %w", filename, err)
	}
	addedCount, updatedCount, warnings := m.addFeedsToList(list, urls, options)

	result := &SubscribeResult{
		CreatedNew:   createdNew,
		AddedCount:   addedCount,
		UpdatedCount: updatedCount,
		TotalURLs:    len(urls),
		Warnings:     warnings,
	}

	if addedCount > 0 || updatedCount > 0 {
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
	addedCount, _, warnings = m.addFeedsToList(list, urlsToAdd, SubscribeOptions{})
	return addedCount, warnings
}

func (m *Manager) addFeedsToList(
	list feedlist.FeedList, urlsToAdd []string, options SubscribeOptions,
) (addedCount, updatedCount int, warnings []string) {
	existingFeeds := make(map[string][]feedlist.Feed)
	for _, feed := range list.GetFeeds() {
		existingFeeds[feed.URL] = append(existingFeeds[feed.URL], feed)
	}

	for _, feedURL := range urlsToAdd {
		matches, exists := existingFeeds[feedURL]
		if exists && (options.UserAgent == nil || allFeedsUseUserAgent(matches, *options.UserAgent)) {
			warnings = append(warnings, fmt.Sprintf("Feed URL already exists in list: %s", feedURL))
			continue
		}
		if exists {
			if list.SetUserAgent(feedURL, *options.UserAgent) {
				updatedCount++
				for i := range matches {
					matches[i].UserAgent = *options.UserAgent
				}
				existingFeeds[feedURL] = matches
			}
			continue
		}

		feed := feedlist.Feed{URL: feedURL}
		if options.UserAgent != nil {
			feed.UserAgent = *options.UserAgent
		}
		if err := list.AddFeed(feed); err != nil {
			warnings = append(warnings, fmt.Sprintf("Failed to add URL %s: %v", feedURL, err))
			continue
		}
		logrus.Debugf("Added feed: %s", feedURL)
		addedCount++
		existingFeeds[feedURL] = []feedlist.Feed{feed}
	}
	return addedCount, updatedCount, warnings
}

func allFeedsUseUserAgent(feeds []feedlist.Feed, userAgent string) bool {
	for _, feed := range feeds {
		if feed.UserAgent != userAgent {
			return false
		}
	}
	return true
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
