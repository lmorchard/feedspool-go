package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/feedlist"
	"github.com/spf13/cobra"
)

var (
	purgeAge      string
	purgeDryRun   bool
	purgeFormat   string
	purgeFilename string
	purgeNoVacuum bool
	purgeMinItems int
)

var purgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Purge archived items and cleanup unsubscribed feeds",
	Long: `Purge command performs two cleanup operations:

Age-based purging:
  Deletes archived items from the database that are older than the specified age.
  Uses --age flag or purge.max_age from config (default: 30d).

  Minimum items protection:
  Use --min-items to ensure at least N items remain per feed regardless of age.
  This helps preserve history for infrequently-updated feeds.

Feed list cleanup (optional):
  When --format and filename are specified (or configured), removes any feeds
  (and their items) from the database that are NOT in the specified feed list.

Examples:
  feedspool purge                             # Delete old items using config max_age
  feedspool purge --age 30d                   # Delete items older than 30 days
  feedspool purge --age 30d --min-items 15    # Keep at least 15 items per feed
  feedspool purge --format text feeds.txt     # Also cleanup unsubscribed feeds
  feedspool purge --age 7d --dry-run          # Preview what would be deleted`,
	RunE: runPurge,
}

func init() {
	purgeCmd.Flags().StringVar(&purgeAge, "age", "", "Delete items older than this (e.g., 30d, 1w, 48h)")
	purgeCmd.Flags().IntVar(&purgeMinItems, "min-items", 0,
		"Minimum items to keep per feed regardless of age (0 = no minimum)")
	purgeCmd.Flags().BoolVar(&purgeDryRun, "dry-run", false, "Preview what would be deleted without actually deleting")
	purgeCmd.Flags().StringVar(&purgeFormat, "format", "", "Feed list format for cleanup (opml or text)")
	purgeCmd.Flags().StringVar(&purgeFilename, "filename", "", "Feed list filename for cleanup")
	purgeCmd.Flags().BoolVar(&purgeNoVacuum, "no-vacuum", false, "Skip running VACUUM on the database")
	rootCmd.AddCommand(purgeCmd)
}

