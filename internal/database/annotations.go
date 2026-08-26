package database

import (
	"database/sql"
	"fmt"
)

type ItemAnnotation struct {
	FeedURL   string
	ItemGUID  string
	Kind      string
	Value     sql.NullString
	Actor     sql.NullString
	CreatedAt string
}

func (db *DB) AddAnnotation(feedURL, itemGUID, kind string, value, actor sql.NullString) error {
	// ON CONFLICT DO NOTHING without a target matches any unique index, which
	// is what the COALESCE(value, '') expression index from migration 10
	// requires -- naming a target would mean restating the expression.
	query := `
		INSERT INTO item_annotations (feed_url, item_guid, kind, value, actor)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING
	`
	_, err := db.conn.Exec(query, feedURL, itemGUID, kind, value, actor)
	if err != nil {
		return fmt.Errorf("failed to add annotation: %w", err)
	}
	return nil
}

func (db *DB) RemoveAnnotation(feedURL, itemGUID, kind string, value sql.NullString) error {
	var query string
	var err error

	if value.Valid {
		query = `DELETE FROM item_annotations WHERE feed_url = ? AND item_guid = ? AND kind = ? AND value = ?`
		_, err = db.conn.Exec(query, feedURL, itemGUID, kind, value)
	} else {
		query = `DELETE FROM item_annotations WHERE feed_url = ? AND item_guid = ? AND kind = ? AND value IS NULL`
		_, err = db.conn.Exec(query, feedURL, itemGUID, kind)
	}

	if err != nil {
		return fmt.Errorf("failed to remove annotation: %w", err)
	}
	return nil
}

func (db *DB) GetAnnotations(feedURL, itemGUID string) ([]ItemAnnotation, error) {
	query := `
		SELECT feed_url, item_guid, kind, value, actor, created_at 
		FROM item_annotations 
		WHERE feed_url = ? AND item_guid = ? 
		ORDER BY created_at
	`
	rows, err := db.conn.Query(query, feedURL, itemGUID)
	if err != nil {
		return nil, fmt.Errorf("failed to query annotations: %w", err)
	}
	defer rows.Close()

	var annotations []ItemAnnotation
	for rows.Next() {
		var a ItemAnnotation
		if err := rows.Scan(&a.FeedURL, &a.ItemGUID, &a.Kind, &a.Value, &a.Actor, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan annotation: %w", err)
		}
		annotations = append(annotations, a)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over annotations: %w", err)
	}

	return annotations, nil
}

// AnnotationExists reports whether an identical annotation is already stored.
// The API uses it to distinguish 201-on-create from 200-on-already-present,
// which AddAnnotation alone cannot answer now that it is idempotent.
func (db *DB) AnnotationExists(feedURL, itemGUID, kind string, value sql.NullString) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM item_annotations
			WHERE feed_url = ? AND item_guid = ? AND kind = ?
				AND COALESCE(value, '') = COALESCE(?, '')
		)
	`
	var exists bool
	if err := db.conn.QueryRow(query, feedURL, itemGUID, kind, value).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to check annotation: %w", err)
	}
	return exists, nil
}
