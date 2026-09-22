package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/lmorchard/feedspool-go/internal/lineage"
)

// BackfillTopicLineage assigns a thread to every topic that has no lineage
// row, replaying runs oldest first so each run sees only its own past. It is
// idempotent (NOT EXISTS on topic_lineage), makes no network call, and goes
// through the same writeTopicLineage and lineage.Assign as a live run, so the
// replay of history and the next live run cannot disagree on what a thread is.
//
// The backfill passes Inherit: false regardless of configuration: every
// historical label was LLM-generated, so each survivor relabels its thread in
// turn and a thread ends up carrying the label of its most recent run --
// nothing on the page changes at upgrade. (The alternative, freezing each
// thread's earliest label as forward inheritance would have, is Inherit: true
// in backfillOptions and nothing else. Les leans weakly that way.)
//
// Attach threshold and lookback come from SetLineageOptions when a command
// installed them, else the lineage defaults, so history is threaded by the
// same rule the next live run applies.
func (db *DB) BackfillTopicLineage(ctx context.Context, progress func(done, total int64)) error {
	runs, err := db.listTopicRunsAscending(ctx)
	if err != nil {
		return err
	}
	for i := range runs {
		run := &runs[i]
		// IsInitialized runs migrations, so a `serve` and a cron `fetch` can
		// both enter this backfill on the first post-upgrade open. Each run's
		// transaction begins deferred, so both may read the same unthreaded
		// topics; whichever commits second fails its write with
		// SQLITE_BUSY_SNAPSHOT (a BUSY subcode, so isSQLiteBusy matches it).
		// Rolling back and re-running the whole run is the correct recovery:
		// the NOT EXISTS predicate then finds those topics already threaded.
		retryCtx, cancel := context.WithTimeout(ctx, sqliteBusyTimeout)
		err := retrySQLiteBusy(retryCtx, func() error { return db.backfillRunLineage(ctx, run) })
		cancel()
		if err != nil {
			return fmt.Errorf("topic run %d: %w", run.ID, err)
		}
		if progress != nil {
			progress(int64(i+1), int64(len(runs)))
		}
	}
	return nil
}

