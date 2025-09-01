package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/feedlist"
	"github.com/lmorchard/feedspool-go/internal/fetcher"
	"github.com/lmorchard/feedspool-go/internal/unfurl"
	"github.com/sirupsen/logrus"
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
	fetchWithUnfurl    bool
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
control, age filtering, and database cleanup based on feed lists.

Parallel Unfurl:
  feedspool fetch --with-unfurl                          # Fetch feeds and unfurl metadata in parallel
  
The --with-unfurl flag enables parallel metadata extraction for new feed items. This runs
unfurl operations concurrently with feed fetching for improved performance. Only new items
without existing metadata will be processed.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runFetch,
}

func init() {
	fetchCmd.Flags().DurationVar(&fetchTimeout, "timeout", config.DefaultTimeout, "Feed fetch timeout")
	fetchCmd.Flags().IntVar(&fetchMaxItems, "max-items", config.DefaultMaxItems, "Maximum items to keep per feed")
	fetchCmd.Flags().BoolVar(&fetchForce, "force", false, "Ignore cache headers and fetch anyway")
	fetchCmd.Flags().IntVar(&fetchConcurrency, "concurrency", config.DefaultConcurrency, "Maximum concurrent fetches")
	fetchCmd.Flags().DurationVar(&fetchMaxAge, "max-age", 0, "Skip feeds fetched within this duration")
	fetchCmd.Flags().BoolVar(&fetchRemoveMissing, "remove-missing", false, "Delete feeds not in list (file mode only)")
	fetchCmd.Flags().StringVar(&fetchFormat, "format", "", "Feed list format (opml or text)")
	fetchCmd.Flags().StringVar(&fetchFilename, "filename", "", "Feed list filename")
	fetchCmd.Flags().BoolVar(&fetchWithUnfurl, "with-unfurl", false, "Run unfurl operations in parallel with feed fetching")
	rootCmd.AddCommand(fetchCmd)
}

// setupGracefulShutdown sets up signal handling for graceful shutdown.
func setupGracefulShutdown() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	
	go func() {
		sig := <-signals
		logrus.Infof("Received signal %v, shutting down gracefully...", sig)
		cancel()
	}()
	
	return ctx, cancel
}

// validateUnfurlConfig validates the unfurl configuration and provides helpful warnings.
func validateUnfurlConfig(cfg *config.Config) error {
	if cfg.Unfurl.Concurrency < 0 {
		return fmt.Errorf("unfurl concurrency cannot be negative")
	}
	if cfg.Unfurl.Concurrency > 100 {
		logrus.Warnf("Unfurl concurrency %d is very high, consider reducing to avoid overwhelming servers", cfg.Unfurl.Concurrency)
	}
	if cfg.Unfurl.RetryAfter < 0 {
		return fmt.Errorf("unfurl retry_after cannot be negative")
	}
	if cfg.Unfurl.RetryAfter > 24*time.Hour {
		logrus.Warnf("Unfurl retry_after %v is very long, failed URLs will wait a long time before retry", cfg.Unfurl.RetryAfter)
	}
	return nil
}

// createUnfurlQueue creates and starts an unfurl queue if withUnfurl is enabled.
func createUnfurlQueue(ctx context.Context, cfg *config.Config, db *database.DB, withUnfurl bool) *unfurl.UnfurlQueue {
	if !withUnfurl {
		return nil
	}
	
	// Validate configuration
	if err := validateUnfurlConfig(cfg); err != nil {
		logrus.Errorf("Invalid unfurl configuration: %v", err)
		return nil
	}
	
	// Use unfurl config settings for concurrency and other options
	concurrency := cfg.Unfurl.Concurrency
	if concurrency <= 0 {
		concurrency = config.DefaultConcurrency
	}
	
	logrus.Infof("Starting fetch with parallel unfurl (unfurl concurrency: %d)", concurrency)
	if cfg.Unfurl.SkipRobots {
		logrus.Debugf("Unfurl robots.txt checking disabled")
	}
	logrus.Debugf("Unfurl retry after: %v", cfg.Unfurl.RetryAfter)
	
	queue := unfurl.NewUnfurlQueue(
		ctx, 
		db, 
		concurrency, 
		cfg.Unfurl.SkipRobots, 
		cfg.Unfurl.RetryAfter,
	)
	queue.Start()
	
	return queue
}

