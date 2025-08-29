package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/spf13/cobra"
)

const (
	formatJSON  = "json"
	formatTable = "table"
)

type FeedWithItems struct {
	*database.Feed
	Items []*database.Item `json:"Items"`
}

var (
	showFormat string
	showSort   string
	showLimit  int
	showSince  string
	showUntil  string
)

var showCmd = &cobra.Command{
	Use:   "show [URL]",
	Short: "Show items for a feed",
	Long:  `Lists items for a given feed URL from the database.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runShow,
}

func init() {
	showCmd.Flags().StringVar(&showFormat, "format", formatTable, "Output format (table|json|csv)")
	showCmd.Flags().StringVar(&showSort, "sort", "newest", "Sort order (newest|oldest)")
	showCmd.Flags().IntVar(&showLimit, "limit", 0, "Maximum items to return (0 for all)")
	showCmd.Flags().StringVar(&showSince, "since", "", "Filter items since date (RFC3339)")
	showCmd.Flags().StringVar(&showUntil, "until", "", "Filter items until date (RFC3339)")
	rootCmd.AddCommand(showCmd)
}

func runShow(_ *cobra.Command, args []string) error {
	feedURL := args[0]
	cfg := GetConfig()

	if err := database.Connect(cfg.Database); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer database.Close()

	if err := database.IsInitialized(); err != nil {
		return err
	}

	since, until, err := parseDateFilters()
	if err != nil {
		return err
	}

	feed, items, err := getFeedAndItems(feedURL, since, until)
	if err != nil {
		return err
	}

	if showSort == "oldest" {
		reverseItems(items)
	}

	format := determineOutputFormat(cfg)
	return outputInFormat(format, feed, items)
}

func parseDateFilters() (since, until time.Time, err error) {
	if showSince != "" {
		since, err = time.Parse(time.RFC3339, showSince)
		if err != nil {
			err = fmt.Errorf("invalid since date: %w", err)
			return
		}
	}

	if showUntil != "" {
		until, err = time.Parse(time.RFC3339, showUntil)
		if err != nil {
			err = fmt.Errorf("invalid until date: %w", err)
			return
		}
	}

	return
}

func getFeedAndItems(feedURL string, since, until time.Time) (*database.Feed, []*database.Item, error) {
	feed, err := database.GetFeed(feedURL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get feed: %w", err)
	}

	items, err := database.GetItemsForFeed(feedURL, showLimit, since, until)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get items: %w", err)
	}

	return feed, items, nil
}

func reverseItems(items []*database.Item) {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}

func determineOutputFormat(cfg *config.Config) string {
	format := showFormat
	if format == formatTable && cfg.JSON {
		format = formatJSON
	}
	return format
}

func outputInFormat(format string, feed *database.Feed, items []*database.Item) error {
	switch format {
	case formatJSON:
		return outputJSON(feed, items)
	case "csv":
		return outputCSV(items)
	case formatTable:
		return outputTable(items)
	default:
		return fmt.Errorf("unknown format: %s", showFormat)
	}
}

func outputTable(items []*database.Item) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DATE\tTITLE\tLINK")
	fmt.Fprintln(w, "----\t-----\t----")

	for _, item := range items {
		date := item.PublishedDate.Format("2006-01-02 15:04")
		title := item.Title
		if len(title) > 60 {
			title = title[:57] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", date, title, item.Link)
	}

	return w.Flush()
}

func outputJSON(feed *database.Feed, items []*database.Item) error {
	feedWithItems := &FeedWithItems{
		Feed:  feed,
		Items: items,
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(feedWithItems)
}

func outputCSV(items []*database.Item) error {
	w := csv.NewWriter(os.Stdout)

	if err := w.Write([]string{"Date", "Title", "Link", "Summary"}); err != nil {
		return err
	}

	for _, item := range items {
		record := []string{
			item.PublishedDate.Format(time.RFC3339),
			item.Title,
			item.Link,
			item.Summary,
		}
		if err := w.Write(record); err != nil {
			return err
		}
	}

	w.Flush()
	return w.Error()
}
