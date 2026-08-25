package database

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const discoveryTimeExpression = `julianday(CASE
	WHEN first_seen IS NULL OR substr(CAST(first_seen AS TEXT), 1, 10) = '0001-01-01'
	THEN published_date
	ELSE first_seen
END)`

const effectiveDateExpression = `julianday(COALESCE(published_date, first_seen))`

const aliasedEffectiveDateExpression = `julianday(COALESCE(i.published_date, i.first_seen))`

const (
	effectiveDateSinceClause = " AND " + effectiveDateExpression +
		" IS NOT NULL AND " + effectiveDateExpression + " >= julianday(?)"
	effectiveDateUntilClause = " AND " + effectiveDateExpression +
		" IS NOT NULL AND " + effectiveDateExpression + " <= julianday(?)"
)

// UpsertItem inserts or updates an item record in the database.
func (db *DB) UpsertItem(item *Item) error {
	var publishedDate interface{}
	if !item.PublishedDate.IsZero() {
		publishedDate = formatDatabaseTime(item.PublishedDate)
	}
	var firstSeen interface{}
	if item.FirstSeen.Valid {
		firstSeen = formatDatabaseTime(item.FirstSeen.Time)
	}
	query := `
		INSERT INTO items (feed_url, guid, title, link, published_date, first_seen,
			content, summary, archived, item_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(feed_url, guid) DO UPDATE SET
			title = excluded.title,
			link = excluded.link,
			content = excluded.content,
			summary = excluded.summary,
			archived = excluded.archived,
			item_json = excluded.item_json
	`

	_, err := db.conn.Exec(query,
		item.FeedURL, item.GUID, item.Title, item.Link, publishedDate, firstSeen,
		item.Content, item.Summary, item.Archived, item.ItemJSON)
	if err != nil {
		return fmt.Errorf("failed to upsert item: %w", err)
	}

	logrus.Debugf("Upserted item: %s - %s", item.FeedURL, item.GUID)
	return nil
}

// GetItemsByLink retrieves every item with the exact link in deterministic order.
func (db *DB) GetItemsByLink(link string) ([]*Item, error) {
	return db.queryItems(`
		SELECT id, feed_url, guid, title, link, published_date, first_seen,
			content, summary, archived, item_json
		FROM items
		WHERE link = ?
		ORDER BY feed_url, guid
	`, link)
}

// GetItem retrieves an item by its feed URL and GUID, or nil if it does not exist.
func (db *DB) GetItem(feedURL, guid string) (*Item, error) {
	items, err := db.queryItems(`
		SELECT id, feed_url, guid, title, link, published_date, first_seen,
			content, summary, archived, item_json
		FROM items
		WHERE feed_url = ? AND guid = ?
	`, feedURL, guid)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return items[0], nil
}

func (db *DB) queryItems(query string, args ...any) ([]*Item, error) {
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query items: %w", err)
	}
	defer rows.Close()

	var items []*Item
	for rows.Next() {
		item := &Item{}
		if err := rows.Scan(
			&item.ID, &item.FeedURL, &item.GUID, &item.Title, &item.Link,
			scanNullableTime(&item.PublishedDate), &item.FirstSeen, &item.Content, &item.Summary,
			&item.Archived, &item.ItemJSON,
		); err != nil {
			return nil, fmt.Errorf("failed to scan item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate items: %w", err)
	}
	return items, nil
}

