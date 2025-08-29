package cmd

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/feedlist"
	"github.com/spf13/cobra"
)

var (
	subscribeFormat   string
	subscribeFilename string
	subscribeDiscover bool
)

var subscribeCmd = &cobra.Command{
	Use:   "subscribe [URL]",
	Short: "Subscribe to a feed by adding it to a feed list",
	Long: `Subscribe to a feed by adding its URL to a feed list (OPML or text format).

If --discover is specified, the URL will be treated as a webpage and parsed for RSS/Atom autodiscovery links.

Examples:
  feedspool subscribe https://example.com/feed.xml
  feedspool subscribe --discover https://example.com/blog
  feedspool subscribe --format text --filename feeds.txt https://example.com/feed.xml`,
	Args: cobra.ExactArgs(1),
	RunE: runSubscribe,
}

func init() {
	subscribeCmd.Flags().StringVar(&subscribeFormat, "format", "", "Feed list format (opml or text)")
	subscribeCmd.Flags().StringVar(&subscribeFilename, "filename", "", "Feed list filename")
	subscribeCmd.Flags().BoolVar(&subscribeDiscover, "discover", false, "Discover RSS/Atom feeds from HTML page")
	rootCmd.AddCommand(subscribeCmd)
}

func runSubscribe(_ *cobra.Command, args []string) error {
	targetURL := args[0]
	cfg := GetConfig()

	format, filename, err := determineFormatAndFilename(cfg, subscribeFormat, subscribeFilename)
	if err != nil {
		return err
	}

	feedFormat, err := validateFormat(format)
	if err != nil {
		return err
	}

	urlsToAdd, err := getURLsToAdd(targetURL, subscribeDiscover)
	if err != nil {
		return err
	}

	if len(urlsToAdd) == 0 {
		return nil
	}

	list := loadOrCreateFeedList(feedFormat, filename)

	addedCount := addURLsToList(list, urlsToAdd)

	return saveFeedListIfNeeded(list, filename, addedCount)
}

func determineFormatAndFilename(
	cfg *config.Config, format, filename string,
) (resultFormat, resultFilename string, err error) {
	if format == "" || filename == "" {
		if cfg.HasDefaultFeedList() {
			if format == "" {
				format, _ = cfg.GetDefaultFeedList()
			}
			if filename == "" {
				_, filename = cfg.GetDefaultFeedList()
			}
		} else {
			return "", "", fmt.Errorf("feed list format and filename must be specified " +
				"(use --format and --filename flags or configure defaults)")
		}
	}
	return format, filename, nil
}

func validateFormat(format string) (feedlist.Format, error) {
	switch format {
	case string(feedlist.FormatOPML):
		return feedlist.FormatOPML, nil
	case string(feedlist.FormatText):
		return feedlist.FormatText, nil
	default:
		return "", fmt.Errorf("unsupported format: %s (must be 'opml' or 'text')", format)
	}
}

func getURLsToAdd(targetURL string, discover bool) ([]string, error) {
	if discover {
		discoveredURLs, err := discoverFeeds(targetURL)
		if err != nil {
			return nil, fmt.Errorf("failed to discover feeds from %s: %w", targetURL, err)
		}
		if len(discoveredURLs) == 0 {
			fmt.Printf("Warning: No RSS/Atom feeds discovered at %s\n", targetURL)
			return nil, nil
		}
		fmt.Printf("Discovered %d feed(s) from %s\n", len(discoveredURLs), targetURL)
		return discoveredURLs, nil
	}
	return []string{targetURL}, nil
}

func loadOrCreateFeedList(feedFormat feedlist.Format, filename string) feedlist.FeedList {
	list, err := feedlist.LoadFeedList(feedFormat, filename)
	if err != nil {
		list = feedlist.NewFeedList(feedFormat)
		fmt.Printf("Creating new feed list: %s\n", filename)
	} else {
		fmt.Printf("Loaded existing feed list: %s\n", filename)
	}
	return list
}

func addURLsToList(list feedlist.FeedList, urlsToAdd []string) int {
	addedCount := 0
	existingURLs := list.GetURLs()
	existingSet := make(map[string]bool)
	for _, url := range existingURLs {
		existingSet[url] = true
	}

	for _, feedURL := range urlsToAdd {
		if existingSet[feedURL] {
			fmt.Printf("Warning: Feed URL already exists in list: %s\n", feedURL)
		} else {
			if err := list.AddURL(feedURL); err != nil {
				fmt.Printf("Warning: Failed to add URL %s: %v\n", feedURL, err)
			} else {
				fmt.Printf("Added feed: %s\n", feedURL)
				addedCount++
			}
		}
	}
	return addedCount
}

func saveFeedListIfNeeded(list feedlist.FeedList, filename string, addedCount int) error {
	if addedCount > 0 {
		if err := list.Save(filename); err != nil {
			return fmt.Errorf("failed to save feed list: %w", err)
		}
		fmt.Printf("Saved %d new feed(s) to %s\n", addedCount, filename)
	} else {
		fmt.Println("No new feeds to add.")
	}
	return nil
}

// discoverFeeds parses HTML at the given URL for RSS/Atom autodiscovery links.
func discoverFeeds(htmlURL string) ([]string, error) {
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
	feeds := parseFeedLinks(string(body))

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

// parseFeedLinks extracts RSS/Atom feed URLs from HTML <link> tags.
func parseFeedLinks(html string) []string {
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
