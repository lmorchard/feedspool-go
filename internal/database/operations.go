package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

func UpsertFeed(feed *Feed) error {
	if db == nil {
		return fmt.Errorf("database not connected")
	}

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

	_, err := db.Exec(query,
		feed.URL, feed.Title, feed.Description, feed.LastUpdated, feed.ETag,
		feed.LastModified, feed.LastFetchTime, feed.LastSuccessfulFetch,
		feed.ErrorCount, feed.LastError, feed.FeedJSON)
	if err != nil {
		return fmt.Errorf("failed to upsert feed: %w", err)
	}

	logrus.Debugf("Upserted feed: %s", feed.URL)
	return nil
}

func GetFeed(url string) (*Feed, error) {
	if db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	query := `
		SELECT url, title, description, last_updated, etag, last_modified,
			last_fetch_time, last_successful_fetch, error_count, last_error, feed_json
		FROM feeds WHERE url = ?
	`

	feed := &Feed{}
	err := db.QueryRow(query, url).Scan(
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

func GetAllFeeds() ([]*Feed, error) {
	if db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	query := `
		SELECT url, title, description, last_updated, etag, last_modified,
			last_fetch_time, last_successful_fetch, error_count, last_error, feed_json
		FROM feeds ORDER BY url
	`

	rows, err := db.Query(query)
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

func UpsertItem(item *Item) error {
	if db == nil {
		return fmt.Errorf("database not connected")
	}

	query := `
		INSERT INTO items (feed_url, guid, title, link, published_date, 
			content, summary, archived, item_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(feed_url, guid) DO UPDATE SET
			title = excluded.title,
			link = excluded.link,
			published_date = excluded.published_date,
			content = excluded.content,
			summary = excluded.summary,
			archived = excluded.archived,
			item_json = excluded.item_json
	`

	_, err := db.Exec(query,
		item.FeedURL, item.GUID, item.Title, item.Link, item.PublishedDate,
		item.Content, item.Summary, item.Archived, item.ItemJSON)
	if err != nil {
		return fmt.Errorf("failed to upsert item: %w", err)
	}

	logrus.Debugf("Upserted item: %s - %s", item.FeedURL, item.GUID)
	return nil
}

func GetItemsForFeed(feedURL string, limit int, since, until time.Time) ([]*Item, error) {
	if db == nil {
		return nil, fmt.Errorf("database not connected")
	}

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

	rows, err := db.Query(query, args...)
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

func MarkItemsArchived(feedURL string, activeGUIDs []string) error {
	if db == nil {
		return fmt.Errorf("database not connected")
	}

	if len(activeGUIDs) == 0 {
		_, err := db.Exec("UPDATE items SET archived = 1 WHERE feed_url = ?", feedURL)
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

	query := fmt.Sprintf(
		"UPDATE items SET archived = 1 WHERE feed_url = ? AND guid NOT IN (%s)",
		strings.Join(placeholders, ","))

	result, err := db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to archive items: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		logrus.Debugf("Archived %d items for feed: %s", rowsAffected, feedURL)
	}

	return nil
}

func DeleteArchivedItems(olderThan time.Time) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("database not connected")
	}

	query := "DELETE FROM items WHERE archived = 1 AND published_date < ?"
	result, err := db.Exec(query, olderThan)
	if err != nil {
		return 0, fmt.Errorf("failed to delete archived items: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	logrus.Debugf("Deleted %d archived items", rowsAffected)
	return rowsAffected, nil
}

func DeleteFeed(url string) error {
	if db == nil {
		return fmt.Errorf("database not connected")
	}

	_, err := db.Exec("DELETE FROM feeds WHERE url = ?", url)
	if err != nil {
		return fmt.Errorf("failed to delete feed: %w", err)
	}

	logrus.Debugf("Deleted feed: %s", url)
	return nil
}

func GetFeedURLs() ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	rows, err := db.Query("SELECT url FROM feeds ORDER BY url")
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
