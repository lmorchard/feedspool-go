package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// UpsertFeed inserts or updates a feed record in the database.
func (db *DB) UpsertFeed(feed *Feed) error {
	query := `
		INSERT INTO feeds (url, title, description, last_updated, etag, last_modified, 
			last_fetch_time, last_successful_fetch, error_count, last_error, latest_item_date, feed_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(url) DO UPDATE SET
			title = excluded.title,
			description = excluded.description,
			last_updated = excluded.last_updated,
			etag = excluded.etag,
			last_modified = excluded.last_modified,
			last_fetch_time = excluded.last_fetch_time,
			last_successful_fetch = excluded.last_successful_fetch,
			error_count = excluded.error_count,
			last_error = excluded.last_error,
			latest_item_date = excluded.latest_item_date,
			feed_json = excluded.feed_json
	`

	_, err := db.conn.Exec(query,
		feed.URL, feed.Title, feed.Description, feed.LastUpdated, feed.ETag,
		feed.LastModified, feed.LastFetchTime, feed.LastSuccessfulFetch,
		feed.ErrorCount, feed.LastError, feed.LatestItemDate, feed.FeedJSON)
	if err != nil {
		return fmt.Errorf("failed to upsert feed: %w", err)
	}

	logrus.Debugf("Upserted feed: %s", feed.URL)
	return nil
}

// GetFeed retrieves a feed by URL from the database.
func (db *DB) GetFeed(url string) (*Feed, error) {
	query := `
		SELECT url, title, description, last_updated, etag, last_modified,
			last_fetch_time, last_successful_fetch, error_count, last_error, latest_item_date, feed_json
		FROM feeds WHERE url = ?
	`

	feed := &Feed{}
	err := db.conn.QueryRow(query, url).Scan(
		&feed.URL, &feed.Title, &feed.Description, &feed.LastUpdated, &feed.ETag,
		&feed.LastModified, &feed.LastFetchTime, &feed.LastSuccessfulFetch,
		&feed.ErrorCount, &feed.LastError, &feed.LatestItemDate, &feed.FeedJSON)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get feed: %w", err)
	}

	return feed, nil
}

// GetAllFeeds retrieves all feeds from the database, ordered by URL.
func (db *DB) GetAllFeeds() ([]*Feed, error) {
	query := `
		SELECT url, title, description, last_updated, etag, last_modified,
			last_fetch_time, last_successful_fetch, error_count, last_error, latest_item_date, feed_json
		FROM feeds ORDER BY url
	`

	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get feeds: %w", err)
	}
	defer rows.Close()

	feeds := []*Feed{}
	for rows.Next() {
		feed := &Feed{}
		err := rows.Scan(
			&feed.URL, &feed.Title, &feed.Description, &feed.LastUpdated, &feed.ETag,
			&feed.LastModified, &feed.LastFetchTime, &feed.LastSuccessfulFetch,
			&feed.ErrorCount, &feed.LastError, &feed.LatestItemDate, &feed.FeedJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to scan feed: %w", err)
		}
		feeds = append(feeds, feed)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over feeds: %w", err)
	}

	return feeds, nil
}

// DeleteFeed deletes a feed and all its associated items from the database.
func (db *DB) DeleteFeed(url string) error {
	_, err := db.conn.Exec("DELETE FROM feeds WHERE url = ?", url)
	if err != nil {
		return fmt.Errorf("failed to delete feed: %w", err)
	}

	logrus.Debugf("Deleted feed: %s", url)
	return nil
}

// GetFeedURLs retrieves all feed URLs from the database, ordered by URL.
func (db *DB) GetFeedURLs() ([]string, error) {
	rows, err := db.conn.Query("SELECT url FROM feeds ORDER BY url")
	if err != nil {
		return nil, fmt.Errorf("failed to get feed URLs: %w", err)
	}
	defer rows.Close()

	urls := []string{}
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, fmt.Errorf("failed to scan URL: %w", err)
		}
		urls = append(urls, url)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over URLs: %w", err)
	}

	return urls, nil
}

