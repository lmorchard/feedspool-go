package cmd

import (
	"fmt"
	"net/url"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/feedlist"
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

	if _, err := url.Parse(targetURL); err != nil {
		return fmt.Errorf("invalid URL: %s - %w", targetURL, err)
	}

	format, filename, err := determineUnsubscribeFormatAndFilename(cfg, unsubscribeFormat, unsubscribeFilename)
	if err != nil {
		return err
	}

	feedFormat, err := validateUnsubscribeFormat(format)
	if err != nil {
		return err
	}

	list, err := feedlist.LoadFeedList(feedFormat, filename)
	if err != nil {
		return fmt.Errorf("failed to load feed list %s: %w", filename, err)
	}

	fmt.Printf("Loaded feed list: %s\n", filename)

	return removeURLFromList(list, targetURL, filename)
}

func determineUnsubscribeFormatAndFilename(
	cfg *config.Config, format, filename string,
) (resultFormat, resultFilename string, err error) {
	if format == "" || filename == "" {
		if cfg.HasDefaultFeedList() {
			if format == "" {
				format, _ = cfg.GetDefaultFeedList()
			}
			if filename == "" {
				_, filename = cfg.GetDefaultFeedList()
			}
		} else {
			return "", "", fmt.Errorf("feed list format and filename must be specified " +
				"(use --format and --filename flags or configure defaults)")
		}
	}
	return format, filename, nil
}

func validateUnsubscribeFormat(format string) (feedlist.Format, error) {
	switch format {
	case string(feedlist.FormatOPML):
		return feedlist.FormatOPML, nil
	case string(feedlist.FormatText):
		return feedlist.FormatText, nil
	default:
		return "", fmt.Errorf("unsupported format: %s (must be 'opml' or 'text')", format)
	}
}

func removeURLFromList(list feedlist.FeedList, targetURL, filename string) error {
	existingURLs := list.GetURLs()
	exists := false
	for _, existing := range existingURLs {
		if existing == targetURL {
			exists = true
			break
		}
	}

	if !exists {
		fmt.Printf("Warning: Feed URL not found in list: %s\n", targetURL)
		return nil
	}

	if err := list.RemoveURL(targetURL); err != nil {
		return fmt.Errorf("failed to remove URL %s: %w", targetURL, err)
	}

	fmt.Printf("Removed feed: %s\n", targetURL)

	if err := list.Save(filename); err != nil {
		return fmt.Errorf("failed to save feed list: %w", err)
	}

	fmt.Printf("Updated feed list saved to %s\n", filename)
	return nil
}