// GetItemsForFeed retrieves items for a specific feed with optional filtering by time range and limit.
func (db *DB) GetItemsForFeed(feedURL string, limit int, since, until time.Time) ([]*Item, error) {
	query := `
		SELECT id, feed_url, guid, title, link, published_date, first_seen,
			content, summary, archived, item_json
		FROM items
		WHERE feed_url = ?
	`
	args := []interface{}{feedURL}

	if !since.IsZero() {
		query += effectiveDateSinceClause
		args = append(args, formatDatabaseTime(since))
	}

	if !until.IsZero() {
		query += effectiveDateUntilClause
		args = append(args, formatDatabaseTime(until))
	}

	query += " ORDER BY " + effectiveDateExpression + " DESC"

	if limit > 0 {
		query += sqlLimitClause
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
			scanNullableTime(&item.PublishedDate), &item.FirstSeen, &item.Content, &item.Summary, &item.Archived,
			&item.ItemJSON,
		)
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
		strings.Join(placeholders, ","),
	)

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
	query := "DELETE FROM items WHERE archived = 1 AND " + effectiveDateExpression +
		" IS NOT NULL AND " + effectiveDateExpression + " < julianday(?)"
	result, err := db.conn.Exec(query, formatDatabaseTime(olderThan))
	if err != nil {
		return 0, fmt.Errorf("failed to delete archived items: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	logrus.Debugf("Deleted %d archived items", rowsAffected)
	return rowsAffected, nil
}

// DeleteArchivedItemsWithMinimum deletes archived items older than the specified time,
// but ensures at least minItemsPerFeed items remain for each feed.
func (db *DB) DeleteArchivedItemsWithMinimum(olderThan time.Time, minItemsPerFeed int) (int64, error) {
	if minItemsPerFeed <= 0 {
		return db.DeleteArchivedItems(olderThan)
	}

	// Get all feed URLs
	feeds, err := db.GetAllFeeds()
	if err != nil {
		return 0, fmt.Errorf("failed to get feeds: %w", err)
	}

	var totalDeleted int64

	// Process each feed individually
	for _, feed := range feeds {
		deleted, err := db.deleteArchivedItemsForFeed(feed.URL, olderThan, minItemsPerFeed)
		if err != nil {
			logrus.Warnf("Failed to delete items for feed %s: %v", feed.URL, err)
			continue
		}
		totalDeleted += deleted
	}

	logrus.Debugf("Deleted %d archived items (with minimum %d items per feed)", totalDeleted, minItemsPerFeed)
	return totalDeleted, nil
}

// deleteArchivedItemsForFeed deletes archived items for a specific feed,
// ensuring at least minItems remain.
func (db *DB) deleteArchivedItemsForFeed(feedURL string, olderThan time.Time, minItems int) (int64, error) {
	// Get IDs of the most recent N items for this feed (to protect them from deletion)
	protectedQuery := `
		SELECT id FROM items
		WHERE feed_url = ?
		ORDER BY ` + effectiveDateExpression + ` DESC
		LIMIT ?
	`
	rows, err := db.conn.Query(protectedQuery, feedURL, minItems)
	if err != nil {
		return 0, fmt.Errorf("failed to query protected items: %w", err)
	}
	defer rows.Close()

	protectedIDs := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("failed to scan item ID: %w", err)
		}
		protectedIDs = append(protectedIDs, id)
	}

	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("error iterating protected items: %w", err)
	}

	// If we have fewer than minItems total, don't delete anything
	if len(protectedIDs) < minItems {
		return 0, nil
	}

	// Build DELETE query excluding protected items
	placeholders := make([]string, len(protectedIDs))
	args := []interface{}{feedURL, formatDatabaseTime(olderThan)}
	for i, id := range protectedIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	//nolint:gosec // Safe: only formatting placeholder count, not user input
	deleteQuery := fmt.Sprintf(`
		DELETE FROM items
		WHERE feed_url = ?
		  AND archived = 1
		  AND %[1]s IS NOT NULL
		  AND %[1]s < julianday(?)
		  AND id NOT IN (%[2]s)
	`, effectiveDateExpression, strings.Join(placeholders, ","))

	result, err := db.conn.Exec(deleteQuery, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete items: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
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
	args = append(args, formatDatabaseTime(start), formatDatabaseTime(end))

	//nolint:gosec // Safe: only formatting placeholder count, not user input
	query := fmt.Sprintf(`
		SELECT id, feed_url, guid, title, link, published_date, first_seen,
			content, summary, archived, item_json
		FROM items
		WHERE feed_url IN (%s)
			AND %[2]s IS NOT NULL
			AND %[2]s >= julianday(?)
			AND %[2]s <= julianday(?)
		ORDER BY feed_url, %[2]s DESC
	`, strings.Join(placeholders, ","), effectiveDateExpression)

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
			scanNullableTime(&item.PublishedDate), &item.FirstSeen, &item.Content, &item.Summary, &item.Archived,
			&item.ItemJSON,
		)
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