// GetFeedsWithItemsByTimeRange gets feeds and their items within a specific time range.
func (db *DB) GetFeedsWithItemsByTimeRange(start, end time.Time, feedURLs []string) ([]Feed, map[string][]Item, error) {
	// Build feeds query
	// Use latest_item_date to determine if feed has recent items, falling back to last_updated
	feedsQuery := `
		SELECT f.url, f.title, f.description, f.last_updated, f.etag, f.last_modified,
			f.last_fetch_time, f.last_successful_fetch, f.error_count, f.last_error, f.latest_item_date, f.feed_json
		FROM feeds f
		WHERE COALESCE(f.latest_item_date, f.last_updated) >= ?
			AND COALESCE(f.latest_item_date, f.last_updated) <= ?
	`
	feedsArgs := []interface{}{start, end}

	// Add feed URL filtering if specified
	if len(feedURLs) > 0 {
		placeholders := make([]string, len(feedURLs))
		for i, url := range feedURLs {
			placeholders[i] = "?"
			feedsArgs = append(feedsArgs, url)
		}
		feedsQuery += " AND f.url IN (" + strings.Join(placeholders, ",") + ")"
	}

	// Order by latest item date (newest first), falling back to last_updated if null
	feedsQuery += " ORDER BY COALESCE(f.latest_item_date, f.last_updated) DESC"

	// Query feeds
	rows, err := db.conn.Query(feedsQuery, feedsArgs...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query feeds: %w", err)
	}
	defer rows.Close()

	feeds := []Feed{}
	feedURLMap := make(map[string]bool)

	for rows.Next() {
		feed := Feed{}
		err := rows.Scan(
			&feed.URL, &feed.Title, &feed.Description, &feed.LastUpdated, &feed.ETag,
			&feed.LastModified, &feed.LastFetchTime, &feed.LastSuccessfulFetch,
			&feed.ErrorCount, &feed.LastError, &feed.LatestItemDate, &feed.FeedJSON)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to scan feed: %w", err)
		}
		feeds = append(feeds, feed)
		feedURLMap[feed.URL] = true
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("error iterating over feeds: %w", err)
	}

	// Query items for the found feeds
	items := make(map[string][]Item)
	if len(feeds) > 0 {
		var err error
		items, err = db.getItemsForFeeds(feedURLMap, start, end)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get items: %w", err)
		}
	}

	return feeds, items, nil
}

// GetFeedsWithItemsByMaxAge gets feeds and their items within a specified age from now.
func (db *DB) GetFeedsWithItemsByMaxAge(maxAge time.Duration, feedURLs []string) ([]Feed, map[string][]Item, error) {
	end := time.Now()
	start := end.Add(-maxAge)
	return db.GetFeedsWithItemsByTimeRange(start, end, feedURLs)
}

// GetFeedsWithItemsMinimum gets ALL feeds and their items, applying a minimum items guarantee.
// For each feed:
//   - If the feed has >= minItemsPerFeed items within the timespan, return those items
//   - If the feed has < minItemsPerFeed items within the timespan, return minItemsPerFeed most recent items
//
// This ensures quiet/infrequently-updated feeds remain visible with recent items, while busy
// feeds show all items within the requested timespan.
//
// When minItemsPerFeed is 0, only items within the timespan are returned (no minimum guarantee).
func (db *DB) GetFeedsWithItemsMinimum(
	start, end time.Time, feedURLs []string, minItemsPerFeed int,
) ([]Feed, map[string][]Item, error) {
	// Get all feeds (optionally filtered by feedURLs list)
	feeds, err := db.getFeedsFiltered(feedURLs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get feeds: %w", err)
	}

	if len(feeds) == 0 {
		return []Feed{}, make(map[string][]Item), nil
	}

	// Get items for each feed with minimum guarantee
	items := make(map[string][]Item)
	feedsWithItems := []Feed{}

	for i := range feeds {
		feedItems, err := db.getItemsForFeedWithMinimum(feeds[i].URL, start, end, minItemsPerFeed)
		if err != nil {
			logrus.Warnf("Failed to get items for feed %s: %v", feeds[i].URL, err)
			continue
		}

		if len(feedItems) > 0 {
			items[feeds[i].URL] = feedItems
			feedsWithItems = append(feedsWithItems, feeds[i])
		}
	}

	return feedsWithItems, items, nil
}

