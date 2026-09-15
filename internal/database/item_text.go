package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lmorchard/feedspool-go/internal/itemtext"
	"github.com/sirupsen/logrus"
)

// itemTextStalenessCondition selects items whose derived text is missing or was
// produced by a different generator or an older version of it. A changed item
// is handled on the live write path, which is the only thing that changes item
// text, so the backfill does not need to re-hash every row on every run.
//
// upsertItemTextIfChanged is the write-path half of this definition and
// compares the same generator and generator_version (plus the source hash the
// predicate deliberately skips). Change one and change the other.
const itemTextStalenessCondition = `t.item_id IS NULL OR t.generator <> ? OR t.generator_version <> ?`

// itemTextBackfill derives HTML-free search text for items that lack it.
// rederiveAll widens that to every item, which is what a forced rebuild needs.
type itemTextBackfill struct {
	opts        itemtext.Options
	rederiveAll bool
}

// newItemTextBackfill returns the generator that maintains item_text.
func newItemTextBackfill(opts itemtext.Options) *itemTextBackfill {
	return &itemTextBackfill{opts: opts}
}

// newItemTextRebuild returns a generator that treats every item as needing
// work, which is what "reindex --force" runs. It re-derives rows that are
// already at the current generator version -- the case the staleness predicate
// deliberately skips, and the only way to recover text a rolled-back binary or
// a changed tokenizer left wrong.
func newItemTextRebuild(opts itemtext.Options) *itemTextBackfill {
	return &itemTextBackfill{opts: opts, rederiveAll: true}
}

// workCondition returns the WHERE fragment selecting items that still need
// work, with the arguments it binds. A rebuild's predicate is a constant: every
// item needs work by definition, so there is nothing to compare against.
func (g *itemTextBackfill) workCondition() (string, []any) {
	if g.rederiveAll {
		return "TRUE", nil
	}
	return itemTextStalenessCondition, []any{g.Name(), g.Version()}
}

func (g *itemTextBackfill) Name() string { return itemtext.Generator }
func (g *itemTextBackfill) Version() int { return itemtext.Version }

// NextBatch returns the next item IDs needing work, in ascending order.
func (g *itemTextBackfill) NextBatch(tx *sql.Tx, afterID int64, limit int) ([]int64, error) {
	condition, conditionArgs := g.workCondition()
	args := append([]any{afterID}, conditionArgs...)
	args = append(args, limit)
	rows, err := tx.Query(
		`
		SELECT i.id
		FROM items i LEFT JOIN item_text t ON t.item_id = i.id
		WHERE i.id > ? AND (`+condition+`)
		ORDER BY i.id
		LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query stale item text: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan stale item id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate stale item ids: %w", err)
	}
	return ids, nil
}

// Remaining counts the items this generator still has work for.
func (g *itemTextBackfill) Remaining(tx *sql.Tx) (int64, error) {
	condition, args := g.workCondition()
	var remaining int64
	if err := tx.QueryRow(
		`
		SELECT COUNT(*)
		FROM items i LEFT JOIN item_text t ON t.item_id = i.id
		WHERE `+condition,
		args...,
	).Scan(&remaining); err != nil {
		return 0, fmt.Errorf("failed to count stale item text: %w", err)
	}
	return remaining, nil
}

// Recompute derives and stores the text for the given item IDs.
func (g *itemTextBackfill) Recompute(tx *sql.Tx, ids []int64) error {
	sources, err := readItemTextSources(tx, ids)
	if err != nil {
		return err
	}
	for _, source := range sources {
		text := itemtext.Derive(source.title, source.summary, source.content, g.opts)
		hash := itemtext.SourceHash(source.title, source.summary, source.content)
		if err := upsertItemTextTx(tx, source.id, text, hash); err != nil {
			return err
		}
	}
	return nil
}

// itemTextSource is the raw item text a derivation reads from.
type itemTextSource struct {
	id                      int64
	title, summary, content string
}

// readItemTextSources reads every source row before any writing begins: the
// pool is capped at one connection, so a write issued while a result set is
// still open would contend with it.
func readItemTextSources(tx *sql.Tx, ids []int64) ([]itemTextSource, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	// COALESCE is here for the hand-written legacy fixtures in
	// migrations_test.go, which declare these columns nullable. No schema
	// feedspool has ever shipped does -- the version-1 baseline already had them
	// NOT NULL DEFAULT '' -- so this is not guarding real databases. It stays
	// because treating a NULL title as empty text is a better failure mode than
	// aborting a migration and leaving a whole spool unindexed.
	//nolint:gosec // Safe: only formatting placeholder count, not user input
	query := fmt.Sprintf(
		`
		SELECT id, COALESCE(title, ''), COALESCE(summary, ''), COALESCE(content, '')
		FROM items WHERE id IN (%s)`,
		strings.Join(placeholders, ","),
	)
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to read item text sources: %w", err)
	}
	defer rows.Close()

	sources := make([]itemTextSource, 0, len(ids))
	for rows.Next() {
		var source itemTextSource
		if err := rows.Scan(&source.id, &source.title, &source.summary, &source.content); err != nil {
			return nil, fmt.Errorf("failed to scan item text source: %w", err)
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate item text sources: %w", err)
	}
	return sources, nil
}

// upsertItemTextTx is the only place item_text rows are written. The backfill
// runner and UpsertItem both go through it.
func upsertItemTextTx(tx *sql.Tx, itemID int64, text itemtext.Text, sourceHash string) error {
	_, err := tx.Exec(
		`
		INSERT INTO item_text
			(item_id, title, summary, body, source_hash, generator, generator_version, computed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(item_id) DO UPDATE SET
			title = excluded.title, summary = excluded.summary, body = excluded.body,
			source_hash = excluded.source_hash, generator = excluded.generator,
			generator_version = excluded.generator_version, computed_at = excluded.computed_at`,
		itemID, text.Title, text.Summary, text.Body, sourceHash,
		itemtext.Generator, itemtext.Version, formatDatabaseTime(time.Now().UTC()),
	)
	if err != nil {
		return fmt.Errorf("failed to upsert item text for item %d: %w", itemID, err)
	}
	return nil
}

// ReindexItemText brings the derived text and search index up to date. force
// re-derives every item rather than only the stale ones, so a changed tokenizer
// or a rolled-back binary can be recovered from without a schema migration.
//
// force overwrites rows in place, in the same committed batches as any other
// backfill, and never deletes. That is what keeps search answering throughout:
// an item's entry is replaced within one transaction, so the index goes from
// complete-and-stale to complete-and-fresh with no window in between. Interrupt
// it and the rows already rebuilt stay rebuilt; the run simply starts over.
func (db *DB) ReindexItemText(force bool, progress func(done, total int64)) error {
	return db.reindexItemText(force, defaultBackfillBatchSize, progress)
}

// reindexItemText is ReindexItemText with the batch size exposed, which is what
// lets a test observe the state of the index between committed batches.
func (db *DB) reindexItemText(force bool, batchSize int, progress func(done, total int64)) error {
	generator := newItemTextBackfill(itemtext.DefaultOptions())
	if force {
		generator = newItemTextRebuild(itemtext.DefaultOptions())
	}
	return db.RunBackfill(generator, batchSize, progress)
}

// ItemTextProgressLogger reports backfill progress at info level, which is what
// makes a long migration on a large spool visibly alive rather than hung.
func ItemTextProgressLogger() func(done, total int64) {
	return func(done, total int64) {
		logrus.Infof("Indexed %d/%d outstanding items", done, total)
	}
}
