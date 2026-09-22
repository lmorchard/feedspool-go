package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lmorchard/feedspool-go/internal/lineage"
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

	// Lineage, written by InsertTopicRun. ThreadID 0 on input means "open a
	// new thread"; SetHash and LabelSource are filled in when empty.
	ThreadID    int64
	SetHash     string // lineage.SetHash of the topic's item IDs
	LabelSource string // lineage.SourceGenerated or lineage.SourceInherited
	// ThreadIsNew is transient: true when this insert opened the thread.
	ThreadIsNew bool
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

		if err := writeTopicLineage(ctx, tx, run.CreatedAt, topic, items); err != nil {
			return err
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
		SELECT t.id, t.run_id, t.label, t.score,
		       COALESCE(l.thread_id, 0), COALESCE(l.set_hash, ''), COALESCE(l.label_source, '')
		FROM topics t
		LEFT JOIN topic_lineage l ON l.topic_id = t.id
		WHERE t.run_id = ?
		ORDER BY t.score DESC
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to query topics for run: %w", err)
	}
	defer rows.Close()

	var topics []*Topic
	for rows.Next() {
		var t Topic
		if err := rows.Scan(&t.ID, &t.RunID, &t.Label, &t.Score, &t.ThreadID, &t.SetHash, &t.LabelSource); err != nil {
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

	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollbackUnlessDone(tx, "DeleteTopicRuns")

	res, err := tx.ExecContext(ctx, query, cutoffStr)
	if err != nil {
		return 0, fmt.Errorf("failed to delete topic runs: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get deleted topic runs count: %w", err)
	}

	// A thread whose last topic just cascaded away can never be matched
	// again (matching reads topics rows), so it is dead weight; remove it.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM topic_threads
		WHERE id NOT IN (SELECT thread_id FROM topic_lineage)
	`); err != nil {
		return 0, fmt.Errorf("failed to delete orphaned topic threads: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return rows, nil
}

// writeTopicLineage creates or updates the topic's thread and inserts its
// lineage row, inside the caller's transaction. InsertTopicRun and the
// migration 14 backfill both go through here, so live runs and the replay of
// historical runs cannot disagree on what a thread is.
//
// A generated label on a surviving thread replaces the thread's label and
// moves labeled_at; an inherited label touches only last_seen_at. An unknown
// ThreadID fails on the topic_lineage foreign key rather than silently
// attaching to nothing.
func writeTopicLineage(ctx context.Context, tx *sql.Tx, runAt time.Time, topic *Topic, items []int64) error {
	if topic.SetHash == "" {
		topic.SetHash = lineage.SetHash(items)
	}
	if topic.LabelSource == "" {
		topic.LabelSource = lineage.SourceGenerated
	}
	at := formatDatabaseTime(runAt)

	switch {
	case topic.ThreadID == 0:
		res, err := tx.ExecContext(ctx, `
			INSERT INTO topic_threads (first_seen_at, last_seen_at, label, labeled_at)
			VALUES (?, ?, ?, ?)
		`, at, at, topic.Label, at)
		if err != nil {
			return fmt.Errorf("failed to open topic thread: %w", err)
		}
		threadID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("failed to get thread_id: %w", err)
		}
		topic.ThreadID = threadID
		topic.ThreadIsNew = true
	case topic.LabelSource == lineage.SourceGenerated:
		if _, err := tx.ExecContext(ctx, `
			UPDATE topic_threads SET last_seen_at = ?, label = ?, labeled_at = ? WHERE id = ?
		`, at, topic.Label, at, topic.ThreadID); err != nil {
			return fmt.Errorf("failed to relabel topic thread %d: %w", topic.ThreadID, err)
		}
	default:
		if _, err := tx.ExecContext(ctx, `
			UPDATE topic_threads SET last_seen_at = ? WHERE id = ?
		`, at, topic.ThreadID); err != nil {
			return fmt.Errorf("failed to touch topic thread %d: %w", topic.ThreadID, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO topic_lineage (topic_id, thread_id, set_hash, label_source)
		VALUES (?, ?, ?, ?)
	`, topic.ID, topic.ThreadID, topic.SetHash, topic.LabelSource); err != nil {
		return fmt.Errorf("failed to insert topic lineage for topic %d: %w", topic.ID, err)
	}
	return nil
}

// GetLineageCandidates returns every threaded topic in the `lookback` most
// recent runs created strictly before `before`, with ascending item IDs. A
// live run passes its own CreatedAt; the migration backfill passes each
// historical run's CreatedAt so it sees only that run's past.
func (db *DB) GetLineageCandidates(ctx context.Context, before time.Time, lookback int) ([]lineage.Candidate, error) {
	return lineageCandidates(ctx, db.conn, before, lookback)
}

