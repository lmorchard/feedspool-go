package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/spf13/cobra"
)

var (
	feedsFormat string
	feedsErrors bool
)

var feedsCmd = &cobra.Command{
	Use:   "feeds",
	Short: "List feeds tracked in the database",
	Long: `List tracked feeds with their URL, title, last fetch time, item count,
and current consecutive fetch error count.`,
	Example: `  feedspool feeds
  feedspool feeds --format json
  feedspool feeds --errors
  feedspool --json feeds`,
	Args: cobra.NoArgs,
	RunE: runFeeds,
}

type feedSummaryOutput struct {
	URL           string     `json:"url"`
	Title         string     `json:"title"`
	LastFetchTime *time.Time `json:"last_fetch_time"`
	ItemCount     int        `json:"item_count"`
	ErrorCount    int        `json:"error_count"`
}

func init() {
	feedsCmd.Flags().StringVar(&feedsFormat, "format", formatTable, "Output format (table|json)")
	feedsCmd.Flags().BoolVar(&feedsErrors, "errors", false, "Show only feeds with fetch errors")
	rootCmd.AddCommand(feedsCmd)
}

func runFeeds(_ *cobra.Command, _ []string) error {
	cfg := GetConfig()
	db, err := database.New(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()
	if err := db.IsInitialized(); err != nil {
		return err
	}

	feeds, err := db.GetFeedSummaries()
	if err != nil {
		return err
	}
	if feedsErrors {
		failing := make([]database.FeedSummary, 0, len(feeds))
		for _, feed := range feeds {
			if feed.ErrorCount > 0 {
				failing = append(failing, feed)
			}
		}
		feeds = failing
	}
	format := feedsFormat
	if format == formatTable && cfg.JSON {
		format = formatJSON
	}
	switch format {
	case formatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(feedSummaryOutputs(feeds))
	case formatTable:
		return outputFeedSummaries(feeds)
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}

func feedSummaryOutputs(feeds []database.FeedSummary) []feedSummaryOutput {
	outputs := make([]feedSummaryOutput, 0, len(feeds))
	for _, feed := range feeds {
		output := feedSummaryOutput{
			URL: feed.URL, Title: feed.Title, ItemCount: feed.ItemCount, ErrorCount: feed.ErrorCount,
		}
		if feed.LastFetchTime.Valid && !feed.LastFetchTime.Time.IsZero() {
			lastFetch := feed.LastFetchTime.Time
			output.LastFetchTime = &lastFetch
		}
		outputs = append(outputs, output)
	}
	return outputs
}

func outputFeedSummaries(feeds []database.FeedSummary) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "URL\tTITLE\tLAST FETCH\tITEMS\tERRORS")
	fmt.Fprintln(w, "---\t-----\t----------\t-----\t------")
	for _, feed := range feeds {
		lastFetch := "-"
		if feed.LastFetchTime.Valid && !feed.LastFetchTime.Time.IsZero() {
			lastFetch = feed.LastFetchTime.Time.Format(time.RFC3339)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\n",
			feed.URL, feed.Title, lastFetch, feed.ItemCount, feed.ErrorCount)
	}
	return w.Flush()
}
