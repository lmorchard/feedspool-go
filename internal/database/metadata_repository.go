package database

import (
	"database/sql"
	"fmt"
	"time"
)

// GetMetadata retrieves metadata for a URL
func (db *DB) GetMetadata(url string) (*URLMetadata, error) {
	var metadata URLMetadata
	query := `
		SELECT url, title, description, image_url, favicon_url, metadata,
		       last_fetch_at, fetch_status_code, fetch_error, created_at, updated_at
		FROM url_metadata
		WHERE url = ?
	`

	err := db.conn.QueryRow(query, url).Scan(
		&metadata.URL,
		&metadata.Title,
		&metadata.Description,
		&metadata.ImageURL,
		&metadata.FaviconURL,
		&metadata.Metadata,
		&metadata.LastFetchAt,
		&metadata.FetchStatusCode,
		&metadata.FetchError,
		&metadata.CreatedAt,
		&metadata.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata: %w", err)
	}

	return &metadata, nil
}

// UpsertMetadata inserts or updates URL metadata
func (db *DB) UpsertMetadata(metadata *URLMetadata) error {
	query := `
		INSERT INTO url_metadata (
			url, title, description, image_url, favicon_url, metadata,
			last_fetch_at, fetch_status_code, fetch_error, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(url) DO UPDATE SET
			title = excluded.title,
			description = excluded.description,
			image_url = excluded.image_url,
			favicon_url = excluded.favicon_url,
			metadata = excluded.metadata,
			last_fetch_at = excluded.last_fetch_at,
			fetch_status_code = excluded.fetch_status_code,
			fetch_error = excluded.fetch_error,
			updated_at = CURRENT_TIMESTAMP
	`

	_, err := db.conn.Exec(query,
		metadata.URL,
		metadata.Title,
		metadata.Description,
		metadata.ImageURL,
		metadata.FaviconURL,
		metadata.Metadata,
		metadata.LastFetchAt,
		metadata.FetchStatusCode,
		metadata.FetchError,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert metadata: %w", err)
	}

	return nil
}

// GetURLsNeedingFetch finds item URLs without metadata or due for retry
func (db *DB) GetURLsNeedingFetch(limit int, retryAfter time.Duration) ([]string, error) {
	retryTime := time.Now().Add(-retryAfter)

	query := `
		SELECT DISTINCT i.link
		FROM items i
		LEFT JOIN url_metadata um ON i.link = um.url
		WHERE i.link != '' 
		AND i.archived = 0
		AND (
			um.url IS NULL  -- No metadata exists
			OR (
				um.fetch_status_code NOT BETWEEN 200 AND 299  -- Failed fetch
				AND um.last_fetch_at < ?  -- And enough time has passed
			)
		)
		ORDER BY i.published_date DESC
	`

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := db.conn.Query(query, retryTime)
	if err != nil {
		return nil, fmt.Errorf("failed to get URLs needing fetch: %w", err)
	}
	defer rows.Close()

	var urls []string
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, fmt.Errorf("failed to scan URL: %w", err)
		}
		urls = append(urls, url)
	}

	return urls, rows.Err()
}

// DeleteOrphanedMetadata removes metadata for URLs with no item references
func (db *DB) DeleteOrphanedMetadata() (int64, error) {
	query := `
		DELETE FROM url_metadata
		WHERE url NOT IN (
			SELECT DISTINCT link FROM items WHERE link != ''
		)
	`

	result, err := db.conn.Exec(query)
	if err != nil {
		return 0, fmt.Errorf("failed to delete orphaned metadata: %w", err)
	}

	return result.RowsAffected()
}

// GetMetadataForItems retrieves metadata for multiple item URLs
func (db *DB) GetMetadataForItems(feedURL string) (map[string]*URLMetadata, error) {
	query := `
		SELECT um.url, um.title, um.description, um.image_url, um.favicon_url, 
		       um.metadata, um.last_fetch_at, um.fetch_status_code, um.fetch_error,
		       um.created_at, um.updated_at
		FROM url_metadata um
		INNER JOIN items i ON i.link = um.url
		WHERE i.feed_url = ? AND i.archived = 0
	`

	rows, err := db.conn.Query(query, feedURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata for items: %w", err)
	}
	defer rows.Close()

	metadataMap := make(map[string]*URLMetadata)
	for rows.Next() {
		var metadata URLMetadata
		err := rows.Scan(
			&metadata.URL,
			&metadata.Title,
			&metadata.Description,
			&metadata.ImageURL,
			&metadata.FaviconURL,
			&metadata.Metadata,
			&metadata.LastFetchAt,
			&metadata.FetchStatusCode,
			&metadata.FetchError,
			&metadata.CreatedAt,
			&metadata.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan metadata: %w", err)
		}
		metadataMap[metadata.URL] = &metadata
	}

	return metadataMap, rows.Err()
}

// GetFeedFavicon retrieves the most common favicon URL for a feed's items
func (db *DB) GetFeedFavicon(feedURL string) (string, error) {
	query := `
		SELECT um.favicon_url
		FROM url_metadata um
		INNER JOIN items i ON i.link = um.url
		WHERE i.feed_url = ? 
		AND i.archived = 0
		AND um.favicon_url IS NOT NULL 
		AND um.favicon_url != ''
		GROUP BY um.favicon_url
		ORDER BY COUNT(*) DESC
		LIMIT 1
	`

	var faviconURL sql.NullString
	err := db.conn.QueryRow(query, feedURL).Scan(&faviconURL)

	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get feed favicon: %w", err)
	}

	if faviconURL.Valid {
		return faviconURL.String, nil
	}
	return "", nil
}
