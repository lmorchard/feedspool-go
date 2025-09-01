package cmd

import (
	"fmt"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/httpclient"
	"github.com/lmorchard/feedspool-go/internal/unfurl"
	"github.com/spf13/cobra"
)

var (
	unfurlLimit       int
	unfurlFormat      string
	unfurlConcurrency int
	unfurlRetryAfter  time.Duration
)

var unfurlCmd = &cobra.Command{
	Use:   "unfurl [URL]",
	Short: "Fetch and extract metadata from URLs",
	Long: `Unfurl command operates in two modes:

Single URL:
  feedspool unfurl <url>                     # Fetch metadata for specific URL
  feedspool unfurl <url> --format json      # Output as JSON to stdout

Batch mode:
  feedspool unfurl                           # Process all item URLs without metadata
  feedspool unfurl --limit 100              # Process only 100 URLs

The command extracts OpenGraph metadata, Twitter Cards, and favicons from web pages.
It respects robots.txt and caches results in the database for efficient reuse.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runUnfurl,
}

func init() {
	unfurlCmd.Flags().IntVar(&unfurlLimit, "limit", 0, "Maximum URLs to process in batch mode")
	unfurlCmd.Flags().StringVar(&unfurlFormat, "format", "", "Output format for single URL (json)")
	unfurlCmd.Flags().IntVar(&unfurlConcurrency, "concurrency", config.DefaultConcurrency, "Maximum concurrent fetches")
	unfurlCmd.Flags().DurationVar(&unfurlRetryAfter, "retry-after", 1*time.Hour,
		"Retry failed fetches after this duration")
	rootCmd.AddCommand(unfurlCmd)
}

func runUnfurl(_ *cobra.Command, args []string) error {
	cfg := GetConfig()

	db, err := database.New(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	if err := db.IsInitialized(); err != nil {
		return err
	}

	// Create HTTP client
	httpClient := httpclient.NewClient(&httpclient.Config{
		UserAgent:       httpclient.DefaultUserAgent,
		Timeout:         cfg.Timeout,
		MaxResponseSize: httpclient.MaxResponseSize,
	})

	if len(args) == 1 {
		// Single URL mode
		return runSingleURLUnfurl(db, httpClient, args[0])
	}

	// Batch mode
	return runBatchUnfurl(db, httpClient)
}

func runSingleURLUnfurl(db *database.DB, httpClient *httpclient.Client, targetURL string) error {
	service := unfurl.NewService(db, httpClient)
	return service.ProcessSingleURL(targetURL, unfurlFormat, unfurlRetryAfter)
}

func runBatchUnfurl(db *database.DB, httpClient *httpclient.Client) error {
	service := unfurl.NewService(db, httpClient)
	return service.ProcessBatchURLs(unfurlLimit, unfurlRetryAfter, unfurlConcurrency)
}
