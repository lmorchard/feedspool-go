package cmd

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/spf13/cobra"
)

var (
	purgeAge    string
	purgeDryRun bool
)

var purgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Purge archived items",
	Long:  `Deletes archived items from the database that are older than the specified age.`,
	RunE:  runPurge,
}

func init() {
	purgeCmd.Flags().StringVar(&purgeAge, "age", "30d", "Delete items older than this (e.g., 30d, 1w, 48h)")
	purgeCmd.Flags().BoolVar(&purgeDryRun, "dry-run", false, "Preview what would be deleted without actually deleting")
	rootCmd.AddCommand(purgeCmd)
}

func runPurge(_ *cobra.Command, _ []string) error {
	cfg := GetConfig()

	if err := database.Connect(cfg.Database); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer database.Close()

	duration, err := parseDuration(purgeAge)
	if err != nil {
		return fmt.Errorf("invalid age format: %w", err)
	}

	cutoffTime := time.Now().Add(-duration)

	if purgeDryRun {
		if cfg.JSON {
			result := map[string]interface{}{
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

	deleted, err := database.DeleteArchivedItems(cutoffTime)
	if err != nil {
		return fmt.Errorf("failed to delete archived items: %w", err)
	}

	if cfg.JSON {
		result := map[string]interface{}{
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
