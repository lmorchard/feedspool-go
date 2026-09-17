package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/lmorchard/feedspool-go/internal/embed"
)

// itemEmbeddingFrom is the shared FROM clause for every embed query.
//
// The JOIN on item_text (not LEFT JOIN) is what skips items with no derived
// text rather than embedding empty strings; CountItemsMissingText reports how
// many that excluded, so the command can point at `feedspool reindex`.
//
// The model_id comparison sits in the LEFT JOIN rather than the WHERE clause on
// purpose: in the WHERE it would discard items that have no row for this model
// at all, which are exactly the ones needing work.
const itemEmbeddingFrom = `
	FROM items i
	JOIN item_text t            ON t.item_id = i.id
	LEFT JOIN item_embeddings e ON e.item_id = i.id AND e.model_id = ?`

// itemEmbeddingWindow bounds the work by effective date, inclusive at both
// ends.
//
// It concatenates aliasedEffectiveDateExpression rather than spelling out an
// equivalent COALESCE, and that is load-bearing: migration 9 indexes this
// exact expression, and SQLite only uses an expression index on a textual
// match. A hand-written equivalent compiles, returns the right rows, and
// silently full-scans.
// TestEmbedQueryUsesTheEffectiveDateIndex asserts the index is really used.
const itemEmbeddingWindow = aliasedEffectiveDateExpression + " IS NOT NULL" +
	" AND " + aliasedEffectiveDateExpression + " >= julianday(?)" +
	" AND " + aliasedEffectiveDateExpression + " <= julianday(?)"

// itemEmbeddingStaleness selects items whose vector is missing, was derived
// from different text, or was produced by an older generator.
//
// Unlike itemTextStalenessCondition this DOES compare the source hash, and the
// difference matters: item_text is kept fresh by a trigger on the live write
// path, but embeddings have no trigger and no live write path -- `embed` is
// their only producer. Without the hash comparison a revised item would keep
// its stale vector forever.
const itemEmbeddingStaleness = `e.item_id IS NULL
	OR e.source_hash <> t.source_hash
	OR e.generator_version <> ?`

// itemEmbeddingBackfill embeds items in a date window that need it.
type itemEmbeddingBackfill struct {
	provider embed.Provider
	modelID  string
	since    time.Time
	until    time.Time
	// force widens the predicate to the whole window, which is what
	// `embed --force` runs. It re-embeds rows already at the current version --
	// the case the staleness predicate deliberately skips.
	force bool
}

func (g *itemEmbeddingBackfill) Name() string { return "itemembedding" }
func (g *itemEmbeddingBackfill) Version() int { return embed.Version }

// workCondition returns the WHERE fragment selecting items that still need
// work, with the arguments it binds.
func (g *itemEmbeddingBackfill) workCondition() (condition string, args []any) {
	// Capacity 3: the two window bounds, plus the generator version when the
	// staleness clause is included below.
	args = make([]any, 0, 3)
	args = append(args, formatDatabaseTime(g.since), formatDatabaseTime(g.until))
	if g.force {
		return itemEmbeddingWindow, args
	}
	return itemEmbeddingWindow + " AND (" + itemEmbeddingStaleness + ")",
		append(args, g.Version())
}

// batchQuery builds the cursor query and its arguments.
//
// Argument order follows the query text: model_id binds in the JOIN, before
// the cursor and the window. Exported to the package's tests so the query plan
// can be asserted against the real index.
func (g *itemEmbeddingBackfill) batchQuery(afterID int64, limit int) (query string, args []any) {
	condition, conditionArgs := g.workCondition()
	args = append([]any{g.modelID, afterID}, conditionArgs...)
	args = append(args, limit)
	// condition is built from package constants, so the concatenation cannot
	// carry anything a caller supplied.
	query = `SELECT i.id` + itemEmbeddingFrom + `
		WHERE i.id > ? AND (` + condition + `)
		ORDER BY i.id
		LIMIT ?`
	return query, args
}

// NextBatch returns the next item IDs needing work, in ascending order.
func (g *itemEmbeddingBackfill) NextBatch(tx *sql.Tx, afterID int64, limit int) ([]int64, error) {
	query, args := g.batchQuery(afterID, limit)
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query items needing embeddings: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan item id needing an embedding: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate items needing embeddings: %w", err)
	}
	return ids, nil
}

// Remaining counts the items this generator still has work for.
func (g *itemEmbeddingBackfill) Remaining(tx *sql.Tx) (int64, error) {
	condition, conditionArgs := g.workCondition()
	args := append([]any{g.modelID}, conditionArgs...)

	var remaining int64
	// condition is assembled from package constants, not user input.
	query := `SELECT COUNT(*)` + itemEmbeddingFrom + ` WHERE ` + condition
	if err := tx.QueryRow(query, args...).Scan(&remaining); err != nil {
		return 0, fmt.Errorf("failed to count items needing embeddings: %w", err)
	}
	return remaining, nil
}

