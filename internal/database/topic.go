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
