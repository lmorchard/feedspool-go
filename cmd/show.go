package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/spf13/cobra"
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
	Use:   "show <feed>",
	Short: "Show items for a feed",
	Long: `List items for a feed selected by its exact URL or a unique URL
substring. A missing or ambiguous selector exits non-zero; ambiguity errors
list every matching URL.`,
	Example: `  feedspool show https://example.com/feed.xml
  feedspool show example.com
  feedspool --json show example.com --since 2026-08-25T12:00:00Z`,
	Args: cobra.ExactArgs(1),
	RunE: runShow,
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

	db, err := database.New(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	if err := db.IsInitialized(); err != nil {
		return err
	}

	since, until, err := parseDateFilters()
	if err != nil {
		return err
	}

	feed, items, err := getFeedAndItems(db, feedURL, since, until)
	if err != nil {
		return err
	}

	if showSort == sortOldest {
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

func getFeedAndItems(
	db *database.DB, feedQuery string, since, until time.Time,
) (*database.Feed, []*database.Item, error) {
	feed, err := resolveFeed(db, feedQuery)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get feed: %w", err)
	}

	items, err := db.GetItemsForFeed(feed.URL, showLimit, since, until)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get items: %w", err)
	}

	return feed, items, nil
}

func resolveFeed(db *database.DB, query string) (*database.Feed, error) {
	feed, err := db.GetFeed(query)
	if err != nil {
		return nil, err
	}
	if feed != nil {
		return feed, nil
	}

	matches, err := db.FindFeedsByURLSubstring(query)
	if err != nil {
		return nil, err
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no feed URL matches %q", query)
	case 1:
		return matches[0], nil
	default:
		urls := make([]string, 0, len(matches))
		for _, match := range matches {
			urls = append(urls, match.URL)
		}
		return nil, fmt.Errorf("feed selector %q is ambiguous; matches: %s", query, strings.Join(urls, ", "))
	}
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
	case formatCSV:
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
		date := item.EffectiveDate().Format("2006-01-02 15:04")
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
			item.EffectiveDate().Format(time.RFC3339),
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
