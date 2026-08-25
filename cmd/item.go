package cmd

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/spf13/cobra"
)

var itemFormat string

var itemCmd = &cobra.Command{
	Use:   "item <link>",
	Short: "Show details for a single item",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return runItem(args[0])
	},
}

func init() {
	itemCmd.Flags().StringVar(&itemFormat, "format", formatTable, "Output format (table|json)")
	rootCmd.AddCommand(itemCmd)
}

type ItemOutput struct {
	*database.Item
	Annotations []database.ItemAnnotation `json:"Annotations"`
}

func runItem(link string) error {
	cfg := GetConfig()
	db, err := database.New(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	if err := db.IsInitialized(); err != nil {
		return err
	}

	// Fetch item
	query := `
		SELECT feed_url, guid, title, link, summary, published_date, first_seen
		FROM items
		WHERE link = ?
		LIMIT 1
	`
	var item database.Item
	err = db.GetConnection().QueryRow(query, link).Scan(
		&item.FeedURL, &item.GUID, &item.Title, &item.Link, &item.Summary,
		&item.PublishedDate, &item.FirstSeen,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("item with link %q not found", link)
	} else if err != nil {
		return fmt.Errorf("failed to look up item: %w", err)
	}

	annotations, err := db.GetAnnotations(item.FeedURL, item.GUID)
	if err != nil {
		return err
	}

	format := itemFormat
	if format == formatTable && cfg.JSON {
		format = formatJSON
	}

	switch format {
	case formatJSON:
		out := ItemOutput{
			Item:        &item,
			Annotations: annotations,
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(out)
	case formatTable:
		//nolint:forbidigo
		fmt.Printf("Title: %s\n", item.Title)
		//nolint:forbidigo
		fmt.Printf("Link: %s\n", item.Link)
		//nolint:forbidigo
		fmt.Printf("Feed URL: %s\n", item.FeedURL)
		//nolint:forbidigo
		fmt.Printf("Date: %s\n", item.PublishedDate.Format(time.RFC3339))
		if item.FirstSeen.Valid {
			//nolint:forbidigo
			fmt.Printf("First Seen: %s\n", item.FirstSeen.Time.Format(time.RFC3339))
		}
		//nolint:forbidigo
		fmt.Printf("\nAnnotations:\n")
		if len(annotations) == 0 {
			//nolint:forbidigo
			fmt.Println("  (none)")
		} else {
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, a := range annotations {
				val := ""
				if a.Value.Valid {
					val = a.Value.String
				}
				fmt.Fprintf(w, "  %s\t%s\n", a.Kind, val)
			}
			w.Flush()
		}
		return nil
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}
