package fetcher

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/httpclient"
	"github.com/lmorchard/feedspool-go/internal/unfurl"
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
	client      *httpclient.Client
	timeout     time.Duration
	maxItems    int
	forceFlag   bool
	db          *database.DB
	unfurlQueue *unfurl.UnfurlQueue
}

func NewFetcher(db *database.DB, timeout time.Duration, maxItems int, force bool) *Fetcher {
	httpClient := httpclient.NewClient(&httpclient.Config{
		Timeout:   timeout,
		UserAgent: httpclient.DefaultUserAgent,
	})

	return &Fetcher{
		client:    httpClient,
		timeout:   timeout,
		maxItems:  maxItems,
		forceFlag: force,
		db:        db,
	}
}

// SetUnfurlQueue sets the unfurl queue for parallel unfurl operations.
func (f *Fetcher) SetUnfurlQueue(queue *unfurl.UnfurlQueue) {
	f.unfurlQueue = queue
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

	headers := make(map[string]string)
	if !f.forceFlag && existingFeed != nil {
		if existingFeed.ETag != "" {
			headers["If-None-Match"] = existingFeed.ETag
		}
		if existingFeed.LastModified != "" {
			headers["If-Modified-Since"] = existingFeed.LastModified
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), f.timeout)
	defer cancel()

	resp, err := f.client.Do(&httpclient.Request{
		URL:     feedURL,
		Method:  "GET",
		Headers: headers,
		Context: ctx,
	})
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
	gofeedData, err := parser.Parse(resp.BodyReader)
	if err != nil {
		result.Error = fmt.Errorf("failed to parse: %w", err)
		f.updateFeedError(existingFeed, result.Error.Error())
		return result
	}

	return f.processParsedFeed(result, gofeedData, feedURL, resp)
}

func (f *Fetcher) processParsedFeed(
	result *FetchResult, gofeedData *gofeed.Feed, feedURL string, resp *httpclient.Response,
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

	// Process items and get the latest item date (only from new items)
	itemCount, latestItemDate := f.processFeedItems(gofeedData, feedURL)
	if !latestItemDate.IsZero() {
		// Only update if we found new items with first_seen timestamps
		feed.LatestItemDate = sql.NullTime{Time: latestItemDate, Valid: true}
		// Update feed with latest item date
		if err := f.db.UpsertFeed(feed); err != nil {
			logrus.Warnf("Failed to update feed with latest item date: %v", err)
		}
	}
	// Note: If no new items, we preserve the existing latest_item_date in the database

	result.ItemCount = itemCount
	result.Feed = feed
	return result
}

//nolint:cyclop // Complex feed processing logic requires multiple conditions
func (f *Fetcher) processFeedItems(gofeedData *gofeed.Feed, feedURL string) (int, time.Time) {
	activeGUIDs := []string{}
	itemCount := 0
	var latestItemDate time.Time
	var newItemURLs []string

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

		// Check if this is a new item (before upserting)
		isNewItem := f.isNewItem(feedURL, item.GUID)

		// Set first_seen timestamp for new items, or load it for existing items
		if isNewItem {
			item.FirstSeen = sql.NullTime{Time: time.Now(), Valid: true}
		} else {
			// Load first_seen from database for existing items
			existingFirstSeen, err := f.getItemFirstSeen(feedURL, item.GUID)
			if err == nil && existingFirstSeen.Valid {
				item.FirstSeen = existingFirstSeen
			}
		}

		// Track the latest item date - only count items with first_seen set
		// This ensures we track when content actually appeared, not re-fetch times
		if item.FirstSeen.Valid {
			itemDate := item.FirstSeen.Time
			if latestItemDate.IsZero() || itemDate.After(latestItemDate) {
				latestItemDate = itemDate
			}
		}

		item.Archived = false
		if err := f.db.UpsertItem(item); err != nil {
			logrus.Warnf("Failed to save item: %v", err)
			continue
		}

		activeGUIDs = append(activeGUIDs, item.GUID)
		itemCount++

		// If this is a new item and we have an unfurl queue, validate and enqueue the item URL
		if isNewItem && f.unfurlQueue != nil && item.Link != "" {
			if f.isValidURL(item.Link) {
				newItemURLs = append(newItemURLs, item.Link)
			} else {
				logrus.Debugf("Skipping malformed URL for unfurl: %s", item.Link)
			}
		}
	}

	// Enqueue new item URLs for unfurl processing
	if len(newItemURLs) > 0 && f.unfurlQueue != nil {
		// Filter out URLs that already have metadata
		urlsNeedingUnfurl, err := f.filterURLsNeedingUnfurl(newItemURLs)
		if err != nil {
			logrus.Warnf("Error filtering URLs for unfurl: %v", err)
			// Continue with all URLs if filtering fails
			urlsNeedingUnfurl = newItemURLs
		}

		filteredCount := len(newItemURLs) - len(urlsNeedingUnfurl)
		if filteredCount > 0 {
			logrus.Debugf("Filtered %d items that already have metadata", filteredCount)
		}

		if len(urlsNeedingUnfurl) > 0 {
			logrus.Debugf("Enqueuing %d new items for unfurl from feed %s", len(urlsNeedingUnfurl), feedURL)
			for _, url := range urlsNeedingUnfurl {
				f.unfurlQueue.Enqueue(unfurl.UnfurlJob{URL: url})
			}
			logrus.Infof("Enqueued %d items for unfurl", len(urlsNeedingUnfurl))
		}
	}

	if err := f.db.MarkItemsArchived(feedURL, activeGUIDs); err != nil {
		logrus.Warnf("Failed to mark archived items: %v", err)
	}

	return itemCount, latestItemDate
}