// getFeedsFiltered gets all feeds, optionally filtered by a list of URLs.
func (db *DB) getFeedsFiltered(feedURLs []string) ([]Feed, error) {
	query := `
		SELECT url, title, description, last_updated, etag, last_modified,
			last_fetch_time, last_successful_fetch, error_count, last_error, latest_item_date, feed_json
		FROM feeds
	`
	args := []interface{}{}

	if len(feedURLs) > 0 {
		placeholders := make([]string, len(feedURLs))
		for i, url := range feedURLs {
			placeholders[i] = "?"
			args = append(args, url)
		}
		query += " WHERE url IN (" + strings.Join(placeholders, ",") + ")"
	}

	query += " ORDER BY COALESCE(latest_item_date, last_updated) DESC"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query feeds: %w", err)
	}
	defer rows.Close()

	feeds := []Feed{}
	for rows.Next() {
		feed := Feed{}
		err := rows.Scan(
			&feed.URL, &feed.Title, &feed.Description, &feed.LastUpdated, &feed.ETag,
			&feed.LastModified, &feed.LastFetchTime, &feed.LastSuccessfulFetch,
			&feed.ErrorCount, &feed.LastError, &feed.LatestItemDate, &feed.FeedJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to scan feed: %w", err)
		}
		feeds = append(feeds, feed)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over feeds: %w", err)
	}

	return feeds, nil
}

// getItemsForFeedWithMinimum gets items for a feed, ensuring at least minItems are returned.
// Returns MAX(items within timespan, minItems most recent items).
// This ensures active feeds show all new items, while quiet feeds still show recent history.
func (db *DB) getItemsForFeedWithMinimum(feedURL string, start, end time.Time, minItems int) ([]Item, error) {
	// First, get items within the timespan
	timespanQuery := `
		SELECT id, feed_url, guid, title, link, published_date,
			content, summary, archived, item_json
		FROM items
		WHERE feed_url = ?
			AND published_date >= ? AND published_date <= ?
		ORDER BY published_date DESC
	`

	rows, err := db.conn.Query(timespanQuery, feedURL, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to query timespan items: %w", err)
	}
	defer rows.Close()

	items := []Item{}
	for rows.Next() {
		item := Item{}
		err := rows.Scan(
			&item.ID, &item.FeedURL, &item.GUID, &item.Title, &item.Link,
			&item.PublishedDate, &item.Content, &item.Summary, &item.Archived,
			&item.ItemJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to scan timespan item: %w", err)
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over timespan items: %w", err)
	}

	// If we have enough items from the timespan, we're done
	if len(items) >= minItems {
		return items, nil
	}

	// Otherwise, get additional recent items to reach minItems
	// Query for recent items that we might not have already
	recentQuery := `
		SELECT id, feed_url, guid, title, link, published_date,
			content, summary, archived, item_json
		FROM items
		WHERE feed_url = ?
		ORDER BY published_date DESC
		LIMIT ?
	`

	rows2, err := db.conn.Query(recentQuery, feedURL, minItems)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent items: %w", err)
	}
	defer rows2.Close()

	// Build a map of GUIDs we already have from the timespan
	existingGUIDs := make(map[string]bool)
	for i := range items {
		existingGUIDs[items[i].GUID] = true
	}

	// Add recent items that aren't already in our list, up to minItems total
	needed := minItems - len(items)
	for rows2.Next() && needed > 0 {
		item := Item{}
		err := rows2.Scan(
			&item.ID, &item.FeedURL, &item.GUID, &item.Title, &item.Link,
			&item.PublishedDate, &item.Content, &item.Summary, &item.Archived,
			&item.ItemJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to scan recent item: %w", err)
		}
		if !existingGUIDs[item.GUID] {
			items = append(items, item)
			needed--
		}
	}

	if err := rows2.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over recent items: %w", err)
	}

	return items, nil
}