type ItemFilter struct {
	FeedURL   string
	FeedQuery string
	Search    string
	Limit     int
	Since     time.Time
	Until     time.Time
	Unseen    bool
	Seen      bool
}

// GetItems retrieves items with filtering options.
func (db *DB) GetItems(filter *ItemFilter) ([]*Item, error) {
	query, args, filterTimesInGo := buildItemsQuery(filter)
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get items: %w", err)
	}
	defer rows.Close()

	var items []*Item
	for rows.Next() {
		var item Item
		if err := rows.Scan(
			&item.ID, &item.FeedURL, &item.GUID, &item.Title, &item.Link,
			scanNullableTime(&item.PublishedDate), &item.FirstSeen, &item.Content, &item.Summary,
			&item.Archived, &item.ItemJSON,
		); err != nil {
			return nil, fmt.Errorf("failed to scan item: %w", err)
		}
		if !itemInDiscoveryWindow(&item, filter.Since, filter.Until) {
			continue
		}
		items = append(items, &item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over items: %w", err)
	}

	if filterTimesInGo {
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].EffectiveDate().After(items[j].EffectiveDate())
		})
		if filter.Limit > 0 && len(items) > filter.Limit {
			items = items[:filter.Limit]
		}
	}

	return items, nil
}

func buildItemsQuery(filter *ItemFilter) (query string, args []interface{}, filterTimesInGo bool) {
	filterTimesInGo = !filter.Since.IsZero() || !filter.Until.IsZero()
	fromClause := "FROM items i"
	if filterTimesInGo {
		fromClause += " INDEXED BY idx_items_discovery_time"
	}
	query = `
		SELECT i.id, i.feed_url, i.guid, i.title, i.link, i.published_date, i.first_seen,
			i.content, i.summary, i.archived, i.item_json
		` + fromClause + "\n"
	var conditions []string

	if filter.Unseen {
		conditions = append(conditions,
			`NOT EXISTS (
					SELECT 1 FROM item_annotations a 
					WHERE a.feed_url = i.feed_url AND a.item_guid = i.guid AND a.kind = 'seen'
				)`)
	}
	if filter.Seen {
		conditions = append(conditions,
			`EXISTS (
					SELECT 1 FROM item_annotations a 
					WHERE a.feed_url = i.feed_url AND a.item_guid = i.guid AND a.kind = 'seen'
				)`)
	}
	if filter.FeedURL != "" {
		conditions = append(conditions, "i.feed_url = ?")
		args = append(args, filter.FeedURL)
	}
	if filter.FeedQuery != "" {
		conditions = append(conditions, "instr(lower(i.feed_url), lower(?)) > 0")
		args = append(args, filter.FeedQuery)
	}
	if filter.Search != "" {
		conditions = append(conditions, "instr(lower(i.title), lower(?)) > 0")
		args = append(args, filter.Search)
	}
	if !filter.Since.IsZero() {
		conditions = append(conditions, fmt.Sprintf(
			"(%[1]s IS NULL OR %[1]s >= julianday(?))", discoveryTimeExpression,
		))
		args = append(args, filter.Since.Format(time.RFC3339Nano))
	}
	if !filter.Until.IsZero() {
		conditions = append(conditions, fmt.Sprintf(
			"(%[1]s IS NULL OR %[1]s <= julianday(?))", discoveryTimeExpression,
		))
		args = append(args, filter.Until.Format(time.RFC3339Nano))
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY " + aliasedEffectiveDateExpression + " DESC"
	if !filterTimesInGo {
		if filter.Limit > 0 {
			query += sqlLimitClause
			args = append(args, filter.Limit)
		}
	}
	return query, args, filterTimesInGo
}

func itemInDiscoveryWindow(item *Item, since, until time.Time) bool {
	discoveredAt := item.PublishedDate
	if item.FirstSeen.Valid && !item.FirstSeen.Time.IsZero() {
		discoveredAt = item.FirstSeen.Time
	}
	if !since.IsZero() && !discoveredAt.After(since) {
		return false
	}
	return until.IsZero() || !discoveredAt.After(until)
}