func runPurge(_ *cobra.Command, _ []string) error {
	cfg := GetConfig()

	db, err := database.New(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	if err := db.IsInitialized(); err != nil {
		return err
	}

	// Run list-based cleanup if format/filename provided or configured
	if shouldRunListCleanup(cfg) {
		if err := runFeedListCleanup(cfg, db); err != nil {
			return err
		}
	}

	// Determine minimum items to keep per feed
	minItems := purgeMinItems
	if minItems == 0 {
		minItems = cfg.Purge.MinItemsKeep
	}

	// Always run age-based cleanup
	if err := runAgePurge(cfg, db, minItems); err != nil {
		return err
	}

	// Run VACUUM unless skipped via flag or config
	shouldSkipVacuum := purgeNoVacuum || cfg.Purge.SkipVacuum
	if !shouldSkipVacuum && !purgeDryRun {
		fmt.Println("Running VACUUM to optimize database...")
		if err := db.Vacuum(); err != nil {
			fmt.Printf("Warning: Failed to vacuum database: %v\n", err)
		} else {
			fmt.Println("Database vacuumed successfully")
		}
	}

	return nil
}

func shouldRunListCleanup(cfg *config.Config) bool {
	// Run if CLI flags are provided
	if purgeFormat != "" || purgeFilename != "" {
		return true
	}
	// Run if default feedlist is configured
	return cfg.HasDefaultFeedList()
}

func runFeedListCleanup(cfg *config.Config, db *database.DB) error {
	format, filename, err := determinePurgeFormatAndFilename(cfg, purgeFormat, purgeFilename)
	if err != nil {
		return err
	}

	feedFormat, err := validatePurgeFormat(format)
	if err != nil {
		return err
	}

	authorizedURLs, err := loadAuthorizedFeeds(feedFormat, filename)
	if err != nil {
		return err
	}

	feedsToDelete, err := findFeedsToDelete(db, authorizedURLs)
	if err != nil {
		return err
	}

	return processFeedDeletion(cfg, db, feedsToDelete, format, filename)
}

func loadAuthorizedFeeds(feedFormat feedlist.Format, filename string) ([]string, error) {
	list, err := feedlist.LoadFeedList(feedFormat, filename)
	if err != nil {
		return nil, fmt.Errorf("failed to load feed list %s: %w", filename, err)
	}

	authorizedURLs := list.GetURLs()
	fmt.Printf("Loaded %d subscribed feed(s) from %s\n", len(authorizedURLs), filename)
	return authorizedURLs, nil
}

func findFeedsToDelete(db *database.DB, authorizedURLs []string) ([]string, error) {
	dbFeeds, err := db.GetAllFeeds()
	if err != nil {
		return nil, fmt.Errorf("failed to get feeds from database: %w", err)
	}

	authorizedSet := make(map[string]bool)
	for _, url := range authorizedURLs {
		authorizedSet[url] = true
	}

	var feedsToDelete []string
	for _, feed := range dbFeeds {
		if !authorizedSet[feed.URL] {
			feedsToDelete = append(feedsToDelete, feed.URL)
		}
	}

	return feedsToDelete, nil
}

func processFeedDeletion(cfg *config.Config, db *database.DB, feedsToDelete []string, format, filename string) error {
	if len(feedsToDelete) == 0 {
		return reportNoFeedsToDelete(cfg, format, filename)
	}

	if purgeDryRun {
		return reportDryRunDeletion(cfg, feedsToDelete, format, filename)
	}

	return executeFeedDeletion(cfg, db, feedsToDelete, format, filename)
}

func reportNoFeedsToDelete(cfg *config.Config, format, filename string) error {
	if cfg.JSON {
		result := map[string]interface{}{
			"mode":     "feedlist",
			"dryRun":   purgeDryRun,
			"deleted":  0,
			"filename": filename,
			"format":   format,
			"message":  "No feeds to delete - all database feeds are subscribed",
		}
		jsonData, _ := json.Marshal(result)
		fmt.Println(string(jsonData))
	} else {
		fmt.Println("No feeds to delete - all database feeds are subscribed")
	}
	return nil
}

func reportDryRunDeletion(cfg *config.Config, feedsToDelete []string, format, filename string) error {
	if cfg.JSON {
		result := map[string]interface{}{
			"mode":          "feedlist",
			"dryRun":        true,
			"wouldDelete":   len(feedsToDelete),
			"filename":      filename,
			"format":        format,
			"feedsToDelete": feedsToDelete,
		}
		jsonData, _ := json.Marshal(result)
		fmt.Println(string(jsonData))
	} else {
		fmt.Printf("Dry run mode - would delete %d unsubscribed feed(s):\n", len(feedsToDelete))
		for _, url := range feedsToDelete {
			fmt.Printf("  - %s\n", url)
		}
	}
	return nil
}

func executeFeedDeletion(cfg *config.Config, db *database.DB, feedsToDelete []string, format, filename string) error {
	deletedCount := 0
	for _, url := range feedsToDelete {
		if err := db.DeleteFeed(url); err != nil {
			fmt.Printf("Warning: Failed to delete feed %s: %v\n", url, err)
		} else {
			deletedCount++
			fmt.Printf("Deleted unsubscribed feed: %s\n", url)
		}
	}

	// Clean up orphaned metadata after deleting feeds
	metadataDeleted, err := db.DeleteOrphanedMetadata()
	if err != nil {
		fmt.Printf("Warning: Failed to clean up orphaned metadata: %v\n", err)
	} else if metadataDeleted > 0 {
		fmt.Printf("Cleaned up %d orphaned metadata entries\n", metadataDeleted)
	}

	if cfg.JSON {
		result := map[string]interface{}{
			"mode":            "feedlist",
			"dryRun":          false,
			"deleted":         deletedCount,
			"filename":        filename,
			"format":          format,
			"metadataDeleted": metadataDeleted,
		}
		jsonData, _ := json.Marshal(result)
		fmt.Println(string(jsonData))
	} else {
		fmt.Printf("Deleted %d unsubscribed feed(s) from database\n", deletedCount)
	}

	return nil
}

func runAgePurge(cfg *config.Config, db *database.DB, minItems int) error {
	// Use --age flag if provided, otherwise use config max_age, fallback to 30d
	ageStr := purgeAge
	if ageStr == "" {
		ageStr = cfg.Purge.MaxAge
	}
	if ageStr == "" {
		ageStr = "30d"
	}

	duration, err := database.ParseDuration(ageStr)
	if err != nil {
		return fmt.Errorf("invalid age format: %w", err)
	}

	cutoffTime := time.Now().Add(-duration)

	if purgeDryRun {
		if cfg.JSON {
			result := map[string]interface{}{
				"mode":         "age",
				"dryRun":       true,
				"cutoffDate":   cutoffTime.Format(time.RFC3339),
				"minItemsKeep": minItems,
				"deleted":      0,
			}
			jsonData, _ := json.Marshal(result)
			fmt.Println(string(jsonData))
		} else {
			fmt.Printf("Dry run mode - would delete archived items older than %s\n", cutoffTime.Format("2006-01-02"))
			fmt.Printf("(Items published before %s)\n", cutoffTime.Format(time.RFC3339))
			if minItems > 0 {
				fmt.Printf("Keeping at least %d items per feed\n", minItems)
			}
		}
		return nil
	}

	var deleted int64
	if minItems > 0 {
		deleted, err = db.DeleteArchivedItemsWithMinimum(cutoffTime, minItems)
	} else {
		deleted, err = db.DeleteArchivedItems(cutoffTime)
	}
	if err != nil {
		return fmt.Errorf("failed to delete archived items: %w", err)
	}

	// Clean up orphaned metadata after deleting items
	metadataDeleted, err := db.DeleteOrphanedMetadata()
	if err != nil {
		fmt.Printf("Warning: Failed to clean up orphaned metadata: %v\n", err)
	} else if metadataDeleted > 0 {
		fmt.Printf("Cleaned up %d orphaned metadata entries\n", metadataDeleted)
	}

	if cfg.JSON {
		result := map[string]interface{}{
			"mode":            "age",
			"dryRun":          false,
			"cutoffDate":      cutoffTime.Format(time.RFC3339),
			"minItemsKeep":    minItems,
			"deleted":         deleted,
			"metadataDeleted": metadataDeleted,
		}
		jsonData, _ := json.Marshal(result)
		fmt.Println(string(jsonData))
	} else {
		fmt.Printf("Deleted %d archived items older than %s\n", deleted, cutoffTime.Format("2006-01-02"))
		if minItems > 0 {
			fmt.Printf("(Kept at least %d items per feed)\n", minItems)
		}
	}

	return nil
}

func determinePurgeFormatAndFilename(
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

func validatePurgeFormat(format string) (feedlist.Format, error) {
	switch format {
	case string(feedlist.FormatOPML):
		return feedlist.FormatOPML, nil
	case string(feedlist.FormatText):
		return feedlist.FormatText, nil
	default:
		return "", fmt.Errorf("unsupported format: %s (must be 'opml' or 'text')", format)
	}
}