// isNewItem checks if an item with the given GUID already exists for the feed.
func (f *Fetcher) isNewItem(feedURL, guid string) bool {
	query := `SELECT COUNT(*) FROM items WHERE feed_url = ? AND guid = ?`
	var count int
	err := f.db.GetConnection().QueryRow(query, feedURL, guid).Scan(&count)
	if err != nil {
		logrus.Debugf("Error checking if item is new: %v", err)
		return false // Assume not new if we can't check
	}
	return count == 0
}

// getItemFirstSeen retrieves the first_seen timestamp for an existing item.
func (f *Fetcher) getItemFirstSeen(feedURL, guid string) (sql.NullTime, error) {
	query := `SELECT first_seen FROM items WHERE feed_url = ? AND guid = ?`
	var firstSeen sql.NullTime
	err := f.db.GetConnection().QueryRow(query, feedURL, guid).Scan(&firstSeen)
	if err != nil {
		return sql.NullTime{}, err
	}
	return firstSeen, nil
}

// filterURLsNeedingUnfurl filters URLs to only include those that don't already have metadata.
func (f *Fetcher) filterURLsNeedingUnfurl(urls []string) ([]string, error) {
	if len(urls) == 0 {
		return urls, nil
	}

	// Check which URLs already have metadata
	hasMetadata, err := f.db.HasUnfurlMetadataBatch(urls)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing metadata: %w", err)
	}

	// Filter to only URLs that don't have metadata
	var filtered []string
	for _, url := range urls {
		if !hasMetadata[url] {
			filtered = append(filtered, url)
		}
	}

	return filtered, nil
}

// isValidURL checks if a URL is valid and suitable for unfurling.
func (f *Fetcher) isValidURL(urlStr string) bool {
	if urlStr == "" {
		return false
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return false
	}

	// Only allow HTTP and HTTPS schemes
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return false
	}

	// Must have a host
	if parsedURL.Host == "" {
		return false
	}

	return true
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
	return FetchConcurrentWithUnfurl(db, urls, concurrency, timeout, maxItems, maxAge, force, nil)
}

// FetchConcurrentWithUnfurl is the same as FetchConcurrent but supports an optional unfurl queue.
func FetchConcurrentWithUnfurl(
	db *database.DB, urls []string, concurrency int, timeout time.Duration,
	maxItems int, maxAge time.Duration, force bool, unfurlQueue *unfurl.UnfurlQueue,
) []*FetchResult {
	fetcher := NewFetcher(db, timeout, maxItems, force)
	if unfurlQueue != nil {
		fetcher.SetUnfurlQueue(unfurlQueue)
	}
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
