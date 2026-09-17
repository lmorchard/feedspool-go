package database

import (
	"context"
	"fmt"
	"time"
)

type TopicRun struct {
	ID           int64
	CreatedAt    time.Time
	WindowStart  time.Time
	WindowEnd    time.Time
	EmbedModelID string
	LLMModelID   string
}

type Topic struct {
	ID    int64
	RunID int64
	Label string
	Score float64
}

type TopicItem struct {
	TopicID int64
	ItemID  int64
}

// InsertTopicRun inserts a topic run and all its computed topics and items in a single transaction.
func (db *DB) InsertTopicRun(
	ctx context.Context,
	run *TopicRun,
	topics []*Topic,
	topicItems map[*Topic][]int64,
) error {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollbackUnlessDone(tx, "InsertTopicRun")

	res, err := tx.ExecContext(ctx, `
		INSERT INTO topic_runs (created_at, window_start, window_end, embed_model_id, llm_model_id)
		VALUES (?, ?, ?, ?, ?)
	`, formatDatabaseTime(run.CreatedAt), formatDatabaseTime(run.WindowStart), formatDatabaseTime(run.WindowEnd),
		run.EmbedModelID, run.LLMModelID)
	if err != nil {
		return fmt.Errorf("failed to insert topic_run: %w", err)
	}

	runID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get run_id: %w", err)
	}
	run.ID = runID

	for _, topic := range topics {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO topics (run_id, label, score)
			VALUES (?, ?, ?)
		`, runID, topic.Label, topic.Score)
		if err != nil {
			return fmt.Errorf("failed to insert topic: %w", err)
		}

		topicID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("failed to get topic_id: %w", err)
		}
		topic.ID = topicID
		topic.RunID = runID

		items := topicItems[topic]
		for _, itemID := range items {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO topic_items (topic_id, item_id)
				VALUES (?, ?)
			`, topicID, itemID)
			if err != nil {
				return fmt.Errorf("failed to insert topic_item: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// GetLatestTopicRun fetches the most recent topic run from the database.
// Returns nil, nil if there are no topic runs.
func (db *DB) GetLatestTopicRun(ctx context.Context) (*TopicRun, error) {
	row := db.conn.QueryRowContext(ctx, `
		SELECT id, created_at, window_start, window_end, embed_model_id, llm_model_id
		FROM topic_runs
		ORDER BY created_at DESC
		LIMIT 1
	`)

	var run TopicRun
	var createdAt, windowStart, windowEnd string
	err := row.Scan(&run.ID, &createdAt, &windowStart, &windowEnd, &run.EmbedModelID, &run.LLMModelID)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to scan latest topic run: %w", err)
	}

	run.CreatedAt, _ = parseDatabaseTime(createdAt)
	run.WindowStart, _ = parseDatabaseTime(windowStart)
	run.WindowEnd, _ = parseDatabaseTime(windowEnd)

	return &run, nil
}

// GetTopicsForRun retrieves all topics associated with a specific run, sorted by score descending.
func (db *DB) GetTopicsForRun(ctx context.Context, runID int64) ([]*Topic, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT id, run_id, label, score
		FROM topics
		WHERE run_id = ?
		ORDER BY score DESC
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to query topics for run: %w", err)
	}
	defer rows.Close()

	var topics []*Topic
	for rows.Next() {
		var t Topic
		if err := rows.Scan(&t.ID, &t.RunID, &t.Label, &t.Score); err != nil {
			return nil, fmt.Errorf("failed to scan topic: %w", err)
		}
		topics = append(topics, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating topics: %w", err)
	}

	return topics, nil
}

// GetTopicItems retrieves all item IDs for the topics in a given run, mapped by Topic ID.
func (db *DB) GetTopicItems(ctx context.Context, runID int64) (map[int64][]int64, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT ti.topic_id, ti.item_id
		FROM topic_items ti
		JOIN topics t ON ti.topic_id = t.id
		WHERE t.run_id = ?
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to query topic items: %w", err)
	}
	defer rows.Close()

	topicItems := make(map[int64][]int64)
	for rows.Next() {
		var topicID, itemID int64
		if err := rows.Scan(&topicID, &itemID); err != nil {
			return nil, fmt.Errorf("failed to scan topic item: %w", err)
		}
		topicItems[topicID] = append(topicItems[topicID], itemID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating topic items: %w", err)
	}

	return topicItems, nil
}

// DeleteTopicRuns deletes topic runs created before cutoffTime.
// If keepLatest is true, the single most recent topic run is preserved even if created before cutoffTime.
// Returns the number of deleted topic runs.
func (db *DB) DeleteTopicRuns(ctx context.Context, cutoffTime time.Time, keepLatest bool) (int64, error) {
	cutoffStr := formatDatabaseTime(cutoffTime)

	var query string
	if keepLatest {
		query = `
			DELETE FROM topic_runs
			WHERE created_at < ?
			AND id NOT IN (
				SELECT id FROM topic_runs ORDER BY created_at DESC LIMIT 1
			)
		`
	} else {
		query = `
			DELETE FROM topic_runs
			WHERE created_at < ?
		`
	}

	res, err := db.conn.ExecContext(ctx, query, cutoffStr)
	if err != nil {
		return 0, fmt.Errorf("failed to delete topic runs: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get deleted topic runs count: %w", err)
	}

	return rows, nil
}
