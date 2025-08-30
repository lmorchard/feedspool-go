package fetcher

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/mmcdole/gofeed"
	"github.com/sirupsen/logrus"
)

type FetchResult struct {
	URL       string
	Feed      *database.Feed
	ItemCount int
	Cached    bool
	Error     error
}

type Fetcher struct {
	client    *http.Client
	timeout   time.Duration
	maxItems  int
	forceFlag bool
	db        *database.DB
}

func NewFetcher(db *database.DB, timeout time.Duration, maxItems int, force bool) *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: timeout,
		},
		timeout:   timeout,
		maxItems:  maxItems,
		forceFlag: force,
		db:        db,
	}
}

func (f *Fetcher) FetchFeed(feedURL string) *FetchResult {
	result := &FetchResult{
		URL: feedURL,
	}

	existingFeed, err := f.db.GetFeed(feedURL)
	if err != nil {
		result.Error = fmt.Errorf("failed to check existing feed: %w", err)
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), f.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", feedURL, http.NoBody)
	if err != nil {
		result.Error = fmt.Errorf("failed to create request: %w", err)
		f.updateFeedError(existingFeed, result.Error.Error())
		return result
	}

	if !f.forceFlag && existingFeed != nil {
		if existingFeed.ETag != "" {
			req.Header.Set("If-None-Match", existingFeed.ETag)
		}
		if existingFeed.LastModified != "" {
			req.Header.Set("If-Modified-Since", existingFeed.LastModified)
		}
	}

	resp, err := f.client.Do(req)
	if err != nil {
		result.Error = fmt.Errorf("failed to fetch: %w", err)
		f.updateFeedError(existingFeed, result.Error.Error())
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return f.handleCachedFeed(result, existingFeed)
	}

	if resp.StatusCode != http.StatusOK {
		result.Error = fmt.Errorf("HTTP %d", resp.StatusCode)
		f.updateFeedError(existingFeed, result.Error.Error())
		return result
	}

	parser := gofeed.NewParser()
	gofeedData, err := parser.Parse(resp.Body)
	if err != nil {
		result.Error = fmt.Errorf("failed to parse: %w", err)
		f.updateFeedError(existingFeed, result.Error.Error())
		return result
	}

	return f.processParsedFeed(result, gofeedData, feedURL, resp)
}

func (f *Fetcher) processParsedFeed(
	result *FetchResult, gofeedData *gofeed.Feed, feedURL string, resp *http.Response,
) *FetchResult {
	feed, err := database.FeedFromGofeed(gofeedData, feedURL)
	if err != nil {
		result.Error = fmt.Errorf("failed to convert: %w", err)
		return result
	}

	feed.ETag = resp.Header.Get("ETag")
	feed.LastModified = resp.Header.Get("Last-Modified")
	feed.LastFetchTime = time.Now()
	feed.LastSuccessfulFetch = time.Now()
	feed.ErrorCount = 0
	feed.LastError = ""

	// Save feed first to satisfy foreign key constraints
	if err := f.db.UpsertFeed(feed); err != nil {
		result.Error = fmt.Errorf("failed to save feed: %w", err)
		return result
	}

	// Process items and get the latest item date
	itemCount, latestItemDate := f.processFeedItems(gofeedData, feedURL)
	if !latestItemDate.IsZero() {
		feed.LatestItemDate = latestItemDate
		// Update feed with latest item date
		if err := f.db.UpsertFeed(feed); err != nil {
			logrus.Warnf("Failed to update feed with latest item date: %v", err)
		}
	}

	result.ItemCount = itemCount
	result.Feed = feed
	return result
}

