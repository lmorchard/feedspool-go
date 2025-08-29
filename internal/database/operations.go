package database

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// UpsertFeed inserts or updates a feed record in the database.
func (db *DB) UpsertFeed(feed *Feed) error {
	query := `
		INSERT INTO feeds (url, title, description, last_updated, etag, last_modified, 
			last_fetch_time, last_successful_fetch, error_count, last_error, feed_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			feed_json = excluded.feed_json
	`

	_, err := db.conn.Exec(query,
		feed.URL, feed.Title, feed.Description, feed.LastUpdated, feed.ETag,
		feed.LastModified, feed.LastFetchTime, feed.LastSuccessfulFetch,
		feed.ErrorCount, feed.LastError, feed.FeedJSON)
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
			last_fetch_time, last_successful_fetch, error_count, last_error, feed_json
		FROM feeds WHERE url = ?
	`

	feed := &Feed{}
	err := db.conn.QueryRow(query, url).Scan(
		&feed.URL, &feed.Title, &feed.Description, &feed.LastUpdated, &feed.ETag,
		&feed.LastModified, &feed.LastFetchTime, &feed.LastSuccessfulFetch,
		&feed.ErrorCount, &feed.LastError, &feed.FeedJSON)

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
			last_fetch_time, last_successful_fetch, error_count, last_error, feed_json
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
			&feed.ErrorCount, &feed.LastError, &feed.FeedJSON)
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

// UpsertItem inserts or updates an item record in the database.
func (db *DB) UpsertItem(item *Item) error {
	query := `
		INSERT INTO items (feed_url, guid, title, link, published_date, 
			content, summary, archived, item_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(feed_url, guid) DO UPDATE SET
			title = excluded.title,
			link = excluded.link,
			content = excluded.content,
			summary = excluded.summary,
			archived = excluded.archived,
			item_json = excluded.item_json
	`

	_, err := db.conn.Exec(query,
		item.FeedURL, item.GUID, item.Title, item.Link, item.PublishedDate,
		item.Content, item.Summary, item.Archived, item.ItemJSON)
	if err != nil {
		return fmt.Errorf("failed to upsert item: %w", err)
	}

	logrus.Debugf("Upserted item: %s - %s", item.FeedURL, item.GUID)
	return nil
}

// GetItemsForFeed retrieves items for a specific feed with optional filtering by time range and limit.
func (db *DB) GetItemsForFeed(feedURL string, limit int, since, until time.Time) ([]*Item, error) {
	query := `
		SELECT id, feed_url, guid, title, link, published_date, 
			content, summary, archived, item_json
		FROM items 
		WHERE feed_url = ? AND archived = 0
	`
	args := []interface{}{feedURL}

	if !since.IsZero() {
		query += " AND published_date >= ?"
		args = append(args, since)
	}

	if !until.IsZero() {
		query += " AND published_date <= ?"
		args = append(args, until)
	}

	query += " ORDER BY published_date DESC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get items: %w", err)
	}
	defer rows.Close()

	items := []*Item{}
	for rows.Next() {
		item := &Item{}
		err := rows.Scan(
			&item.ID, &item.FeedURL, &item.GUID, &item.Title, &item.Link,
			&item.PublishedDate, &item.Content, &item.Summary, &item.Archived,
			&item.ItemJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to scan item: %w", err)
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over items: %w", err)
	}

	return items, nil
}

// MarkItemsArchived marks items as archived for a specific feed, except for the provided active GUIDs.
func (db *DB) MarkItemsArchived(feedURL string, activeGUIDs []string) error {
	if len(activeGUIDs) == 0 {
		_, err := db.conn.Exec("UPDATE items SET archived = 1 WHERE feed_url = ?", feedURL)
		if err != nil {
			return fmt.Errorf("failed to archive all items: %w", err)
		}
		logrus.Debugf("Archived all items for feed: %s", feedURL)
		return nil
	}

	placeholders := make([]string, len(activeGUIDs))
	args := make([]interface{}, len(activeGUIDs)+1)
	args[0] = feedURL
	for i, guid := range activeGUIDs {
		placeholders[i] = "?"
		args[i+1] = guid
	}

	//nolint:gosec // Safe: only formatting placeholder count, not user input
	query := fmt.Sprintf(
		"UPDATE items SET archived = 1 WHERE feed_url = ? AND guid NOT IN (%s)",
		strings.Join(placeholders, ","))

	result, err := db.conn.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to archive items: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		logrus.Debugf("Archived %d items for feed: %s", rowsAffected, feedURL)
	}

	return nil
}

// DeleteArchivedItems deletes archived items older than the specified time.
func (db *DB) DeleteArchivedItems(olderThan time.Time) (int64, error) {
	query := "DELETE FROM items WHERE archived = 1 AND published_date < ?"
	result, err := db.conn.Exec(query, olderThan)
	if err != nil {
		return 0, fmt.Errorf("failed to delete archived items: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	logrus.Debugf("Deleted %d archived items", rowsAffected)
	return rowsAffected, nil
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
	feedsQuery := `
		SELECT f.url, f.title, f.description, f.last_updated, f.etag, f.last_modified,
			f.last_fetch_time, f.last_successful_fetch, f.error_count, f.last_error, f.feed_json
		FROM feeds f
		WHERE f.last_updated >= ? AND f.last_updated <= ?
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

	feedsQuery += " ORDER BY f.last_updated DESC"

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
			&feed.ErrorCount, &feed.LastError, &feed.FeedJSON)
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

// getItemsForFeeds gets all items for a set of feeds within a time range.
func (db *DB) getItemsForFeeds(feedURLMap map[string]bool, start, end time.Time) (map[string][]Item, error) {
	if len(feedURLMap) == 0 {
		return make(map[string][]Item), nil
	}

	// Build placeholders for IN clause
	feedURLs := make([]string, 0, len(feedURLMap))
	for url := range feedURLMap {
		feedURLs = append(feedURLs, url)
	}

	placeholders := make([]string, len(feedURLs))
	args := make([]interface{}, 0, len(feedURLs)+2)

	for i, url := range feedURLs {
		placeholders[i] = "?"
		args = append(args, url)
	}
	args = append(args, start, end)

	//nolint:gosec // Safe: only formatting placeholder count, not user input
	query := fmt.Sprintf(`
		SELECT id, feed_url, guid, title, link, published_date,
			content, summary, archived, item_json
		FROM items
		WHERE feed_url IN (%s) AND archived = 0 
			AND published_date >= ? AND published_date <= ?
		ORDER BY feed_url, published_date DESC
	`, strings.Join(placeholders, ","))

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query items: %w", err)
	}
	defer rows.Close()

	items := make(map[string][]Item)
	for rows.Next() {
		item := Item{}
		err := rows.Scan(
			&item.ID, &item.FeedURL, &item.GUID, &item.Title, &item.Link,
			&item.PublishedDate, &item.Content, &item.Summary, &item.Archived,
			&item.ItemJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to scan item: %w", err)
		}
		items[item.FeedURL] = append(items[item.FeedURL], item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over items: %w", err)
	}

	return items, nil
}

// ParseTimeWindow parses CLI time arguments and returns start and end times.
func ParseTimeWindow(maxAge, startStr, endStr string) (startTime, endTime time.Time, err error) {
	// Parse explicit time range first
	if startStr != "" || endStr != "" {
		return parseExplicitTimeRange(startStr, endStr)
	}

	// Parse max age duration
	if maxAge != "" {
		return parseMaxAgeDuration(maxAge)
	}

	// Default to 24 hours if nothing specified
	endTime = time.Now()
	startTime = endTime.Add(-24 * time.Hour)
	return startTime, endTime, nil
}

func parseExplicitTimeRange(startStr, endStr string) (startTime, endTime time.Time, err error) {
	if startStr != "" {
		startTime, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start time format: %w", err)
		}
	}

	if endStr != "" {
		endTime, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end time format: %w", err)
		}
	} else {
		endTime = time.Now()
	}

	if !startTime.IsZero() && !endTime.IsZero() && startTime.After(endTime) {
		return time.Time{}, time.Time{}, fmt.Errorf("start time cannot be after end time")
	}

	return startTime, endTime, nil
}

func parseMaxAgeDuration(maxAge string) (startTime, endTime time.Time, err error) {
	duration, err := ParseDuration(maxAge)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid max-age duration: %w", err)
	}

	endTime = time.Now()
	startTime = endTime.Add(-duration)
	return startTime, endTime, nil
}

// ParseDuration parses duration strings including "d" for days and "w" for weeks.
func ParseDuration(s string) (time.Duration, error) {
	re := regexp.MustCompile(`^(\d+)([dwh])$`)
	matches := re.FindStringSubmatch(strings.ToLower(s))

	if len(matches) != 3 {
		return time.ParseDuration(s)
	}

	num, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, err
	}

	switch matches[2] {
	case "d":
		return time.Duration(num) * 24 * time.Hour, nil
	case "w":
		return time.Duration(num) * 7 * 24 * time.Hour, nil
	case "h":
		return time.Duration(num) * time.Hour, nil
	default:
		return time.ParseDuration(s)
	}
}