func runFetch(_ *cobra.Command, args []string) error {
	cfg := GetConfig()

	// Determine final withUnfurl value: CLI flag takes precedence over config
	withUnfurl := cfg.Fetch.WithUnfurl || fetchWithUnfurl

	db, err := database.New(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	if err := db.IsInitialized(); err != nil {
		return err
	}

	// Determine fetch mode
	if len(args) == 1 {
		// Single URL mode
		return runSingleURLFetch(cfg, args[0], withUnfurl)
	}

	if fetchFormat != "" || fetchFilename != "" {
		// File mode
		return runFileFetch(cfg, db, withUnfurl)
	}

	// Database mode (no args, no file flags)
	return runDatabaseFetch(cfg, db, withUnfurl)
}

func runSingleURLFetch(cfg *config.Config, feedURL string, withUnfurl bool) error {
	// We need a DB instance for single URL fetch too
	db, err := database.New(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	if err := db.IsInitialized(); err != nil {
		return err
	}

	// Create unfurl queue if needed with graceful shutdown
	ctx, cancel := setupGracefulShutdown()
	defer cancel()
	unfurlQueue := createUnfurlQueue(ctx, cfg, db, withUnfurl)
	
	fetcher := fetcher.NewFetcher(db, fetchTimeout, fetchMaxItems, fetchForce)
	if unfurlQueue != nil {
		fetcher.SetUnfurlQueue(unfurlQueue)
	}
	
	result := fetcher.FetchFeed(feedURL)
	
	// Handle unfurl queue completion
	if unfurlQueue != nil {
		enqueued, processed := unfurlQueue.Stats()
		if enqueued > 0 {
			logrus.Infof("Fetch completed, waiting for %d unfurl operations to complete", enqueued-processed)
		} else {
			logrus.Infof("Fetch completed, no items needed unfurl")
		}
		unfurlQueue.Close()
		
		// Check for cancellation while waiting
		select {
		case <-ctx.Done():
			logrus.Infof("Shutdown signal received, cancelling unfurl operations")
			unfurlQueue.Cancel()
		default:
			unfurlQueue.Wait()
			finalEnqueued, finalProcessed := unfurlQueue.Stats()
			if finalEnqueued > 0 {
				logrus.Infof("All operations completed: %d unfurl operations processed", finalProcessed)
			}
		}
	}

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

func runFileFetch(cfg *config.Config, db *database.DB, withUnfurl bool) error {
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

	if withUnfurl {
		fmt.Printf("Found %d feeds in %s - fetching with parallel unfurl\n", len(feedURLs), filename)
	} else {
		fmt.Printf("Found %d feeds in %s\n", len(feedURLs), filename)
	}

	// Create unfurl queue if needed with graceful shutdown
	ctx, cancel := setupGracefulShutdown()
	defer cancel()
	unfurlQueue := createUnfurlQueue(ctx, cfg, db, withUnfurl)

	// Fetch feeds concurrently (with optional unfurl)
	results := fetcher.FetchConcurrentWithUnfurl(
		db,
		feedURLs,
		fetchConcurrency,
		fetchTimeout,
		fetchMaxItems,
		fetchMaxAge,
		fetchForce,
		unfurlQueue,
	)
	
	// Handle unfurl queue completion
	if unfurlQueue != nil {
		enqueued, processed := unfurlQueue.Stats()
		if enqueued > 0 {
			logrus.Infof("Fetch completed, waiting for %d unfurl operations to complete", enqueued-processed)
		} else {
			logrus.Infof("Fetch completed, no items needed unfurl")
		}
		unfurlQueue.Close()
		
		// Check for cancellation while waiting
		select {
		case <-ctx.Done():
			logrus.Infof("Shutdown signal received, cancelling unfurl operations")
			unfurlQueue.Cancel()
		default:
			unfurlQueue.Wait()
			finalEnqueued, finalProcessed := unfurlQueue.Stats()
			if finalEnqueued > 0 {
				logrus.Infof("All operations completed: %d unfurl operations processed", finalProcessed)
			}
		}
	}

	successCount, errorCount, cachedCount, totalItems := processFetchResults(results)

	// Remove missing feeds if requested
	if fetchRemoveMissing {
		removedCount := removeMissingFeedsFromFile(db, feedURLs)
		if !cfg.JSON && removedCount > 0 {
			fmt.Printf("Removed %d feeds not in list\n", removedCount)
		}
	}

	printFetchSummary(cfg, "file", successCount, errorCount, cachedCount, totalItems, len(feedURLs))

	return nil
}

func runDatabaseFetch(cfg *config.Config, db *database.DB, withUnfurl bool) error {
	// Get all feeds from database
	dbFeeds, err := db.GetAllFeeds()
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

	if withUnfurl {
		fmt.Printf("Fetching %d feeds from database with parallel unfurl\n", len(feedURLs))
	} else {
		fmt.Printf("Fetching %d feeds from database\n", len(feedURLs))
	}

	// Create unfurl queue if needed with graceful shutdown
	ctx, cancel := setupGracefulShutdown()
	defer cancel()
	unfurlQueue := createUnfurlQueue(ctx, cfg, db, withUnfurl)
	
	// Fetch feeds concurrently (with optional unfurl)
	results := fetcher.FetchConcurrentWithUnfurl(
		db,
		feedURLs,
		fetchConcurrency,
		fetchTimeout,
		fetchMaxItems,
		fetchMaxAge,
		fetchForce,
		unfurlQueue,
	)
	
	// Handle unfurl queue completion
	if unfurlQueue != nil {
		enqueued, processed := unfurlQueue.Stats()
		if enqueued > 0 {
			logrus.Infof("Fetch completed, waiting for %d unfurl operations to complete", enqueued-processed)
		} else {
			logrus.Infof("Fetch completed, no items needed unfurl")
		}
		unfurlQueue.Close()
		
		// Check for cancellation while waiting
		select {
		case <-ctx.Done():
			logrus.Infof("Shutdown signal received, cancelling unfurl operations")
			unfurlQueue.Cancel()
		default:
			unfurlQueue.Wait()
			finalEnqueued, finalProcessed := unfurlQueue.Stats()
			if finalEnqueued > 0 {
				logrus.Infof("All operations completed: %d unfurl operations processed", finalProcessed)
			}
		}
	}

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

func removeMissingFeedsFromFile(db *database.DB, feedURLs []string) int {
	existingURLs, err := db.GetFeedURLs()
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
			if err := db.DeleteFeed(existingURL); err != nil {
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
