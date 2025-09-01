package fetcher

import (
	"context"
	"fmt"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/feedlist"
	"github.com/lmorchard/feedspool-go/internal/unfurl"
	"github.com/sirupsen/logrus"
)

// FetchOptions contains all configuration options for fetch operations.
type FetchOptions struct {
	Timeout       time.Duration
	MaxItems      int
	MaxAge        time.Duration
	Force         bool
	Concurrency   int
	WithUnfurl    bool
	RemoveMissing bool
}

// Orchestrator handles high-level fetch operations with unfurl integration.
type Orchestrator struct {
	db     *database.DB
	config *config.Config
}

// NewOrchestrator creates a new fetch orchestrator.
func NewOrchestrator(db *database.DB, cfg *config.Config) *Orchestrator {
	return &Orchestrator{
		db:     db,
		config: cfg,
	}
}

// FetchSingle executes a single URL fetch with optional unfurl.
func (o *Orchestrator) FetchSingle(ctx context.Context, feedURL string, opts FetchOptions) (*FetchResult, error) {
	unfurlQueue := o.createUnfurlQueue(ctx, opts.WithUnfurl)
	defer o.cleanupUnfurlQueue(ctx, unfurlQueue)

	fetcher := NewFetcher(o.db, opts.Timeout, opts.MaxItems, opts.Force)
	if unfurlQueue != nil {
		fetcher.SetUnfurlQueue(unfurlQueue)
	}

	result := fetcher.FetchFeed(feedURL)

	o.awaitUnfurlCompletion(ctx, unfurlQueue)

	return result, result.Error
}

// FetchFromFile executes fetch from a feed list file with optional unfurl.
func (o *Orchestrator) FetchFromFile(
	ctx context.Context, format feedlist.Format, filename string, opts FetchOptions,
) ([]*FetchResult, error) {
	// Load feed URLs from file
	list, err := feedlist.LoadFeedList(format, filename)
	if err != nil {
		return nil, fmt.Errorf("failed to load feed list %s: %w", filename, err)
	}

	feedURLs := list.GetURLs()
	if len(feedURLs) == 0 {
		logrus.Infof("No feed URLs found in %s", filename)
		return []*FetchResult{}, nil
	}

	if opts.WithUnfurl {
		logrus.Infof("Found %d feeds in %s - fetching with parallel unfurl", len(feedURLs), filename)
	} else {
		logrus.Infof("Found %d feeds in %s", len(feedURLs), filename)
	}

	results := o.fetchConcurrentWithUnfurl(ctx, feedURLs, opts)

	// Handle feed removal if requested
	if opts.RemoveMissing {
		removedCount := o.removeMissingFeeds(feedURLs)
		if removedCount > 0 {
			logrus.Infof("Removed %d feeds not in list", removedCount)
		}
	}

	return results, nil
}

// FetchFromDatabase executes fetch from all feeds in database with optional unfurl.
func (o *Orchestrator) FetchFromDatabase(ctx context.Context, opts FetchOptions) ([]*FetchResult, error) {
	// Get all feeds from database
	dbFeeds, err := o.db.GetAllFeeds()
	if err != nil {
		return nil, fmt.Errorf("failed to get feeds from database: %w", err)
	}

	if len(dbFeeds) == 0 {
		logrus.Info("No feeds in database")
		return []*FetchResult{}, nil
	}

	// Extract URLs
	feedURLs := make([]string, 0, len(dbFeeds))
	for _, feed := range dbFeeds {
		feedURLs = append(feedURLs, feed.URL)
	}

	if opts.WithUnfurl {
		logrus.Infof("Fetching %d feeds from database with parallel unfurl", len(feedURLs))
	} else {
		logrus.Infof("Fetching %d feeds from database", len(feedURLs))
	}

	return o.fetchConcurrentWithUnfurl(ctx, feedURLs, opts), nil
}

// fetchConcurrentWithUnfurl handles concurrent fetching with unfurl integration.
func (o *Orchestrator) fetchConcurrentWithUnfurl(
	ctx context.Context, feedURLs []string, opts FetchOptions,
) []*FetchResult {
	unfurlQueue := o.createUnfurlQueue(ctx, opts.WithUnfurl)
	defer o.cleanupUnfurlQueue(ctx, unfurlQueue)

	// Fetch feeds concurrently
	results := FetchConcurrentWithUnfurl(
		o.db,
		feedURLs,
		opts.Concurrency,
		opts.Timeout,
		opts.MaxItems,
		opts.MaxAge,
		opts.Force,
		unfurlQueue,
	)

	o.awaitUnfurlCompletion(ctx, unfurlQueue)

	return results
}