func (f *Fetcher) processFeedItems(gofeedData *gofeed.Feed, feedURL string) (int, time.Time) {
	activeGUIDs := []string{}
	itemCount := 0
	var latestItemDate time.Time
	maxItems := f.maxItems
	if maxItems <= 0 {
		maxItems = len(gofeedData.Items)
	}

	for i, gofeedItem := range gofeedData.Items {
		if i >= maxItems {
			break
		}

		item, err := database.ItemFromGofeed(gofeedItem, feedURL)
		if err != nil {
			logrus.Warnf("Failed to convert item: %v", err)
			continue
		}

		// Track the latest item date
		if latestItemDate.IsZero() || item.PublishedDate.After(latestItemDate) {
			latestItemDate = item.PublishedDate
		}

		item.Archived = false
		if err := f.db.UpsertItem(item); err != nil {
			logrus.Warnf("Failed to save item: %v", err)
			continue
		}

		activeGUIDs = append(activeGUIDs, item.GUID)
		itemCount++
	}

	if err := f.db.MarkItemsArchived(feedURL, activeGUIDs); err != nil {
		logrus.Warnf("Failed to mark archived items: %v", err)
	}

	return itemCount, latestItemDate
}

func (f *Fetcher) handleCachedFeed(result *FetchResult, existingFeed *database.Feed) *FetchResult {
	if existingFeed != nil {
		existingFeed.LastFetchTime = time.Now()
		existingFeed.LastSuccessfulFetch = time.Now()
		if upsertErr := f.db.UpsertFeed(existingFeed); upsertErr != nil {
			logrus.WithError(upsertErr).Warn("Failed to update feed in database")
		}
		result.Feed = existingFeed
		result.Cached = true
	}
	return result
}

func (f *Fetcher) updateFeedError(feed *database.Feed, errorMsg string) {
	if feed != nil {
		feed.ErrorCount++
		feed.LastError = errorMsg
		feed.LastFetchTime = time.Now()
		if err := f.db.UpsertFeed(feed); err != nil {
			logrus.WithError(err).Warn("Failed to update feed error in database")
		}
	}
}

type completionEvent struct {
	index  int
	result *FetchResult
}

func FetchConcurrent(
	db *database.DB, urls []string, concurrency int, timeout time.Duration,
	maxItems int, maxAge time.Duration, force bool,
) []*FetchResult {
	fetcher := NewFetcher(db, timeout, maxItems, force)
	results := make([]*FetchResult, len(urls))

	sem := make(chan struct{}, concurrency)
	completions := make(chan completionEvent, len(urls))
	var wg sync.WaitGroup

	// WaitGroup for the logging goroutine
	var logWg sync.WaitGroup
	logWg.Add(1)

	// Start goroutine to handle completion logging in order
	go func() {
		defer logWg.Done()
		completedCount := 0
		nextExpected := 0
		pending := make(map[int]completionEvent)

		// Calculate padding width based on total count
		totalWidth := len(strconv.Itoa(len(urls)))

		for completion := range completions {
			pending[completion.index] = completion

			// Process all sequential completions starting from nextExpected
			for {
				if event, exists := pending[nextExpected]; exists {
					completedCount++
					percentage := int(float64(completedCount) / float64(len(urls)) * 100)

					if event.result.Error != nil {
						logrus.Infof("Failed   %3d%% (%*d/%d) %s: %v",
							percentage, totalWidth, completedCount, len(urls), event.result.URL, event.result.Error)
					} else if event.result.Cached {
						logrus.Infof("Cached   %3d%% (%*d/%d) %s",
							percentage, totalWidth, completedCount, len(urls), event.result.URL)
					} else if event.result.Feed != nil {
						logrus.Infof("Fetched  %3d%% (%*d/%d) %s (%d items)",
							percentage, totalWidth, completedCount, len(urls), event.result.URL, event.result.ItemCount)
					}

					delete(pending, nextExpected)
					nextExpected++
				} else {
					break
				}
			}
		}
	}()

	for i, url := range urls {
		wg.Add(1)
		go func(index int, feedURL string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			logrus.Debugf("Starting fetch: %s", feedURL)

			var result *FetchResult

			if maxAge > 0 && !force {
				existingFeed, _ := db.GetFeed(feedURL)
				if existingFeed != nil && time.Since(existingFeed.LastFetchTime) < maxAge {
					result = &FetchResult{
						URL:    feedURL,
						Feed:   existingFeed,
						Cached: true,
					}
				}
			}

			if result == nil {
				result = fetcher.FetchFeed(feedURL)
			}

			results[index] = result
			completions <- completionEvent{index: index, result: result}
		}(i, url)
	}

	wg.Wait()
	close(completions)

	// Wait for the logging goroutine to finish processing all events
	logWg.Wait()

	return results
}