// ReadInputs assembles the embed input for each item from its derived text.
//
// Every row is read before anything is written, for the reason
// readItemTextSources documents: the pool is capped at one connection, so a
// write issued while a result set is still open would contend with it.
func (g *itemEmbeddingBackfill) ReadInputs(tx *sql.Tx, ids []int64) ([]StagedInput, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	//nolint:gosec // Safe: only formatting placeholder count, not user input
	query := fmt.Sprintf(
		`SELECT item_id, title, summary, body, source_hash
		 FROM item_text WHERE item_id IN (%s)`,
		strings.Join(placeholders, ","),
	)
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to read derived text for embedding: %w", err)
	}
	defer rows.Close()

	inputs := make([]StagedInput, 0, len(ids))
	for rows.Next() {
		var (
			id                   int64
			title, summary, body string
			sourceHash           string
		)
		if err := rows.Scan(&id, &title, &summary, &body, &sourceHash); err != nil {
			return nil, fmt.Errorf("failed to scan derived text for embedding: %w", err)
		}
		inputs = append(inputs, StagedInput{
			ItemID:     id,
			Text:       embed.ItemInput(title, summary, body),
			SourceHash: sourceHash,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate derived text for embedding: %w", err)
	}
	return inputs, nil
}

// Compute embeds the batch. No transaction is open here -- see StagedBackfill.
func (g *itemEmbeddingBackfill) Compute(
	ctx context.Context, inputs []StagedInput,
) ([]StagedResult, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	texts := make([]string, len(inputs))
	for i, input := range inputs {
		texts[i] = input.Text
	}

	vectors, err := g.provider.Embed(ctx, texts)
	if err != nil {
		// If batch embedding failed (e.g. HTTP 400 token count limit on an oversized item),
		// fall back to embedding items individually so valid items in the batch still get processed.
		logrus.Warnf("Batch embedding of %d items failed (%v); retrying items individually...", len(inputs), err)
		var results []StagedResult
		for _, input := range inputs {
			vecs, itemErr := g.provider.Embed(ctx, []string{input.Text})
			if itemErr != nil {
				logrus.Warnf("Skipping item %d for model %q due to embedding error: %v",
					input.ItemID, g.modelID, itemErr)
				continue
			}
			if len(vecs) > 0 {
				results = append(results, StagedResult{
					ItemID:     input.ItemID,
					SourceHash: input.SourceHash,
					Vector:     vecs[0],
				})
			}
		}
		if len(results) > 0 {
			return results, nil
		}
		return nil, fmt.Errorf("failed to embed %d items with model %q: %w",
			len(inputs), g.modelID, err)
	}
	if len(vectors) != len(inputs) {
		return nil, fmt.Errorf("embedded %d items but got %d vectors back from model %q",
			len(inputs), len(vectors), g.modelID)
	}

	results := make([]StagedResult, len(inputs))
	for i, input := range inputs {
		results[i] = StagedResult{
			ItemID:     input.ItemID,
			SourceHash: input.SourceHash,
			Vector:     vectors[i],
		}
	}
	return results, nil
}

// Write stores the batch's vectors.
func (g *itemEmbeddingBackfill) Write(tx *sql.Tx, results []StagedResult) error {
	for _, result := range results {
		if err := upsertItemEmbeddingTx(
			tx, result.ItemID, g.modelID, result.Vector, result.SourceHash,
		); err != nil {
			return err
		}
	}
	return nil
}

// EmbedItems fills in embeddings for items in a date window that need them.
//
// force re-embeds every item in the window rather than only the stale ones,
// which is how a changed input template or a rolled-back binary is recovered
// from without a schema migration.
//
// It mirrors ReindexItemText in shape, but rides RunStagedBackfill rather than
// RunBackfill: embedding makes network calls, and RunBackfill would hold the
// single pooled connection across them.
func (db *DB) EmbedItems(
	ctx context.Context, provider embed.Provider, since, until time.Time,
	force bool, batchSize int, progress func(done, total int64),
) error {
	generator := &itemEmbeddingBackfill{
		provider: provider,
		modelID:  provider.ModelID(),
		since:    since,
		until:    until,
		force:    force,
	}
	return db.RunStagedBackfill(ctx, generator, batchSize, progress)
}

// CountItemsToEmbed reports how many items in the window need embedding, which
// is what `embed --dry-run` prints. It makes no network call.
func (db *DB) CountItemsToEmbed(
	modelID string, since, until time.Time, force bool,
) (int64, error) {
	generator := &itemEmbeddingBackfill{
		modelID: modelID,
		since:   since,
		until:   until,
		force:   force,
	}
	return db.stagedRemaining(generator)
}

// CountItemsMissingText reports items in the window with no item_text row.
//
// The embed generator's inner JOIN excludes them, so without this count a user
// would silently get fewer embeddings than items and no explanation. The
// command turns a non-zero count into a pointer at `feedspool reindex`.
func (db *DB) CountItemsMissingText(since, until time.Time) (int64, error) {
	var missing int64
	// The window is a package constant, not user input.
	query := `
		SELECT COUNT(*)
		FROM items i LEFT JOIN item_text t ON t.item_id = i.id
		WHERE t.item_id IS NULL AND ` + itemEmbeddingWindow
	if err := db.conn.QueryRow(
		query, formatDatabaseTime(since), formatDatabaseTime(until),
	).Scan(&missing); err != nil {
		return 0, fmt.Errorf("failed to count items missing derived text: %w", err)
	}
	return missing, nil
}
