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
	unfurlLimit          int
	unfurlFormat         string
	unfurlConcurrency    int
	unfurlRetryAfter     time.Duration
	unfurlRetryImmediate bool
	unfurlSkipRobots     bool
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
  feedspool unfurl --retry-immediate         # Retry all failed URLs immediately
  feedspool unfurl --skip-robots             # Skip robots.txt checking

The command extracts OpenGraph metadata, Twitter Cards, and favicons from web pages.
By default it respects robots.txt, but this can be disabled with --skip-robots.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runUnfurl,
}

func init() {
	unfurlCmd.Flags().IntVar(&unfurlLimit, "limit", 0, "Maximum URLs to process in batch mode")
	unfurlCmd.Flags().StringVar(&unfurlFormat, "format", "", "Output format for single URL (json)")
	unfurlCmd.Flags().IntVar(&unfurlConcurrency, "concurrency", config.DefaultConcurrency, "Maximum concurrent fetches")
	unfurlCmd.Flags().DurationVar(&unfurlRetryAfter, "retry-after", 1*time.Hour,
		"Retry failed fetches after this duration")
	unfurlCmd.Flags().BoolVar(&unfurlRetryImmediate, "retry-immediate", false,
		"Retry failed URLs immediately, ignoring retry delay")
	unfurlCmd.Flags().BoolVar(&unfurlSkipRobots, "skip-robots", false,
		"Skip robots.txt checking when fetching URLs")
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
		return runSingleURLUnfurl(db, httpClient, args[0], cfg)
	}

	// Batch mode
	return runBatchUnfurl(db, httpClient, cfg)
}

func runSingleURLUnfurl(db *database.DB, httpClient *httpclient.Client, targetURL string, cfg *config.Config) error {
	service := unfurl.NewService(db, httpClient)
	// Use CLI flag if set, otherwise fall back to config
	skipRobots := unfurlSkipRobots || cfg.Unfurl.SkipRobots
	retryAfter := unfurlRetryAfter
	if retryAfter == 1*time.Hour && cfg.Unfurl.RetryAfter > 0 {
		retryAfter = cfg.Unfurl.RetryAfter
	}
	return service.ProcessSingleURL(targetURL, unfurlFormat, retryAfter, unfurlRetryImmediate, skipRobots)
}

func runBatchUnfurl(db *database.DB, httpClient *httpclient.Client, cfg *config.Config) error {
	service := unfurl.NewService(db, httpClient)
	// Use CLI flag if set, otherwise fall back to config
	skipRobots := unfurlSkipRobots || cfg.Unfurl.SkipRobots
	retryAfter := unfurlRetryAfter
	if retryAfter == 1*time.Hour && cfg.Unfurl.RetryAfter > 0 {
		retryAfter = cfg.Unfurl.RetryAfter
	}
	concurrency := unfurlConcurrency
	if concurrency == config.DefaultConcurrency && cfg.Unfurl.Concurrency > 0 {
		concurrency = cfg.Unfurl.Concurrency
	}
	return service.ProcessBatchURLs(unfurlLimit, retryAfter, concurrency, unfurlRetryImmediate, skipRobots)
}
