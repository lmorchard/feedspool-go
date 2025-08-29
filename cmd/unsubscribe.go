package cmd

import (
	"fmt"

	"github.com/lmorchard/feedspool-go/internal/subscription"
	"github.com/spf13/cobra"
)

var (
	unsubscribeFormat   string
	unsubscribeFilename string
)

var unsubscribeCmd = &cobra.Command{
	Use:   "unsubscribe [URL]",
	Short: "Unsubscribe from a feed by removing it from a feed list",
	Long: `Unsubscribe from a feed by removing its URL from a feed list (OPML or text format).

Examples:
  feedspool unsubscribe https://example.com/feed.xml
  feedspool unsubscribe --format text --filename feeds.txt https://example.com/feed.xml`,
	Args: cobra.ExactArgs(1),
	RunE: runUnsubscribe,
}

func init() {
	unsubscribeCmd.Flags().StringVar(&unsubscribeFormat, "format", "", "Feed list format (opml or text)")
	unsubscribeCmd.Flags().StringVar(&unsubscribeFilename, "filename", "", "Feed list filename")
	rootCmd.AddCommand(unsubscribeCmd)
}

func runUnsubscribe(_ *cobra.Command, args []string) error {
	targetURL := args[0]
	cfg := GetConfig()

	manager := subscription.New(cfg)

	format, filename, err := manager.ResolveFormatAndFilename(unsubscribeFormat, unsubscribeFilename)
	if err != nil {
		return err
	}

	result, err := manager.Unsubscribe(format, filename, targetURL)
	if err != nil {
		return err
	}

	if !result.Found {
		fmt.Printf("Feed URL not found in list: %s\n", targetURL)
		return nil
	}

	if result.Removed {
		fmt.Printf("Removed feed from %s: %s\n", filename, targetURL)
	}

	return nil
}
