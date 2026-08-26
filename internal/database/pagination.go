package database

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// itemIDLength and feedIDLength mirror internal/ids. The hashes are duplicated
// rather than imported because internal/ids sits above this package in the
// dependency graph and importing it here would make the graph cyclic once the
// renderer and API both depend on both. A test in internal/ids asserts the two
// implementations agree.
const (
	feedHashIDLength = 8
	itemHashIDLength = 16
	itemHashIDSep    = "\n"
)

// FeedHashID derives a feed's public identifier. Keep in sync with ids.FeedID.
func FeedHashID(feedURL string) string {
	sum := sha256.Sum256([]byte(feedURL))
	return hex.EncodeToString(sum[:])[:feedHashIDLength]
}

// ItemHashID derives an item's public identifier. Keep in sync with ids.ItemID.
func ItemHashID(feedURL, guid string) string {
	sum := sha256.Sum256([]byte(feedURL + itemHashIDSep + guid))
	return hex.EncodeToString(sum[:])[:itemHashIDLength]
}

// ItemCursor is a position in the item ordering. DateRank is 0 for rows with a
// usable effective date and 1 for rows without one, so those sort as a single
// block at the tail instead of scattering. EffectiveDate is a julianday value
// and is meaningless when DateRank is 1.
type ItemCursor struct {
	DateRank      int
	EffectiveDate float64
	ID            int64
}

// ItemPage describes one page of an item query. A nil *bool means "no filter"
// rather than false, which is why Seen and Archived are pointers.
type ItemPage struct {
	FeedURL   string
	FeedQuery string
	Link      string
	Search    string
	Since     time.Time
	Until     time.Time
	Seen      *bool
	Archived  *bool
	Ascending bool
	Limit     int
	After     *ItemCursor
}

// FeedPage describes one page of a feed query, ordered by URL. URL is the
// primary key -- never NULL, always unique -- so the cursor needs no tiebreak.
type FeedPage struct {
	URL   string
	Limit int
	After string
}

const itemSelectColumns = `i.id, i.feed_url, i.guid, i.title, i.link, i.published_date,
	i.first_seen, i.content, i.summary, i.archived, i.item_json`

// aliasedDiscoveryTimeExpression mirrors discoveryTimeExpression with the item
// table alias applied. Spelled out rather than derived because the codebase
// already declares aliased and unaliased variants side by side;
// TestDiscoveryTimeExpressionsAgree keeps the two in step.
const aliasedDiscoveryTimeExpression = `julianday(CASE
	WHEN i.first_seen IS NULL OR substr(CAST(i.first_seen AS TEXT), 1, 10) = '0001-01-01'
	THEN i.published_date
	ELSE i.first_seen
END)`

// dateRankExpression is 0 when the effective date is usable and 1 when it is
// not, so ordering by it first keeps unorderable rows together at the tail.
const dateRankExpression = "CASE WHEN " + aliasedEffectiveDateExpression + " IS NULL THEN 1 ELSE 0 END"

// ListItems returns one page of items plus the cursor for the next page, or a
// nil cursor when the result set is exhausted.
//
// This exists alongside GetItems rather than replacing it: GetItems loads every
// matching row and filters and sorts in Go whenever a time window is set, which
// has no stable cursor position. The CLI depends on that behavior, so it stays.
func (db *DB) ListItems(page *ItemPage) ([]*Item, *ItemCursor, error) {
	limit := page.Limit
	if limit <= 0 {
		limit = 1
	}

	conditions, args := itemPageConditions(page)
	query := "SELECT " + itemSelectColumns + ", " + dateRankExpression + " AS date_rank, " +
		aliasedEffectiveDateExpression + " AS effective_date\n\tFROM items i"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	direction := "DESC"
	if page.Ascending {
		direction = "ASC"
	}
	// date_rank always ascends so undated rows stay at the tail in both
	// directions; only the date and id components flip.
	//nolint:gosec // direction is one of two literals above; all values are bound
	query += fmt.Sprintf(" ORDER BY date_rank ASC, effective_date %s, i.id %s", direction, direction)

	// Fetch one extra row to learn whether another page exists without a
	// second count query.
	query += " LIMIT ?"
	args = append(args, limit+1)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list items: %w", err)
	}
	defer rows.Close()

	items := []*Item{}
	cursors := []ItemCursor{}
	for rows.Next() {
		var item Item
		var cursor ItemCursor
		var effectiveDate sql.NullFloat64
		if err := rows.Scan(
			&item.ID, &item.FeedURL, &item.GUID, &item.Title, &item.Link,
			scanNullableTime(&item.PublishedDate), &item.FirstSeen, &item.Content,
			&item.Summary, &item.Archived, &item.ItemJSON,
			&cursor.DateRank, &effectiveDate,
		); err != nil {
			return nil, nil, fmt.Errorf("failed to scan item: %w", err)
		}
		cursor.EffectiveDate = effectiveDate.Float64
		cursor.ID = item.ID
		items = append(items, &item)
		cursors = append(cursors, cursor)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("error iterating over items: %w", err)
	}

	if len(items) <= limit {
		return items, nil, nil
	}
	next := cursors[limit-1]
	return items[:limit], &next, nil
}

