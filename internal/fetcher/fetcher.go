package fetcher

import (
	"context"
	"fmt"
	"net/http"
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
}

func NewFetcher(timeout time.Duration, maxItems int, force bool) *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: timeout,
		},
		timeout:   timeout,
		maxItems:  maxItems,
		forceFlag: force,
	}
}

func (f *Fetcher) FetchFeed(feedURL string) *FetchResult {
	result := &FetchResult{
		URL: feedURL,
	}

	existingFeed, err := database.GetFeed(feedURL)
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

	if err := database.UpsertFeed(feed); err != nil {
		result.Error = fmt.Errorf("failed to save feed: %w", err)
		return result
	}

	itemCount := f.processFeedItems(gofeedData, feedURL)
	result.ItemCount = itemCount
	result.Feed = feed
	return result
}

func (f *Fetcher) processFeedItems(gofeedData *gofeed.Feed, feedURL string) int {
	activeGUIDs := []string{}
	itemCount := 0
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

		item.Archived = false
		if err := database.UpsertItem(item); err != nil {
			logrus.Warnf("Failed to save item: %v", err)
			continue
		}

		activeGUIDs = append(activeGUIDs, item.GUID)
		itemCount++
	}

	if err := database.MarkItemsArchived(feedURL, activeGUIDs); err != nil {
		logrus.Warnf("Failed to mark archived items: %v", err)
	}

	return itemCount
}

func (f *Fetcher) handleCachedFeed(result *FetchResult, existingFeed *database.Feed) *FetchResult {
	if existingFeed != nil {
		existingFeed.LastFetchTime = time.Now()
		existingFeed.LastSuccessfulFetch = time.Now()
		if upsertErr := database.UpsertFeed(existingFeed); upsertErr != nil {
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
		if err := database.UpsertFeed(feed); err != nil {
			logrus.WithError(err).Warn("Failed to update feed error in database")
		}
	}
}

func FetchConcurrent(
	urls []string, concurrency int, timeout time.Duration,
	maxItems int, maxAge time.Duration, force bool,
) []*FetchResult {
	fetcher := NewFetcher(timeout, maxItems, force)
	results := make([]*FetchResult, len(urls))

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, url := range urls {
		wg.Add(1)
		go func(index int, feedURL string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if maxAge > 0 && !force {
				existingFeed, _ := database.GetFeed(feedURL)
				if existingFeed != nil && time.Since(existingFeed.LastFetchTime) < maxAge {
					results[index] = &FetchResult{
						URL:    feedURL,
						Feed:   existingFeed,
						Cached: true,
					}
					logrus.Debugf("Skipping recently fetched feed: %s", feedURL)
					return
				}
			}

			logrus.Infof("Fetching %s (%d/%d)", feedURL, index+1, len(urls))
			results[index] = fetcher.FetchFeed(feedURL)
		}(i, url)
	}

	wg.Wait()
	return results
}
