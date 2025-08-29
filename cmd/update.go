package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/fetcher"
	"github.com/lmorchard/feedspool-go/internal/opml"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	updateConcurrency   int
	updateTimeout       time.Duration
	updateMaxAge        time.Duration
	updateMaxItems      int
	updateRemoveMissing bool
)

var updateCmd = &cobra.Command{
	Use:   "update [OPML file]",
	Short: "Update feeds from OPML",
	Long:  `Fetches all feeds from an OPML file and updates the database. Use - to read from stdin.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runUpdate,
}

func init() {
	updateCmd.Flags().IntVar(&updateConcurrency, "concurrency", 32, "Maximum concurrent fetches")
	updateCmd.Flags().DurationVar(&updateTimeout, "timeout", 30*time.Second, "Per-feed fetch timeout")
	updateCmd.Flags().DurationVar(&updateMaxAge, "max-age", 0, "Skip feeds fetched within this duration")
	updateCmd.Flags().IntVar(&updateMaxItems, "max-items", 100, "Maximum items to keep per feed")
	updateCmd.Flags().BoolVar(&updateRemoveMissing, "remove-missing", false, "Delete feeds not in OPML")
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(_ *cobra.Command, args []string) error {
	opmlFile := args[0]
	cfg := GetConfig()

	if err := database.Connect(cfg.Database); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer database.Close()

	feedURLs, err := loadFeedURLs(opmlFile)
	if err != nil {
		return err
	}

	fmt.Printf("Found %d feeds in OPML\n", len(feedURLs))

	results := fetcher.FetchConcurrent(
		feedURLs,
		updateConcurrency,
		updateTimeout,
		updateMaxItems,
		updateMaxAge,
		false,
	)

	successCount, errorCount, cachedCount, totalItems := processResults(results)

	if updateRemoveMissing {
		removeMissingFeeds(feedURLs)
	}

	printSummary(cfg, successCount, errorCount, cachedCount, totalItems, len(feedURLs))

	return nil
}

func loadFeedURLs(opmlFile string) ([]string, error) {
	var reader io.Reader
	if opmlFile == "-" {
		reader = os.Stdin
	} else {
		file, err := os.Open(opmlFile)
		if err != nil {
			return nil, fmt.Errorf("failed to open OPML file: %w", err)
		}
		defer file.Close()
		reader = file
	}

	opmlData, err := opml.ParseOPML(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OPML: %w", err)
	}

	feedURLs := opml.ExtractFeedURLs(opmlData)
	if len(feedURLs) == 0 {
		return nil, fmt.Errorf("no feed URLs found in OPML")
	}

	return feedURLs, nil
}

func processResults(results []*fetcher.FetchResult) (successCount, errorCount, cachedCount, totalItems int) {
	for _, result := range results {
		if result.Error != nil {
			errorCount++
			logrus.Warnf("Failed to fetch %s: %v", result.URL, result.Error)
		} else if result.Cached {
			cachedCount++
			logrus.Debugf("Feed cached/skipped: %s", result.URL)
		} else {
			successCount++
			totalItems += result.ItemCount
			if result.Feed != nil {
				logrus.Infof("Fetched: %s - %s (%d items)", result.URL, result.Feed.Title, result.ItemCount)
			}
		}
	}
	return
}

func removeMissingFeeds(feedURLs []string) {
	existingURLs, err := database.GetFeedURLs()
	if err != nil {
		logrus.Warnf("Failed to get existing feeds: %v", err)
		return
	}

	urlMap := make(map[string]bool)
	for _, url := range feedURLs {
		urlMap[url] = true
	}

	removedCount := 0
	for _, existingURL := range existingURLs {
		if !urlMap[existingURL] {
			if err := database.DeleteFeed(existingURL); err != nil {
				logrus.Warnf("Failed to delete feed %s: %v", existingURL, err)
			} else {
				removedCount++
				logrus.Infof("Removed feed not in OPML: %s", existingURL)
			}
		}
	}

	if removedCount > 0 {
		fmt.Printf("Removed %d feeds not in OPML\n", removedCount)
	}
}

func printSummary(cfg *config.Config, successCount, errorCount, cachedCount, totalItems, totalFeeds int) {
	if cfg.JSON {
		summary := map[string]interface{}{
			"successful": successCount,
			"cached":     cachedCount,
			"failed":     errorCount,
			"totalItems": totalItems,
			"totalFeeds": totalFeeds,
		}
		jsonData, _ := json.Marshal(summary)
		fmt.Println(string(jsonData))
	} else {
		fmt.Println("\nUpdate Summary:")
		fmt.Printf("  Successful: %d\n", successCount)
		fmt.Printf("  Cached/Skipped: %d\n", cachedCount)
		fmt.Printf("  Failed: %d\n", errorCount)
		fmt.Printf("  Total Items: %d\n", totalItems)
	}
}
