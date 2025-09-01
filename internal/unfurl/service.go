package unfurl

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/httpclient"
	"github.com/sirupsen/logrus"
)

const jsonFormat = "json"

// Service handles URL metadata operations.
type Service struct {
	db       *database.DB
	unfurler *Unfurler
}

// NewService creates a new unfurl service.
func NewService(db *database.DB, httpClient *httpclient.Client) *Service {
	return &Service{
		db:       db,
		unfurler: NewUnfurler(httpClient),
	}
}

// ProcessSingleURL processes a single URL for metadata extraction.
//
//nolint:cyclop // Complex URL processing logic
func (s *Service) ProcessSingleURL(
	targetURL, format string, retryAfter time.Duration, retryImmediate, skipRobots bool,
) error {
	// Validate URL
	if _, err := url.Parse(targetURL); err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Check if we already have metadata
	logrus.Debugf("Checking for existing metadata for URL: %s", targetURL)
	existing, err := s.db.GetMetadata(targetURL)
	if err != nil {
		logrus.Debugf("Database query failed for %s: %v", targetURL, err)
		return fmt.Errorf("failed to check existing metadata: %w", err)
	}

	var metadata *database.URLMetadata

	if existing != nil {
		logrus.Debugf("Found existing metadata for %s: status=%v, last_fetch=%v",
			targetURL, existing.FetchStatusCode, existing.LastFetchAt)
	} else {
		logrus.Debugf("No existing metadata found for %s", targetURL)
	}

	//nolint:nestif // Complex condition check is necessary
	if existing != nil && existing.FetchStatusCode.Valid &&
		existing.FetchStatusCode.Int64 >= 200 && existing.FetchStatusCode.Int64 < 300 {
		// Use existing successful metadata
		logrus.Debugf("Using cached successful metadata for %s (status: %d)",
			targetURL, existing.FetchStatusCode.Int64)
		metadata = existing
		if format == jsonFormat {
			logrus.Debugf("Using cached metadata for %s", targetURL)
		}
	} else if existing != nil && !retryImmediate && !existing.ShouldRetryFetch(retryAfter) {
		// Previous failure, not time to retry yet (unless retryImmediate is true)
		logrus.Debugf("Skipping retry for %s - not enough time elapsed (retry after %v)",
			targetURL, retryAfter)
		if format == jsonFormat {
			return json.NewEncoder(os.Stdout).Encode(existing)
		}
		logrus.Debugf("Previous fetch failed, retry after %v", retryAfter)
		return nil
	} else {
		// Fetch fresh metadata
		if retryImmediate {
			logrus.Debugf("Force retrying %s due to --retry-immediate flag", targetURL)
		}
		logrus.Debugf("Starting unfurl process for %s", targetURL)
		if format != jsonFormat {
			logrus.Debugf("Fetching metadata for %s...", targetURL)
		}

		result, err := s.unfurler.UnfurlWithOptions(targetURL, skipRobots)
		statusCode := 0
		if err != nil {
			statusCode = extractStatusCodeFromError(err)
			logrus.Debugf("Unfurl failed for %s: error=%v, extracted_status=%d",
				targetURL, err, statusCode)
			if format != jsonFormat {
				logrus.Errorf("Failed to fetch metadata: %v", err)
			}
		} else {
			logrus.Debugf("Unfurl succeeded for %s: title='%s', has_image=%v",
				targetURL, result.Title, result.ImageURL != "")
		}

		// Convert to database model
		logrus.Debugf("Converting unfurl result to database model for %s", targetURL)
		metadata, err = s.unfurler.ToURLMetadata(targetURL, result, statusCode, err)
		if err != nil {
			logrus.Debugf("Failed to convert metadata for %s: %v", targetURL, err)
			return fmt.Errorf("failed to convert metadata: %w", err)
		}

		// Store in database
		logrus.Debugf("Storing metadata in database for %s", targetURL)
		if err := s.db.UpsertMetadata(metadata); err != nil {
			logrus.Debugf("Database storage failed for %s: %v", targetURL, err)
			return fmt.Errorf("failed to store metadata: %w", err)
		}
		logrus.Debugf("Successfully stored metadata for %s", targetURL)

		if format != jsonFormat && result != nil {
			logrus.Debug("Successfully fetched metadata:")
			logrus.Debugf("  Title: %s", result.Title)
			logrus.Debugf("  Description: %s", truncateString(result.Description, 100))
			logrus.Debugf("  Image: %s", result.ImageURL)
			logrus.Debugf("  Favicon: %s", result.FaviconURL)
		}
	}

	// Output JSON if requested
	if format == jsonFormat {
		return json.NewEncoder(os.Stdout).Encode(metadata)
	}

	return nil
}

// ProcessBatchURLs processes multiple URLs concurrently.
//
//nolint:funlen // Long function needed for batch processing logic
func (s *Service) ProcessBatchURLs(
	limit int, retryAfter time.Duration, concurrency int, retryImmediate, skipRobots bool,
) error {
	// Get URLs that need fetching
	var urls []string
	var err error
	if retryImmediate {
		// When retry immediate is enabled, use 0 duration to get all failed URLs
		urls, err = s.db.GetURLsNeedingFetch(limit, 0)
	} else {
		urls, err = s.db.GetURLsNeedingFetch(limit, retryAfter)
	}
	if err != nil {
		return fmt.Errorf("failed to get URLs needing fetch: %w", err)
	}

	if len(urls) == 0 {
		logrus.Info("No URLs need metadata fetching")
		return nil
	}

	logrus.Infof("Found %d URLs needing metadata fetching", len(urls))

	// Set up worker pool
	semaphore := make(chan struct{}, concurrency)
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
			result, fetchErr := s.unfurler.UnfurlWithOptions(url, skipRobots)
			statusCode := 0
			if fetchErr != nil {
				statusCode = extractStatusCodeFromError(fetchErr)
			}

			// Convert to database model
			metadata, err := s.unfurler.ToURLMetadata(url, result, statusCode, fetchErr)
			if err != nil {
				logrus.WithError(err).Warnf("Failed to convert metadata for %s", url)
				mu.Lock()
				failed++
				processed++
				mu.Unlock()
				return
			}

			// Store in database
			if err := s.db.UpsertMetadata(metadata); err != nil {
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
				logrus.Infof("Progress: %d/%d processed (%d successful, %d failed)",
					processed, len(urls), successful, failed)
			}
			mu.Unlock()
		}(targetURL, i)
	}

	// Wait for all workers to complete
	wg.Wait()

	logrus.Infof("Batch unfurl complete: %d URLs processed (%d successful, %d failed)",
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
