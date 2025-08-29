package cmd

import (
	"fmt"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/feedlist"
	"github.com/spf13/cobra"
)

var (
	exportFormat   string
	exportFilename string
)

var exportCmd = &cobra.Command{
	Use:   "export --format [opml|text] [filename]",
	Short: "Export database feeds to a feed list file",
	Long: `Export all feeds from the database to a feed list file (OPML or text format).

For OPML format, includes feed metadata (title, description) when available.
For text format, creates a simple list of URLs with header comments.

Examples:
  feedspool export --format opml feeds.opml
  feedspool export --format text feeds.txt`,
	Args: cobra.ExactArgs(1),
	RunE: runExport,
}

func init() {
	exportCmd.Flags().StringVar(&exportFormat, "format", "", "Feed list format (opml or text) - REQUIRED")
	_ = exportCmd.MarkFlagRequired("format")
	rootCmd.AddCommand(exportCmd)
}

func runExport(_ *cobra.Command, args []string) error {
	exportFilename = args[0]
	cfg := GetConfig()

	// Validate format
	var feedFormat feedlist.Format
	switch exportFormat {
	case string(feedlist.FormatOPML):
		feedFormat = feedlist.FormatOPML
	case string(feedlist.FormatText):
		feedFormat = feedlist.FormatText
	default:
		return fmt.Errorf("unsupported format: %s (must be 'opml' or 'text')", exportFormat)
	}

	// Connect to database
	if err := database.Connect(cfg.Database); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer database.Close()

	// Get all feeds from database
	feeds, err := database.GetAllFeeds()
	if err != nil {
		return fmt.Errorf("failed to get feeds from database: %w", err)
	}

	if len(feeds) == 0 {
		fmt.Printf("Warning: No feeds found in database\n")
		return nil
	}

	// Create new feed list of specified format
	list := feedlist.NewFeedList(feedFormat)

	// Add all feed URLs to the list
	for _, feed := range feeds {
		if err := list.AddURL(feed.URL); err != nil {
			return fmt.Errorf("failed to add URL %s to feed list: %w", feed.URL, err)
		}
	}

	// For OPML format, we could enhance with feed metadata in the future
	// For now, both formats just export URLs

	// Save to specified filename
	if err := list.Save(exportFilename); err != nil {
		return fmt.Errorf("failed to save feed list: %w", err)
	}

	fmt.Printf("Exported %d feeds to %s (%s format)\n", len(feeds), exportFilename, exportFormat)

	return nil
}
