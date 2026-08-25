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
)

var itemsCmd = &cobra.Command{
	Use:   "items [URL]",
	Short: "List items across feeds or for a specific feed",
	Long:  `Lists items. If a feed URL is provided, lists items for that feed. Otherwise lists items across all feeds.`,
	Args:  cobra.MaximumNArgs(1),
	RunE:  runItems,
}

func init() {
	itemsCmd.Flags().StringVar(&itemsFormat, "format", formatTable, "Output format (table|json|csv)")
	itemsCmd.Flags().StringVar(&itemsSort, "sort", "newest", "Sort order (newest|oldest)")
	itemsCmd.Flags().IntVar(&itemsLimit, "limit", 0, "Maximum items to return (0 for all)")
	itemsCmd.Flags().StringVar(&itemsSince, "since", "", "Filter items since date (RFC3339)")
	itemsCmd.Flags().StringVar(&itemsUntil, "until", "", "Filter items until date (RFC3339)")
	itemsCmd.Flags().BoolVar(&itemsUnseen, "unseen", false, "Filter to items with no seen annotation")
	itemsCmd.Flags().BoolVar(&itemsSeen, "seen", false, "Filter to items with a seen annotation")
	itemsCmd.Flags().BoolVar(&itemsMarkSeen, "mark-seen", false, "Mark all returned items as seen")
	rootCmd.AddCommand(itemsCmd)
}

func runItems(_ *cobra.Command, args []string) error {
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
		FeedURL: feedURL,
		Limit:   itemsLimit,
		Since:   since,
		Until:   until,
		Unseen:  itemsUnseen,
		Seen:    itemsSeen,
	}

	items, err := db.GetItems(&filter)
	if err != nil {
		return err
	}

	if itemsSort == sortOldest {
		reverseItemsList(items)
	}

	if itemsMarkSeen {
		for _, item := range items {
			if err := db.AddAnnotation(item.FeedURL, item.GUID, "seen", sql.NullString{}, sql.NullString{}); err != nil {
				return fmt.Errorf("failed to mark seen: %w", err)
			}
		}
	}

	format := itemsFormat
	if format == formatTable && cfg.JSON {
		format = formatJSON
	}

	return outputItemsInFormat(format, items)
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

func outputItemsInFormat(format string, items []*database.Item) error {
	switch format {
	case formatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(items)
	case formatCSV:
		return outputCSV(items) // Reuse existing outputCSV
	case formatTable:
		return outputTable(items) // Reuse existing outputTable
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}
