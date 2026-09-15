package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/spf13/cobra"
)

var (
	itemsFormat   string
	itemsSort     string
	itemsLimit    int
	itemsSince    string
	itemsUntil    string
	itemsUnseen   bool
	itemsSeen     bool
	itemsMarkSeen bool
	itemsFeed     string
	itemsSearch   string
	itemsCompact  bool
)

var itemsCmd = &cobra.Command{
	Use:   "items [URL]",
	Short: "List items across feeds or for a specific feed",
	Long: `List items across all feeds. Pass an exact feed URL as the optional
	argument for compatibility, or use --feed to match a URL substring.
--since and --until filter by discovery time (first_seen) using RFC3339
timestamps. Invalid flags and conflicting filters exit non-zero.`,
	Example: `  feedspool items --since 2026-08-25T12:00:00Z --format json
  feedspool --json items --feed example.com
  feedspool --json items --compact --since 2026-08-25T12:00:00Z
  feedspool items --search "release notes" --limit 20
  feedspool items --search "container networking" --sort relevance
  feedspool items https://example.com/feed.xml`,
	Args: cobra.MaximumNArgs(1),
	RunE: runItems,
}

func init() {
	itemsCmd.Flags().StringVar(&itemsFormat, "format", formatTable, "Output format (table|json|csv)")
	itemsCmd.Flags().StringVar(&itemsSort, "sort", sortNewest, "Sort order (newest|oldest|relevance)")
	itemsCmd.Flags().IntVar(&itemsLimit, "limit", 0, "Maximum items to return (0 for all)")
	itemsCmd.Flags().StringVar(&itemsSince, "since", "", "Filter items discovered after time (RFC3339)")
	itemsCmd.Flags().StringVar(&itemsUntil, "until", "", "Filter items discovered until time (RFC3339)")
	itemsCmd.Flags().BoolVar(&itemsUnseen, "unseen", false, "Filter to items with no seen annotation")
	itemsCmd.Flags().BoolVar(&itemsSeen, "seen", false, "Filter to items with a seen annotation")
	itemsCmd.Flags().BoolVar(&itemsMarkSeen, "mark-seen", false, "Mark all returned items as seen")
	itemsCmd.Flags().StringVar(&itemsFeed, "feed", "", "Filter by feed URL substring")
	itemsCmd.Flags().StringVar(&itemsSearch, "search", "", "Full-text search over title, summary and body")
	itemsCmd.Flags().BoolVar(&itemsCompact, "compact", false, "Emit a compact JSON manifest")
	rootCmd.AddCommand(itemsCmd)
}

func runItems(_ *cobra.Command, args []string) error {
	if err := validateItemsOptions(args); err != nil {
		return err
	}

	feedURL := ""
	if len(args) > 0 {
		feedURL = args[0]
	}

	cfg := GetConfig()
	db, err := database.New(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	if err := db.IsInitialized(); err != nil {
		return err
	}

	since, until, err := parseDateFiltersForItems()
	if err != nil {
		return err
	}

	filter := database.ItemFilter{
		FeedURL:   feedURL,
		FeedQuery: itemsFeed,
		Search:    itemsSearch,
		Sort:      itemsSort,
		Limit:     itemsLimit,
		Since:     since,
		Until:     until,
		Unseen:    itemsUnseen,
		Seen:      itemsSeen,
	}

	items, err := db.GetItems(&filter)
	if err != nil {
		return err
	}

	// Only the oldest-first ordering is produced by reversing; relevance arrives
	// already ranked best-first, and reversing it would surface the worst match.
	if itemsSort == sortOldest {
		reverseItemsList(items)
	}

	if itemsMarkSeen {
		if err := markItemsSeen(db, items); err != nil {
			return err
		}
	}

	format := itemsFormat
	if format == formatTable && cfg.JSON {
		format = formatJSON
	}
	if itemsCompact && format != formatJSON {
		return fmt.Errorf("--compact requires JSON output")
	}

	return outputItemsInFormat(format, items, itemsCompact)
}

func validateItemsOptions(args []string) error {
	if len(args) > 0 && itemsFeed != "" {
		return fmt.Errorf("feed URL argument cannot be combined with --feed")
	}
	if itemsSeen && itemsUnseen {
		return fmt.Errorf("--seen cannot be combined with --unseen")
	}
	if itemsSort != sortNewest && itemsSort != sortOldest && itemsSort != sortRelevance {
		return fmt.Errorf("unknown sort order: %s", itemsSort)
	}
	if itemsSort == sortRelevance && itemsSearch == "" {
		return fmt.Errorf("--sort relevance requires --search")
	}
	return nil
}

func markItemsSeen(db *database.DB, items []*database.Item) error {
	for _, item := range items {
		if err := db.AddAnnotation(item.FeedURL, item.GUID, "seen", sql.NullString{}, sql.NullString{}); err != nil {
			return fmt.Errorf("failed to mark seen: %w", err)
		}
	}
	return nil
}

func parseDateFiltersForItems() (since, until time.Time, err error) {
	if itemsSince != "" {
		since, err = time.Parse(time.RFC3339, itemsSince)
		if err != nil {
			err = fmt.Errorf("invalid since date: %w", err)
			return
		}
	}

	if itemsUntil != "" {
		until, err = time.Parse(time.RFC3339, itemsUntil)
		if err != nil {
			err = fmt.Errorf("invalid until date: %w", err)
			return
		}
	}
	return
}

func reverseItemsList(items []*database.Item) {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}

type compactItemOutput struct {
	FeedURL       string    `json:"feed_url"`
	GUID          string    `json:"guid"`
	Title         string    `json:"title"`
	Link          string    `json:"link"`
	PublishedDate time.Time `json:"published_date"`
	DiscoveredAt  time.Time `json:"discovered_at"`
}

func outputItemsInFormat(format string, items []*database.Item, compact bool) error {
	switch format {
	case formatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if compact {
			outputs := make([]compactItemOutput, 0, len(items))
			for _, item := range items {
				outputs = append(outputs, compactItemOutput{
					FeedURL: item.FeedURL, GUID: item.GUID, Title: item.Title, Link: item.Link,
					PublishedDate: item.PublishedDate, DiscoveredAt: item.DiscoveredAt(),
				})
			}
			return encoder.Encode(outputs)
		}
		return encoder.Encode(items)
	case formatCSV:
		return outputCSV(items) // Reuse existing outputCSV
	case formatTable:
		return outputTable(items) // Reuse existing outputTable
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}
