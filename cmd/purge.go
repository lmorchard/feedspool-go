package cmd

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/feedlist"
	"github.com/spf13/cobra"
)

const defaultPurgeAge = "30d"

var (
	purgeAge      string
	purgeDryRun   bool
	purgeFormat   string
	purgeFilename string
)

var purgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Purge archived items or cleanup feeds based on feed lists",
	Long: `Purge command operates in two modes:

Age-based purging (default):
  Deletes archived items from the database that are older than the specified age.
  
Feed list cleanup:
  When --format and filename are specified, removes any feeds (and their items) 
  from the database that are NOT in the specified feed list. This allows cleanup
  based on authoritative feed lists.

Examples:
  feedspool purge --age 30d                    # Delete items older than 30 days
  feedspool purge --format text feeds.txt     # Keep only feeds in feeds.txt
  feedspool purge --format opml feeds.opml    # Keep only feeds in feeds.opml`,
	RunE: runPurge,
}

func init() {
	purgeCmd.Flags().StringVar(&purgeAge, "age", defaultPurgeAge, "Delete items older than this (e.g., 30d, 1w, 48h)")
	purgeCmd.Flags().BoolVar(&purgeDryRun, "dry-run", false, "Preview what would be deleted without actually deleting")
	purgeCmd.Flags().StringVar(&purgeFormat, "format", "", "Feed list format for cleanup mode (opml or text)")
	purgeCmd.Flags().StringVar(&purgeFilename, "filename", "", "Feed list filename for cleanup mode")
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

	// List mode is active if either format or filename is specified
	isListMode := purgeFormat != "" || purgeFilename != ""

	// Age mode is active if:
	// - The user specified a non-default age (explicit age-based purge)
	// - Or, neither list mode nor a non-default age is specified (default to age-based purge)
	isExplicitAge := purgeAge != defaultPurgeAge
	isDefaultAge := purgeAge == defaultPurgeAge
	isAgeMode := isExplicitAge || (!isListMode && isDefaultAge)

	// Validate mode selection
	if isListMode && isAgeMode && purgeAge != defaultPurgeAge {
		return fmt.Errorf("cannot specify both age-based and list-based purging options")
	}

	if isListMode {
		return runFeedListCleanup(cfg, db)
	}
	return runAgePurge(cfg, db)
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
	fmt.Printf("Loaded %d authorized feed(s) from %s\n", len(authorizedURLs), filename)
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
			"message":  "No feeds to delete - all database feeds are authorized",
		}
		jsonData, _ := json.Marshal(result)
		fmt.Println(string(jsonData))
	} else {
		fmt.Println("No feeds to delete - all database feeds are authorized")
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
		fmt.Printf("Dry run mode - would delete %d unauthorized feed(s):\n", len(feedsToDelete))
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
			fmt.Printf("Deleted unauthorized feed: %s\n", url)
		}
	}

	if cfg.JSON {
		result := map[string]interface{}{
			"mode":     "feedlist",
			"dryRun":   false,
			"deleted":  deletedCount,
			"filename": filename,
			"format":   format,
		}
		jsonData, _ := json.Marshal(result)
		fmt.Println(string(jsonData))
	} else {
		fmt.Printf("Deleted %d unauthorized feed(s) from database\n", deletedCount)
	}

	return nil
}

func runAgePurge(cfg *config.Config, db *database.DB) error {
	duration, err := parseDuration(purgeAge)
	if err != nil {
		return fmt.Errorf("invalid age format: %w", err)
	}

	cutoffTime := time.Now().Add(-duration)

	if purgeDryRun {
		if cfg.JSON {
			result := map[string]interface{}{
				"mode":       "age",
				"dryRun":     true,
				"cutoffDate": cutoffTime.Format(time.RFC3339),
				"deleted":    0,
			}
			jsonData, _ := json.Marshal(result)
			fmt.Println(string(jsonData))
		} else {
			fmt.Printf("Dry run mode - would delete archived items older than %s\n", cutoffTime.Format("2006-01-02"))
			fmt.Printf("(Items published before %s)\n", cutoffTime.Format(time.RFC3339))
		}
		return nil
	}

	deleted, err := db.DeleteArchivedItems(cutoffTime)
	if err != nil {
		return fmt.Errorf("failed to delete archived items: %w", err)
	}

	if cfg.JSON {
		result := map[string]interface{}{
			"mode":       "age",
			"dryRun":     false,
			"cutoffDate": cutoffTime.Format(time.RFC3339),
			"deleted":    deleted,
		}
		jsonData, _ := json.Marshal(result)
		fmt.Println(string(jsonData))
	} else {
		fmt.Printf("Deleted %d archived items older than %s\n", deleted, cutoffTime.Format("2006-01-02"))
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

func parseDuration(s string) (time.Duration, error) {
	re := regexp.MustCompile(`^(\d+)([dwh])$`)
	matches := re.FindStringSubmatch(strings.ToLower(s))

	if len(matches) != 3 {
		return time.ParseDuration(s)
	}

	num, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, err
	}

	switch matches[2] {
	case "d":
		return time.Duration(num) * 24 * time.Hour, nil
	case "w":
		return time.Duration(num) * 7 * 24 * time.Hour, nil
	case "h":
		return time.Duration(num) * time.Hour, nil
	default:
		return time.ParseDuration(s)
	}
}
