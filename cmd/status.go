package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/spf13/cobra"
)

type statusOutput struct {
	FeedCount             int        `json:"feed_count"`
	ItemCount             int        `json:"item_count"`
	LastFetchTime         *time.Time `json:"last_fetch_time"`
	FailingFeedCount      int        `json:"failing_feed_count"`
	ConsecutiveErrorCount int        `json:"consecutive_error_count"`
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Summarize feedspool database status",
	Long: `Show feed and item counts, the most recent fetch attempt, and the
number of failing feeds and consecutive fetch errors currently recorded.`,
	Example: `  feedspool status
  feedspool --json status`,
	Args: cobra.NoArgs,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(_ *cobra.Command, _ []string) error {
	cfg := GetConfig()
	db, err := database.New(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()
	if err := db.IsInitialized(); err != nil {
		return err
	}

	status, err := db.GetSpoolStatus()
	if err != nil {
		return err
	}
	output := statusOutput{
		FeedCount:             status.FeedCount,
		ItemCount:             status.ItemCount,
		FailingFeedCount:      status.FailingFeedCount,
		ConsecutiveErrorCount: status.ConsecutiveErrorCount,
	}
	if status.LastFetchTime.Valid && !status.LastFetchTime.Time.IsZero() {
		lastFetch := status.LastFetchTime.Time
		output.LastFetchTime = &lastFetch
	}

	if cfg.JSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)
	}

	lastFetch := "never"
	if output.LastFetchTime != nil {
		lastFetch = output.LastFetchTime.Format(time.RFC3339)
	}
	//nolint:forbidigo // Command output.
	fmt.Printf("Feeds: %d\nItems: %d\nLast fetch: %s\nFeeds with errors: %d\nConsecutive errors: %d\n",
		output.FeedCount, output.ItemCount, lastFetch,
		output.FailingFeedCount, output.ConsecutiveErrorCount)
	return nil
}
