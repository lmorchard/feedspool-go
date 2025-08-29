package cmd

import (
	"fmt"

	"github.com/lmorchard/feedspool-go/internal/subscription"
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

	manager := subscription.New(cfg)

	format, filename, err := manager.ResolveFormatAndFilename(subscribeFormat, subscribeFilename)
	if err != nil {
		return err
	}

	urlsToAdd, err := getURLsToAdd(manager, targetURL, subscribeDiscover)
	if err != nil {
		return err
	}

	if len(urlsToAdd) == 0 {
		return nil
	}

	result, err := manager.Subscribe(format, filename, urlsToAdd)
	if err != nil {
		return err
	}

	// Handle output based on result
	if result.CreatedNew {
		fmt.Printf("Creating new feed list: %s\n", filename)
	} else {
		fmt.Printf("Loaded existing feed list: %s\n", filename)
	}

	// Print warnings
	for _, warning := range result.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}

	// Print success messages
	if result.AddedCount > 0 {
		fmt.Printf("Added %d new feed(s)\n", result.AddedCount)
		fmt.Printf("Saved %d new feed(s) to %s\n", result.AddedCount, filename)
	} else {
		fmt.Println("No new feeds to add.")
	}

	return nil
}

func getURLsToAdd(manager *subscription.Manager, targetURL string, discover bool) ([]string, error) {
	if discover {
		discoveredURLs, err := manager.DiscoverFeeds(targetURL)
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