func itemPageConditions(page *ItemPage) (conditions []string, args []interface{}) {
	if page.After != nil {
		condition, cursorArgs := itemCursorCondition(page.After, page.Ascending)
		conditions = append(conditions, condition)
		args = append(args, cursorArgs...)
	}
	if page.FeedURL != "" {
		conditions = append(conditions, "i.feed_url = ?")
		args = append(args, page.FeedURL)
	}
	if page.FeedQuery != "" {
		conditions = append(conditions, "instr(lower(i.feed_url), lower(?)) > 0")
		args = append(args, page.FeedQuery)
	}
	if page.Link != "" {
		conditions = append(conditions, "i.link = ?")
		args = append(args, page.Link)
	}
	if page.Search != "" {
		// Matches ItemFilter.Search exactly: title substring, case-insensitive.
		conditions = append(conditions, "instr(lower(i.title), lower(?)) > 0")
		args = append(args, page.Search)
	}
	if page.Seen != nil {
		existence := "EXISTS"
		if !*page.Seen {
			existence = "NOT EXISTS"
		}
		conditions = append(conditions, existence+` (
			SELECT 1 FROM item_annotations a
			WHERE a.feed_url = i.feed_url AND a.item_guid = i.guid AND a.kind = 'seen'
		)`)
	}
	if page.Archived != nil {
		conditions = append(conditions, "i.archived = ?")
		args = append(args, *page.Archived)
	}
	if !page.Since.IsZero() {
		conditions = append(conditions, fmt.Sprintf(
			"(%[1]s IS NULL OR %[1]s >= julianday(?))", aliasedDiscoveryTimeExpression,
		))
		args = append(args, page.Since.Format(time.RFC3339Nano))
	}
	if !page.Until.IsZero() {
		conditions = append(conditions, fmt.Sprintf(
			"(%[1]s IS NULL OR %[1]s <= julianday(?))", aliasedDiscoveryTimeExpression,
		))
		args = append(args, page.Until.Format(time.RFC3339Nano))
	}
	return conditions, args
}

// itemCursorCondition builds the keyset predicate for "strictly after this
// position". It is written out rather than as a row-value comparison because
// date_rank ascends while the date component may descend, and a single
// row-value comparison cannot express mixed directions.
func itemCursorCondition(cursor *ItemCursor, ascending bool) (condition string, args []interface{}) {
	comparison := "<"
	if ascending {
		comparison = ">"
	}

	rankExpr := dateRankExpression
	dateExpr := aliasedEffectiveDateExpression

	// Rows in a later rank block always come after, regardless of direction.
	laterBlock := fmt.Sprintf("%s > ?", rankExpr)
	args = append(args, cursor.DateRank)

	if cursor.DateRank == 1 {
		// The effective date is NULL here, so only the id can order these.
		condition = fmt.Sprintf("(%s OR (%s = ? AND i.id %s ?))", laterBlock, rankExpr, comparison)
		args = append(args, cursor.DateRank, cursor.ID)
		return condition, args
	}

	condition = fmt.Sprintf(
		"(%s OR (%s = ? AND %s %s ?) OR (%s = ? AND %s = ? AND i.id %s ?))",
		laterBlock,
		rankExpr, dateExpr, comparison,
		rankExpr, dateExpr, comparison,
	)
	args = append(
		args,
		cursor.DateRank, cursor.EffectiveDate,
		cursor.DateRank, cursor.EffectiveDate, cursor.ID,
	)
	return condition, args
}

