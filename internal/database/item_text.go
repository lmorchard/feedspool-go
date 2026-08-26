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
const itemTextStalenessCondition = `t.item_id IS NULL OR t.generator <> ? OR t.generator_version <> ?`

// itemTextBackfill derives HTML-free search text for items that lack it.
type itemTextBackfill struct{ opts itemtext.Options }

// newItemTextBackfill returns the generator that maintains item_text.
func newItemTextBackfill(opts itemtext.Options) *itemTextBackfill {
	return &itemTextBackfill{opts: opts}
}

func (g *itemTextBackfill) Name() string { return itemtext.Generator }
func (g *itemTextBackfill) Version() int { return itemtext.Version }

// NextBatch returns the next stale item IDs in ascending order.
func (g *itemTextBackfill) NextBatch(tx *sql.Tx, afterID int64, limit int) ([]int64, error) {
	rows, err := tx.Query(
		`
		SELECT i.id
		FROM items i LEFT JOIN item_text t ON t.item_id = i.id
		WHERE i.id > ? AND (`+itemTextStalenessCondition+`)
		ORDER BY i.id
		LIMIT ?`,
		afterID, itemtext.Generator, itemtext.Version, limit,
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

// Remaining counts the items whose derived text is still stale.
func (g *itemTextBackfill) Remaining(tx *sql.Tx) (int64, error) {
	var remaining int64
	if err := tx.QueryRow(
		`
		SELECT COUNT(*)
		FROM items i LEFT JOIN item_text t ON t.item_id = i.id
		WHERE `+itemTextStalenessCondition,
		itemtext.Generator, itemtext.Version,
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

	// COALESCE guards databases old enough that these columns are nullable. The
	// current schema declares them NOT NULL DEFAULT '', but a migration that
	// aborts on one legacy NULL title would leave the whole spool unindexed.
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
// discards every derived row first, which the triggers turn into a full index
// clear, so a tokenizer change can be applied without a schema migration.
func (db *DB) ReindexItemText(force bool, progress func(done, total int64)) error {
	if force {
		if _, err := db.conn.Exec(`DELETE FROM item_text`); err != nil {
			return fmt.Errorf("failed to discard derived item text: %w", err)
		}
	}
	return db.RunBackfill(newItemTextBackfill(itemtext.DefaultOptions()), defaultBackfillBatchSize, progress)
}

// ItemTextProgressLogger reports backfill progress at info level, which is what
// makes a long migration on a large spool visibly alive rather than hung.
func ItemTextProgressLogger() func(done, total int64) {
	return func(done, total int64) {
		logrus.Infof("Indexed %d/%d items", done, total)
	}
}
