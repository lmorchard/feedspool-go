package database

import (
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

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