// ListFeeds returns one page of feeds ordered by URL, plus the URL to resume
// after. The second return is empty when the result set is exhausted.
func (db *DB) ListFeeds(page *FeedPage) ([]*Feed, string, error) {
	limit := page.Limit
	if limit <= 0 {
		limit = 1
	}

	query := `SELECT url, title, description, last_updated, etag, last_modified,
		last_fetch_time, last_successful_fetch, error_count, user_agent, type,
		scrape_selector, last_error, latest_item_date, feed_json
		FROM feeds`
	var conditions []string
	var args []interface{}
	if page.URL != "" {
		conditions = append(conditions, "url = ?")
		args = append(args, page.URL)
	}
	if page.After != "" {
		conditions = append(conditions, "url > ?")
		args = append(args, page.After)
	}
	if len(conditions) > 0 {
		//nolint:gosec // conditions are built from a fixed set of literals; values are bound
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY url ASC LIMIT ?"
	args = append(args, limit+1)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list feeds: %w", err)
	}
	defer rows.Close()

	feeds := []*Feed{}
	for rows.Next() {
		feed, err := scanFeedRow(rows)
		if err != nil {
			return nil, "", err
		}
		feeds = append(feeds, feed)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("error iterating over feeds: %w", err)
	}

	if len(feeds) <= limit {
		return feeds, "", nil
	}
	return feeds[:limit], feeds[limit-1].URL, nil
}

func scanFeedRow(rows *sql.Rows) (*Feed, error) {
	var feed Feed
	if err := rows.Scan(
		&feed.URL, &feed.Title, &feed.Description,
		scanNullableTime(&feed.LastUpdated), &feed.ETag, &feed.LastModified,
		scanNullableTime(&feed.LastFetchTime), scanNullableTime(&feed.LastSuccessfulFetch),
		&feed.ErrorCount, &feed.UserAgent, &feed.Type, &feed.ScrapeSelector,
		&feed.LastError, &feed.LatestItemDate, &feed.FeedJSON,
	); err != nil {
		return nil, fmt.Errorf("failed to scan feed: %w", err)
	}
	return &feed, nil
}

// GetFeedByHashID resolves a feed from its derived public ID.
//
// A hash cannot be indexed, so this scans the feed URLs. There are tens to
// hundreds of feeds, so the scan is trivial; the item equivalent below is the
// one worth watching.
func (db *DB) GetFeedByHashID(id string) (*Feed, error) {
	urls, err := db.GetFeedURLs()
	if err != nil {
		return nil, err
	}
	for _, url := range urls {
		if FeedHashID(url) == id {
			return db.GetFeed(url)
		}
	}
	return nil, nil
}

// GetItemByHashID resolves an item from its derived public ID.
//
// This scans (feed_url, guid) pairs and hashes each one, because the ID is
// derived rather than stored and so cannot be indexed. At personal scale --
// tens of thousands of items -- that is milliseconds. If it ever stops being
// cheap, the fix is a stored generated column with an index on it, not a
// different ID scheme.
func (db *DB) GetItemByHashID(id string) (*Item, error) {
	rows, err := db.conn.Query(`SELECT feed_url, guid FROM items`)
	if err != nil {
		return nil, fmt.Errorf("failed to scan items for id: %w", err)
	}
	defer rows.Close()

	var matchedFeedURL, matchedGUID string
	found := false
	for rows.Next() {
		var feedURL, guid string
		if err := rows.Scan(&feedURL, &guid); err != nil {
			return nil, fmt.Errorf("failed to scan item key: %w", err)
		}
		if ItemHashID(feedURL, guid) == id {
			matchedFeedURL, matchedGUID = feedURL, guid
			found = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over item keys: %w", err)
	}
	if !found {
		return nil, nil
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("failed to close item key rows: %w", err)
	}

	return db.getItemByKey(matchedFeedURL, matchedGUID)
}

func (db *DB) getItemByKey(feedURL, guid string) (*Item, error) {
	var item Item
	err := db.conn.QueryRow(`
		SELECT id, feed_url, guid, title, link, published_date, first_seen,
			content, summary, archived, item_json
		FROM items WHERE feed_url = ? AND guid = ?
	`, feedURL, guid).Scan(
		&item.ID, &item.FeedURL, &item.GUID, &item.Title, &item.Link,
		scanNullableTime(&item.PublishedDate), &item.FirstSeen, &item.Content,
		&item.Summary, &item.Archived, &item.ItemJSON,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get item: %w", err)
	}
	return &item, nil
}

// CountItemsForFeed returns the total and unseen item counts for one feed.
func (db *DB) CountItemsForFeed(feedURL string) (total, unseen int, err error) {
	err = db.conn.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN NOT EXISTS (
				SELECT 1 FROM item_annotations a
				WHERE a.feed_url = i.feed_url AND a.item_guid = i.guid AND a.kind = 'seen'
			) THEN 1 ELSE 0 END), 0)
		FROM items i WHERE i.feed_url = ?
	`, feedURL).Scan(&total, &unseen)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to count items for feed: %w", err)
	}
	return total, unseen, nil
}
