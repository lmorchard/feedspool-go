package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/feedlist"
	"github.com/lmorchard/feedspool-go/internal/fetcher"
	"github.com/spf13/cobra"
)

var (
	fetchTimeout       time.Duration
	fetchMaxItems      int
	fetchForce         bool
	fetchConcurrency   int
	fetchMaxAge        time.Duration
	fetchRemoveMissing bool
	fetchFormat        string
	fetchFilename      string
)

var fetchCmd = &cobra.Command{
	Use:   "fetch [URL]",
	Short: "Fetch feeds from URL, file, or database",
	Long: `Fetch command operates in multiple modes:

Single URL:
  feedspool fetch <url>         # Fetch one feed from URL

From file:  
  feedspool fetch --format opml --filename feeds.opml    # Fetch all feeds in OPML file
  feedspool fetch --format text --filename feeds.txt     # Fetch all feeds in text file

From database:
  feedspool fetch                                         # Fetch all feeds in database

The command supports all options from the former 'update' command including concurrency
control, age filtering, and database cleanup based on feed lists.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runFetch,
}

func init() {
	fetchCmd.Flags().DurationVar(&fetchTimeout, "timeout", 30*time.Second, "Feed fetch timeout")
	fetchCmd.Flags().IntVar(&fetchMaxItems, "max-items", 100, "Maximum items to keep per feed")
	fetchCmd.Flags().BoolVar(&fetchForce, "force", false, "Ignore cache headers and fetch anyway")
	fetchCmd.Flags().IntVar(&fetchConcurrency, "concurrency", 32, "Maximum concurrent fetches")
	fetchCmd.Flags().DurationVar(&fetchMaxAge, "max-age", 0, "Skip feeds fetched within this duration")
	fetchCmd.Flags().BoolVar(&fetchRemoveMissing, "remove-missing", false, "Delete feeds not in list (file mode only)")
	fetchCmd.Flags().StringVar(&fetchFormat, "format", "", "Feed list format (opml or text)")
	fetchCmd.Flags().StringVar(&fetchFilename, "filename", "", "Feed list filename")
	rootCmd.AddCommand(fetchCmd)
}

func runFetch(_ *cobra.Command, args []string) error {
	cfg := GetConfig()

	if err := database.Connect(cfg.Database); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer database.Close()

	if err := database.IsInitialized(); err != nil {
		return err
	}

	// Determine fetch mode
	if len(args) == 1 {
		// Single URL mode
		return runSingleURLFetch(cfg, args[0])
	}

	if fetchFormat != "" || fetchFilename != "" {
		// File mode
		return runFileFetch(cfg)
	}

	// Database mode (no args, no file flags)
	return runDatabaseFetch(cfg)
}

func runSingleURLFetch(cfg *config.Config, feedURL string) error {
	fetcher := fetcher.NewFetcher(fetchTimeout, fetchMaxItems, fetchForce)
	result := fetcher.FetchFeed(feedURL)

	if result.Error != nil {
		return fmt.Errorf("failed to fetch feed: %w", result.Error)
	}

	if cfg.JSON {
		output := map[string]interface{}{
			"mode":   "single",
			"url":    feedURL,
			"cached": result.Cached,
			"items":  result.ItemCount,
		}
		if result.Feed != nil {
			output["title"] = result.Feed.Title
			output["description"] = result.Feed.Description
		}
		jsonData, _ := json.Marshal(output)
		fmt.Println(string(jsonData))
	} else {
		if result.Cached {
			fmt.Printf("Feed not modified: %s\n", feedURL)
		} else if result.Feed != nil {
			fmt.Printf("Feed fetched successfully: %s\n", result.Feed.Title)
			fmt.Printf("  URL: %s\n", feedURL)
			fmt.Printf("  Items: %d\n", result.ItemCount)
		}
	}

	return nil
}

func runFileFetch(cfg *config.Config) error {
	format, filename, err := determineFetchFormatAndFilename(cfg, fetchFormat, fetchFilename)
	if err != nil {
		return err
	}

	feedFormat, err := validateFetchFormat(format)
	if err != nil {
		return err
	}

	// Load feed URLs from file
	list, err := feedlist.LoadFeedList(feedFormat, filename)
	if err != nil {
		return fmt.Errorf("failed to load feed list %s: %w", filename, err)
	}

	feedURLs := list.GetURLs()
	if len(feedURLs) == 0 {
		fmt.Printf("No feed URLs found in %s\n", filename)
		return nil
	}

	fmt.Printf("Found %d feeds in %s\n", len(feedURLs), filename)

	// Fetch feeds concurrently
	results := fetcher.FetchConcurrent(
		feedURLs,
		fetchConcurrency,
		fetchTimeout,
		fetchMaxItems,
		fetchMaxAge,
		fetchForce,
	)

	successCount, errorCount, cachedCount, totalItems := processFetchResults(results)

	// Remove missing feeds if requested
	if fetchRemoveMissing {
		removedCount := removeMissingFeedsFromFile(feedURLs)
		if !cfg.JSON && removedCount > 0 {
			fmt.Printf("Removed %d feeds not in list\n", removedCount)
		}
	}

	printFetchSummary(cfg, "file", successCount, errorCount, cachedCount, totalItems, len(feedURLs))

	return nil
}

func runDatabaseFetch(cfg *config.Config) error {
	// Get all feeds from database
	dbFeeds, err := database.GetAllFeeds()
	if err != nil {
		return fmt.Errorf("failed to get feeds from database: %w", err)
	}

	if len(dbFeeds) == 0 {
		fmt.Println("No feeds in database")
		return nil
	}

	// Extract URLs
	feedURLs := make([]string, 0, len(dbFeeds))
	for _, feed := range dbFeeds {
		feedURLs = append(feedURLs, feed.URL)
	}

	fmt.Printf("Fetching %d feeds from database\n", len(feedURLs))

	// Fetch feeds concurrently
	results := fetcher.FetchConcurrent(
		feedURLs,
		fetchConcurrency,
		fetchTimeout,
		fetchMaxItems,
		fetchMaxAge,
		fetchForce,
	)

	successCount, errorCount, cachedCount, totalItems := processFetchResults(results)

	printFetchSummary(cfg, "database", successCount, errorCount, cachedCount, totalItems, len(feedURLs))

	return nil
}

func determineFetchFormatAndFilename(
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

func validateFetchFormat(format string) (feedlist.Format, error) {
	switch format {
	case string(feedlist.FormatOPML):
		return feedlist.FormatOPML, nil
	case string(feedlist.FormatText):
		return feedlist.FormatText, nil
	default:
		return "", fmt.Errorf("unsupported format: %s (must be 'opml' or 'text')", format)
	}
}

func processFetchResults(results []*fetcher.FetchResult) (successCount, errorCount, cachedCount, totalItems int) {
	for _, result := range results {
		if result.Error != nil {
			errorCount++
		} else if result.Cached {
			cachedCount++
		} else {
			successCount++
			totalItems += result.ItemCount
		}
	}
	return
}

func removeMissingFeedsFromFile(feedURLs []string) int {
	existingURLs, err := database.GetFeedURLs()
	if err != nil {
		fmt.Printf("Warning: Failed to get existing feeds: %v\n", err)
		return 0
	}

	urlMap := make(map[string]bool)
	for _, url := range feedURLs {
		urlMap[url] = true
	}

	removedCount := 0
	for _, existingURL := range existingURLs {
		if !urlMap[existingURL] {
			if err := database.DeleteFeed(existingURL); err != nil {
				fmt.Printf("Warning: Failed to delete feed %s: %v\n", existingURL, err)
			} else {
				removedCount++
				fmt.Printf("Removed feed not in list: %s\n", existingURL)
			}
		}
	}

	return removedCount
}

func printFetchSummary(
	cfg *config.Config, mode string, successCount, errorCount, cachedCount, totalItems, totalFeeds int,
) {
	if cfg.JSON {
		result := map[string]interface{}{
			"mode":       mode,
			"totalFeeds": totalFeeds,
			"successful": successCount,
			"errors":     errorCount,
			"cached":     cachedCount,
			"totalItems": totalItems,
		}
		jsonData, _ := json.Marshal(result)
		fmt.Println(string(jsonData))
	} else {
		fmt.Printf("\nSummary:\n")
		fmt.Printf("  Total feeds: %d\n", totalFeeds)
		fmt.Printf("  Successful: %d\n", successCount)
		fmt.Printf("  Cached/Skipped: %d\n", cachedCount)
		fmt.Printf("  Errors: %d\n", errorCount)
		fmt.Printf("  Total items: %d\n", totalItems)
	}
}