func (db *DB) listTopicRunsAscending(ctx context.Context) ([]TopicRun, error) {
	rows, err := db.conn.QueryContext(ctx, `SELECT id, created_at FROM topic_runs ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list topic runs: %w", err)
	}
	defer rows.Close()

	var runs []TopicRun
	for rows.Next() {
		var run TopicRun
		var createdAt string
		if err := rows.Scan(&run.ID, &createdAt); err != nil {
			return nil, fmt.Errorf("failed to scan topic run: %w", err)
		}
		if run.CreatedAt, err = parseDatabaseTime(createdAt); err != nil {
			return nil, fmt.Errorf("topic run %d has unparseable created_at %q: %w", run.ID, createdAt, err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating topic runs: %w", err)
	}
	return runs, nil
}

// backfillRunLineage threads one run's unthreaded topics in a transaction.
// Candidates are read through that transaction: with SetMaxOpenConns(1) a
// db.conn query here would wait out busy_timeout and fail.
func (db *DB) backfillRunLineage(ctx context.Context, run *TopicRun) error {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollbackUnlessDone(tx, "topic lineage backfill")

	topics, err := unthreadedTopics(ctx, tx, run.ID)
	if err != nil {
		return err
	}
	if len(topics) == 0 {
		return tx.Commit()
	}

	clusters, err := topicItemSets(ctx, tx, topics)
	if err != nil {
		return err
	}

	candidates, err := lineageCandidates(ctx, tx, run.CreatedAt, db.backfillLookback())
	if err != nil {
		return err
	}
	assignments := lineage.Assign(clusters, candidates, db.backfillOptions())

	for i, topic := range topics {
		topic.ThreadID = assignments[i].ThreadID
		topic.SetHash = assignments[i].Hash
		topic.LabelSource = lineage.SourceGenerated
		if err := writeTopicLineage(ctx, tx, run.CreatedAt, topic, clusters[i]); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// SetLineageOptions installs the configured matching rule for the migration 14
// backfill. Call it before IsInitialized, which is what migrates.
func (db *DB) SetLineageOptions(opts lineage.Options, lookback int) {
	db.lineageOptions = opts
	db.lineageLookback = lookback
}

// backfillOptions is the configured rule with Inherit forced off (see
// BackfillTopicLineage), or the lineage defaults when nothing was installed.
func (db *DB) backfillOptions() lineage.Options {
	opts := db.lineageOptions
	if opts.AttachThreshold == 0 {
		opts.AttachThreshold = lineage.DefaultAttachThreshold
	}
	if opts.InheritThreshold == 0 {
		opts.InheritThreshold = lineage.DefaultInheritThreshold
	}
	opts.Inherit = false
	return opts
}

func (db *DB) backfillLookback() int {
	if db.lineageLookback <= 0 {
		return lineage.DefaultLookback
	}
	return db.lineageLookback
}

// unthreadedTopics lists a run's topics without a lineage row, largest first
// -- the same order lineage.Assign processes clusters in, so a contested
// thread resolves the way it would have live.
func unthreadedTopics(ctx context.Context, tx *sql.Tx, runID int64) ([]*Topic, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT t.id, t.label, t.score
		FROM topics t
		WHERE t.run_id = ?
		  AND NOT EXISTS (SELECT 1 FROM topic_lineage l WHERE l.topic_id = t.id)
		ORDER BY t.score DESC, t.id ASC
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to query unthreaded topics: %w", err)
	}
	defer rows.Close()

	var topics []*Topic
	for rows.Next() {
		t := &Topic{RunID: runID}
		if err := rows.Scan(&t.ID, &t.Label, &t.Score); err != nil {
			return nil, fmt.Errorf("failed to scan topic: %w", err)
		}
		topics = append(topics, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating topics: %w", err)
	}
	return topics, nil
}

// topicItemSets returns each topic's item IDs, ascending, indexed like topics.
func topicItemSets(ctx context.Context, tx *sql.Tx, topics []*Topic) ([][]int64, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(topics)), ",")
	args := make([]any, len(topics))
	index := make(map[int64]int, len(topics))
	for i, t := range topics {
		args[i] = t.ID
		index[t.ID] = i
	}

	//nolint:gosec // Safe: only formatting placeholder count, not user input
	query := `SELECT topic_id, item_id FROM topic_items WHERE topic_id IN (` +
		placeholders + `) ORDER BY topic_id, item_id`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query topic items: %w", err)
	}
	defer rows.Close()

	clusters := make([][]int64, len(topics))
	for rows.Next() {
		var topicID, itemID int64
		if err := rows.Scan(&topicID, &itemID); err != nil {
			return nil, fmt.Errorf("failed to scan topic item: %w", err)
		}
		i := index[topicID]
		clusters[i] = append(clusters[i], itemID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating topic items: %w", err)
	}
	return clusters, nil
}

// applyMigration14 mirrors applyMigration11: schema in its own transaction,
// then an in-process backfill, then INSERT OR IGNORE for the version. A
// running `serve` and a cron `fetch` can both enter this on the first
// post-upgrade open; both do the work idempotently, so the loser has nothing
// left to do but record the version, and a UNIQUE violation there would fail
// a migration that in fact succeeded.
func (db *DB) applyMigration14() error {
	if err := db.applyMigrationSchemaStage(migrationVersion14, migration14DDL); err != nil {
		return err
	}

	logrus.Info("Assigning threads to existing topic runs")
	progress := db.migrationBackfillProgressWith(TopicLineageProgressLogger())
	if err := db.BackfillTopicLineage(context.Background(), progress); err != nil {
		return fmt.Errorf("failed to backfill topic lineage: %w", err)
	}

	if _, err := db.conn.Exec(
		"INSERT OR IGNORE INTO schema_migrations (version) VALUES (?)", migrationVersion14,
	); err != nil {
		return fmt.Errorf("failed to record migration %d: %w", migrationVersion14, err)
	}
	return nil
}

// TopicLineageProgressLogger reports backfill progress per topic run.
func TopicLineageProgressLogger() func(done, total int64) {
	return func(done, total int64) {
		logrus.Infof("Threaded %d/%d topic runs", done, total)
	}
}
