package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/httpclient"
	"github.com/lmorchard/feedspool-go/internal/unfurl"
	"github.com/sirupsen/logrus"
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

	// Create HTTP client and unfurler
	httpClient := httpclient.NewClient(&httpclient.Config{
		UserAgent:       httpclient.DefaultUserAgent,
		Timeout:         cfg.Timeout,
		MaxResponseSize: httpclient.MaxResponseSize,
	})
	unfurler := unfurl.NewUnfurler(httpClient)

	if len(args) == 1 {
		// Single URL mode
		return runSingleURLUnfurl(db, unfurler, args[0])
	}

	// Batch mode
	return runBatchUnfurl(db, unfurler)
}

func runSingleURLUnfurl(db *database.DB, unfurler *unfurl.Unfurler, targetURL string) error {
	// Validate URL
	if _, err := url.Parse(targetURL); err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Check if we already have metadata
	existing, err := db.GetMetadata(targetURL)
	if err != nil {
		return fmt.Errorf("failed to check existing metadata: %w", err)
	}

	var metadata *database.URLMetadata

	if existing != nil && existing.FetchStatusCode.Valid &&
		existing.FetchStatusCode.Int64 >= 200 && existing.FetchStatusCode.Int64 < 300 {
		// Use existing successful metadata
		metadata = existing
		if unfurlFormat == "json" {
			fmt.Printf("Using cached metadata for %s\n", targetURL)
		}
	} else if existing != nil && !existing.ShouldRetryFetch(unfurlRetryAfter) {
		// Previous failure, not time to retry yet
		if unfurlFormat == "json" {
			return json.NewEncoder(os.Stdout).Encode(existing)
		}
		fmt.Printf("Previous fetch failed, retry after %v\n", unfurlRetryAfter)
		return nil
	} else {
		// Fetch fresh metadata
		if unfurlFormat != "json" {
			fmt.Printf("Fetching metadata for %s...\n", targetURL)
		}

		result, err := unfurler.Unfurl(targetURL)
		statusCode := 0
		if err != nil {
			statusCode = extractStatusCodeFromError(err)
			if unfurlFormat != "json" {
				fmt.Printf("Failed to fetch metadata: %v\n", err)
			}
		}

		// Convert to database model
		metadata, err = unfurler.ToURLMetadata(targetURL, result, statusCode, err)
		if err != nil {
			return fmt.Errorf("failed to convert metadata: %w", err)
		}

		// Store in database
		if err := db.UpsertMetadata(metadata); err != nil {
			return fmt.Errorf("failed to store metadata: %w", err)
		}

		if unfurlFormat != "json" && result != nil {
			fmt.Printf("Successfully fetched metadata:\n")
			fmt.Printf("  Title: %s\n", result.Title)
			fmt.Printf("  Description: %s\n", truncateString(result.Description, 100))
			fmt.Printf("  Image: %s\n", result.ImageURL)
			fmt.Printf("  Favicon: %s\n", result.FaviconURL)
		}
	}

	// Output JSON if requested
	if unfurlFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(metadata)
	}

	return nil
}

func runBatchUnfurl(db *database.DB, unfurler *unfurl.Unfurler) error {
	// Get URLs that need fetching
	urls, err := db.GetURLsNeedingFetch(unfurlLimit, unfurlRetryAfter)
	if err != nil {
		return fmt.Errorf("failed to get URLs needing fetch: %w", err)
	}

	if len(urls) == 0 {
		fmt.Println("No URLs need metadata fetching")
		return nil
	}

	fmt.Printf("Found %d URLs needing metadata fetching\n", len(urls))

	// Set up worker pool
	semaphore := make(chan struct{}, unfurlConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	processed := 0
	successful := 0
	failed := 0

	// Process URLs concurrently
	for i, targetURL := range urls {
		wg.Add(1)
		go func(url string, _ int) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Fetch metadata
			result, fetchErr := unfurler.Unfurl(url)
			statusCode := 0
			if fetchErr != nil {
				statusCode = extractStatusCodeFromError(fetchErr)
			}

			// Convert to database model
			metadata, err := unfurler.ToURLMetadata(url, result, statusCode, fetchErr)
			if err != nil {
				logrus.WithError(err).Warnf("Failed to convert metadata for %s", url)
				mu.Lock()
				failed++
				processed++
				mu.Unlock()
				return
			}

			// Store in database
			if err := db.UpsertMetadata(metadata); err != nil {
				logrus.WithError(err).Warnf("Failed to store metadata for %s", url)
				mu.Lock()
				failed++
				processed++
				mu.Unlock()
				return
			}

			mu.Lock()
			processed++
			if fetchErr == nil {
				successful++
			} else {
				failed++
			}

			// Progress update every 10 items
			if processed%10 == 0 {
				fmt.Printf("Progress: %d/%d processed (%d successful, %d failed)\n",
					processed, len(urls), successful, failed)
			}
			mu.Unlock()
		}(targetURL, i)
	}

	// Wait for all workers to complete
	wg.Wait()

	fmt.Printf("Batch unfurl complete: %d URLs processed (%d successful, %d failed)\n",
		processed, successful, failed)

	return nil
}

func extractStatusCodeFromError(err error) int {
	// Try to extract HTTP status code from error message
	if err == nil {
		return 0
	}

	errStr := err.Error()
	if len(errStr) > 5 && errStr[:5] == "HTTP " {
		// Try to parse "HTTP 404" format
		var code int
		if n, _ := fmt.Sscanf(errStr, "HTTP %d", &code); n == 1 {
			return code
		}
	}

	return 0 // Unknown error
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