// createUnfurlQueue creates and starts an unfurl queue if needed.
func (o *Orchestrator) createUnfurlQueue(ctx context.Context, withUnfurl bool) *unfurl.UnfurlQueue {
	if !withUnfurl {
		return nil
	}

	// Validate configuration
	if err := o.validateUnfurlConfig(); err != nil {
		logrus.Errorf("Invalid unfurl configuration: %v", err)
		return nil
	}

	// Use unfurl config settings for concurrency and other options
	concurrency := o.config.Unfurl.Concurrency
	if concurrency <= 0 {
		concurrency = config.DefaultConcurrency
	}

	logrus.Infof("Starting fetch with parallel unfurl (unfurl concurrency: %d)", concurrency)
	if o.config.Unfurl.SkipRobots {
		logrus.Debugf("Unfurl robots.txt checking disabled")
	}
	logrus.Debugf("Unfurl retry after: %v", o.config.Unfurl.RetryAfter)

	queue := unfurl.NewUnfurlQueue(
		ctx,
		o.db,
		concurrency,
		o.config.Unfurl.SkipRobots,
		o.config.Unfurl.RetryAfter,
	)
	queue.Start()

	return queue
}

// awaitUnfurlCompletion waits for unfurl operations to complete.
func (o *Orchestrator) awaitUnfurlCompletion(ctx context.Context, unfurlQueue *unfurl.UnfurlQueue) {
	if unfurlQueue == nil {
		return
	}

	enqueued, processed := unfurlQueue.Stats()
	if enqueued > 0 {
		logrus.Infof("Fetch completed, waiting for %d unfurl operations to complete", enqueued-processed)
	} else {
		logrus.Infof("Fetch completed, no items needed unfurl")
	}
	unfurlQueue.Close()

	// Check for cancellation while waiting
	select {
	case <-ctx.Done():
		logrus.Infof("Shutdown signal received, canceling unfurl operations")
		unfurlQueue.Cancel()
	default:
		unfurlQueue.Wait()
		finalEnqueued, finalProcessed := unfurlQueue.Stats()
		if finalEnqueued > 0 {
			logrus.Infof("All operations completed: %d unfurl operations processed", finalProcessed)
		}
	}
}

// cleanupUnfurlQueue ensures proper cleanup of unfurl queue resources.
func (o *Orchestrator) cleanupUnfurlQueue(ctx context.Context, unfurlQueue *unfurl.UnfurlQueue) {
	if unfurlQueue != nil && ctx.Err() != nil {
		unfurlQueue.Cancel()
	}
}

// validateUnfurlConfig validates the unfurl configuration.
func (o *Orchestrator) validateUnfurlConfig() error {
	if o.config.Unfurl.Concurrency < 0 {
		return fmt.Errorf("unfurl concurrency cannot be negative")
	}
	if o.config.Unfurl.Concurrency > 100 {
		logrus.Warnf("Unfurl concurrency %d is very high, consider reducing to avoid overwhelming servers",
			o.config.Unfurl.Concurrency)
	}
	if o.config.Unfurl.RetryAfter < 0 {
		return fmt.Errorf("unfurl retry_after cannot be negative")
	}
	if o.config.Unfurl.RetryAfter > 24*time.Hour {
		logrus.Warnf("Unfurl retry_after %v is very long, failed URLs will wait a long time before retry",
			o.config.Unfurl.RetryAfter)
	}
	return nil
}

// removeMissingFeeds removes feeds from database that are not in the provided URL list.
func (o *Orchestrator) removeMissingFeeds(feedURLs []string) int {
	existingURLs, err := o.db.GetFeedURLs()
	if err != nil {
		logrus.Warnf("Failed to get existing feeds: %v", err)
		return 0
	}

	urlMap := make(map[string]bool)
	for _, url := range feedURLs {
		urlMap[url] = true
	}

	removedCount := 0
	for _, existingURL := range existingURLs {
		if !urlMap[existingURL] {
			if err := o.db.DeleteFeed(existingURL); err != nil {
				logrus.Warnf("Failed to delete feed %s: %v", existingURL, err)
			} else {
				removedCount++
				logrus.Infof("Removed feed not in list: %s", existingURL)
			}
		}
	}

	return removedCount
}
