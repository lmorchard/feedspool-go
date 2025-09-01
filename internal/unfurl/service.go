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
func (s *Service) ProcessSingleURL(targetURL, format string, retryAfter time.Duration) error { //nolint:cyclop
	// Validate URL
	if _, err := url.Parse(targetURL); err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Check if we already have metadata
	existing, err := s.db.GetMetadata(targetURL)
	if err != nil {
		return fmt.Errorf("failed to check existing metadata: %w", err)
	}

	var metadata *database.URLMetadata

	//nolint:nestif // Complex condition check is necessary
	if existing != nil && existing.FetchStatusCode.Valid &&
		existing.FetchStatusCode.Int64 >= 200 && existing.FetchStatusCode.Int64 < 300 {
		// Use existing successful metadata
		metadata = existing
		if format == jsonFormat {
			logrus.Infof("Using cached metadata for %s", targetURL)
		}
	} else if existing != nil && !existing.ShouldRetryFetch(retryAfter) {
		// Previous failure, not time to retry yet
		if format == jsonFormat {
			return json.NewEncoder(os.Stdout).Encode(existing)
		}
		logrus.Infof("Previous fetch failed, retry after %v", retryAfter)
		return nil
	} else {
		// Fetch fresh metadata
		if format != jsonFormat {
			logrus.Infof("Fetching metadata for %s...", targetURL)
		}

		result, err := s.unfurler.Unfurl(targetURL)
		statusCode := 0
		if err != nil {
			statusCode = extractStatusCodeFromError(err)
			if format != jsonFormat {
				logrus.Errorf("Failed to fetch metadata: %v", err)
			}
		}

		// Convert to database model
		metadata, err = s.unfurler.ToURLMetadata(targetURL, result, statusCode, err)
		if err != nil {
			return fmt.Errorf("failed to convert metadata: %w", err)
		}

		// Store in database
		if err := s.db.UpsertMetadata(metadata); err != nil {
			return fmt.Errorf("failed to store metadata: %w", err)
		}

		if format != jsonFormat && result != nil {
			logrus.Info("Successfully fetched metadata:")
			logrus.Infof("  Title: %s", result.Title)
			logrus.Infof("  Description: %s", truncateString(result.Description, 100))
			logrus.Infof("  Image: %s", result.ImageURL)
			logrus.Infof("  Favicon: %s", result.FaviconURL)
		}
	}

	// Output JSON if requested
	if format == jsonFormat {
		return json.NewEncoder(os.Stdout).Encode(metadata)
	}

	return nil
}

// ProcessBatchURLs processes multiple URLs concurrently.
func (s *Service) ProcessBatchURLs(limit int, retryAfter time.Duration, concurrency int) error {
	// Get URLs that need fetching
	urls, err := s.db.GetURLsNeedingFetch(limit, retryAfter)
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
			result, fetchErr := s.unfurler.Unfurl(url)
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
