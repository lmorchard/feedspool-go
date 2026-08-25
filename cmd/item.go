package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/spf13/cobra"
)

var (
	itemFormat string
	itemFeed   string
	itemGUID   string
)

var itemCmd = &cobra.Command{
	Use:   "item [link]",
	Short: "Show details for a single item",
	Long: `Show a single item selected by link, including stored unfurl metadata
and annotations. A missing or ambiguous link exits non-zero; ambiguity errors
identify each matching feed URL and GUID. Use --feed and --guid together to
select an item whose link is ambiguous.`,
	Example: `  feedspool item https://example.com/posts/one
  feedspool item --feed https://example.com/feed.xml --guid post-one
  feedspool --json item https://example.com/posts/one`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return runItem(args)
	},
}

func init() {
	itemCmd.Flags().StringVar(&itemFormat, "format", formatTable, "Output format (table|json)")
	itemCmd.Flags().StringVar(&itemFeed, "feed", "", "Select by exact feed URL (requires --guid)")
	itemCmd.Flags().StringVar(&itemGUID, "guid", "", "Select by exact item GUID (requires --feed)")
	rootCmd.AddCommand(itemCmd)
}

type ItemOutput struct {
	*database.Item
	Annotations []database.ItemAnnotation `json:"Annotations"`
	Metadata    *database.URLMetadata     `json:"Metadata,omitempty"`
}

type itemSelector struct {
	link    string
	feedURL string
	guid    string
}

func runItem(args []string) error {
	selector, err := parseItemSelector(args)
	if err != nil {
		return err
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

	output, err := getItemOutput(db, selector)
	if err != nil {
		return err
	}

	format := itemFormat
	if format == formatTable && cfg.JSON {
		format = formatJSON
	}
	return outputItem(format, output)
}

func parseItemSelector(args []string) (itemSelector, error) {
	if len(args) == 1 {
		if itemFeed != "" || itemGUID != "" {
			return itemSelector{}, fmt.Errorf("link cannot be combined with --feed or --guid")
		}
		return itemSelector{link: args[0]}, nil
	}
	if itemFeed == "" && itemGUID == "" {
		return itemSelector{}, fmt.Errorf("provide a link or both --feed and --guid")
	}
	if itemFeed == "" || itemGUID == "" {
		return itemSelector{}, fmt.Errorf("--feed and --guid must be used together")
	}
	return itemSelector{feedURL: itemFeed, guid: itemGUID}, nil
}

func getItemOutput(db *database.DB, selector itemSelector) (*ItemOutput, error) {
	query := `
		SELECT id, feed_url, guid, title, link, published_date, first_seen,
			content, summary, archived, item_json
		FROM items
	`
	var args []any
	if selector.link != "" {
		query += " WHERE link = ? ORDER BY feed_url"
		args = append(args, selector.link)
	} else {
		query += " WHERE feed_url = ? AND guid = ?"
		args = append(args, selector.feedURL, selector.guid)
	}
	rows, err := db.GetConnection().Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to look up item: %w", err)
	}
	defer rows.Close()

	var items []database.Item
	for rows.Next() {
		var item database.Item
		if err := rows.Scan(
			&item.ID, &item.FeedURL, &item.GUID, &item.Title, &item.Link,
			&item.PublishedDate, &item.FirstSeen, &item.Content, &item.Summary,
			&item.Archived, &item.ItemJSON,
		); err != nil {
			return nil, fmt.Errorf("failed to scan item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate matching items: %w", err)
	}
	if len(items) == 0 {
		if selector.link != "" {
			return nil, fmt.Errorf("item with link %q not found", selector.link)
		}
		return nil, fmt.Errorf("item with feed %q and GUID %q not found", selector.feedURL, selector.guid)
	}
	if len(items) > 1 {
		matches := make([]string, 0, len(items))
		for i := range items {
			matches = append(matches, fmt.Sprintf("%s (%s)", items[i].FeedURL, items[i].GUID))
		}
		return nil, fmt.Errorf(
			"item link %q is ambiguous; matching items: %s; select one with --feed and --guid",
			selector.link, strings.Join(matches, ", "),
		)
	}
	item := items[0]

	annotations, err := db.GetAnnotations(item.FeedURL, item.GUID)
	if err != nil {
		return nil, err
	}
	metadata, err := db.GetMetadata(item.Link)
	if err != nil {
		return nil, err
	}
	return &ItemOutput{Item: &item, Annotations: annotations, Metadata: metadata}, nil
}

func outputItem(format string, output *ItemOutput) error {
	switch format {
	case formatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)
	case formatTable:
		return outputItemTable(output)
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}

func outputItemTable(output *ItemOutput) error {
	item := output.Item
	//nolint:forbidigo // Command output.
	fmt.Printf("Title: %s\nLink: %s\nFeed URL: %s\nDate: %s\nSummary: %s\nContent: %s\n",
		item.Title, item.Link, item.FeedURL, item.PublishedDate.Format(time.RFC3339), item.Summary, item.Content)
	if item.FirstSeen.Valid {
		//nolint:forbidigo // Command output.
		fmt.Printf("First Seen: %s\n", item.FirstSeen.Time.Format(time.RFC3339))
	}
	outputItemAnnotations(output.Annotations)
	outputItemMetadata(output.Metadata)
	return nil
}

func outputItemAnnotations(annotations []database.ItemAnnotation) {
	//nolint:forbidigo // Command output.
	fmt.Printf("\nAnnotations:\n")
	if len(annotations) == 0 {
		//nolint:forbidigo // Command output.
		fmt.Println("  (none)")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, annotation := range annotations {
		value := ""
		if annotation.Value.Valid {
			value = annotation.Value.String
		}
		fmt.Fprintf(w, "  %s\t%s\n", annotation.Kind, value)
	}
	_ = w.Flush()
}

func outputItemMetadata(metadata *database.URLMetadata) {
	//nolint:forbidigo // Command output.
	fmt.Printf("\nUnfurl metadata:\n")
	if metadata == nil {
		//nolint:forbidigo // Command output.
		fmt.Println("  (none)")
		return
	}
	printMetadataField("Title", metadata.Title)
	printMetadataField("Description", metadata.Description)
	printMetadataField("Image", metadata.ImageURL)
	printMetadataField("Favicon", metadata.FaviconURL)
	if len(metadata.Metadata) > 0 && string(metadata.Metadata) != "null" {
		//nolint:forbidigo // Command output.
		fmt.Printf("  Raw: %s\n", string(metadata.Metadata))
	}
}

func printMetadataField(label string, value sql.NullString) {
	if value.Valid {
		//nolint:forbidigo // Command output.
		fmt.Printf("  %s: %s\n", label, value.String)
	}
}