// queryer is satisfied by *sql.DB and *sql.Tx. The backfill must read
// candidates through its open transaction: with SetMaxOpenConns(1), a db.conn
// query while a transaction is open waits out busy_timeout and then fails.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func lineageCandidates(ctx context.Context, q queryer, before time.Time, lookback int) ([]lineage.Candidate, error) {
	rows, err := q.QueryContext(ctx, `
		WITH recent AS (
			SELECT id FROM topic_runs WHERE created_at < ? ORDER BY created_at DESC LIMIT ?
		)
		SELECT t.id, l.thread_id, th.label, l.set_hash, ti.item_id
		FROM topics t
		JOIN recent r         ON r.id = t.run_id
		JOIN topic_lineage l  ON l.topic_id = t.id
		JOIN topic_threads th ON th.id = l.thread_id
		JOIN topic_items ti   ON ti.topic_id = t.id
		ORDER BY t.id, ti.item_id
	`, formatDatabaseTime(before), lookback)
	if err != nil {
		return nil, fmt.Errorf("failed to query lineage candidates: %w", err)
	}
	defer rows.Close()

	var out []lineage.Candidate
	var current *lineage.Candidate
	for rows.Next() {
		var topicID, threadID, itemID int64
		var label, hash string
		if err := rows.Scan(&topicID, &threadID, &label, &hash, &itemID); err != nil {
			return nil, fmt.Errorf("failed to scan lineage candidate: %w", err)
		}
		if current == nil || current.TopicID != topicID {
			out = append(out, lineage.Candidate{TopicID: topicID, ThreadID: threadID, ThreadLabel: label, Hash: hash})
			current = &out[len(out)-1]
		}
		current.Items = append(current.Items, itemID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating lineage candidates: %w", err)
	}
	return out, nil
}

// int64Placeholders returns "?,?,..." and the bound args for an IN list.
func int64Placeholders(ids []int64) (placeholders string, args []any) {
	args = make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}

// GetPreviousThreadItems returns, for each thread, the ascending item IDs of
// its topic in the most recent run created strictly before `before`. Threads
// with no earlier topic are absent. It ignores the lineage lookback on
// purpose: trend reporting diffs against where a thread last was, however
// long ago that was.
func (db *DB) GetPreviousThreadItems(
	ctx context.Context, before time.Time, threadIDs []int64,
) (map[int64][]int64, error) {
	out := make(map[int64][]int64)
	if len(threadIDs) == 0 {
		return out, nil
	}
	placeholders, args := int64Placeholders(threadIDs)
	args = append([]any{formatDatabaseTime(before)}, args...)

	//nolint:gosec // Safe: only formatting placeholder count, not user input
	query := `
		WITH ranked AS (
			SELECT l.thread_id, l.topic_id,
			       ROW_NUMBER() OVER (
			           PARTITION BY l.thread_id ORDER BY r.created_at DESC, t.id DESC
			       ) AS rn
			FROM topic_lineage l
			JOIN topics t     ON t.id = l.topic_id
			JOIN topic_runs r ON r.id = t.run_id
			WHERE r.created_at < ? AND l.thread_id IN (` + placeholders + `)
		)
		SELECT ranked.thread_id, ti.item_id
		FROM ranked
		JOIN topic_items ti ON ti.topic_id = ranked.topic_id
		WHERE ranked.rn = 1
		ORDER BY ranked.thread_id, ti.item_id`
	rows, err := db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query previous thread items: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var threadID, itemID int64
		if err := rows.Scan(&threadID, &itemID); err != nil {
			return nil, fmt.Errorf("failed to scan previous thread item: %w", err)
		}
		out[threadID] = append(out[threadID], itemID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating previous thread items: %w", err)
	}
	return out, nil
}

// GetThreadFirstSeen returns topic_threads.first_seen_at for each thread that
// exists. Unknown IDs are absent from the map.
func (db *DB) GetThreadFirstSeen(ctx context.Context, threadIDs []int64) (map[int64]time.Time, error) {
	out := make(map[int64]time.Time)
	if len(threadIDs) == 0 {
		return out, nil
	}
	placeholders, args := int64Placeholders(threadIDs)

	//nolint:gosec // Safe: only formatting placeholder count, not user input
	query := `SELECT id, first_seen_at FROM topic_threads WHERE id IN (` + placeholders + `)`
	rows, err := db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query thread first-seen times: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, fmt.Errorf("failed to scan thread first-seen time: %w", err)
		}
		seen, err := parseDatabaseTime(raw)
		if err != nil {
			return nil, fmt.Errorf("thread %d has unparseable first_seen_at %q: %w", id, raw, err)
		}
		out[id] = seen
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating thread first-seen times: %w", err)
	}
	return out, nil
}
