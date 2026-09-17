package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lmorchard/feedspool-go/internal/embed"
)

// ErrNoEmbedding reports that an item has no vector for the requested model.
//
// Distinguishable from a real failure on purpose: "not embedded yet" is the
// normal state of an item outside the window a user has run `embed` over, and
// a command should be able to say "run feedspool embed" rather than reporting
// an error.
var ErrNoEmbedding = errors.New("item has no embedding for this model")

// IsNoEmbedding reports whether err means the embedding is simply absent.
func IsNoEmbedding(err error) bool {
	return errors.Is(err, ErrNoEmbedding)
}

// ItemEmbedding is one stored vector and its bookkeeping.
type ItemEmbedding struct {
	ItemID     int64
	ModelID    string
	Dims       int
	Vector     []float32
	SourceHash string
	// GeneratorVersion is embed.Version at the time the vector was written.
	// Stored per row so a code change can mark existing rows stale without a
	// schema migration.
	GeneratorVersion int
	ComputedAt       time.Time
}

// upsertItemEmbeddingTx is the only place item_embeddings rows are written.
//
// dims is derived from the vector rather than passed in, so the column and the
// blob cannot disagree at the point of writing. Mirrors upsertItemTextTx.
func upsertItemEmbeddingTx(
	tx *sql.Tx, itemID int64, modelID string, vector []float32, sourceHash string,
) error {
	if len(vector) == 0 {
		return fmt.Errorf("refusing to store an empty embedding for item %d", itemID)
	}

	_, err := tx.Exec(
		`
		INSERT INTO item_embeddings
			(item_id, model_id, dims, vector, source_hash, generator_version, computed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(item_id, model_id) DO UPDATE SET
			dims = excluded.dims, vector = excluded.vector,
			source_hash = excluded.source_hash,
			generator_version = excluded.generator_version,
			computed_at = excluded.computed_at`,
		itemID, modelID, len(vector), EncodeVector(vector), sourceHash,
		embed.Version, formatDatabaseTime(time.Now().UTC()),
	)
	if err != nil {
		return fmt.Errorf("failed to upsert embedding for item %d model %q: %w",
			itemID, modelID, err)
	}
	return nil
}

// GetItemEmbedding reads one item's vector for a model.
func (db *DB) GetItemEmbedding(itemID int64, modelID string) (*ItemEmbedding, error) {
	row := db.conn.QueryRow(
		`
		SELECT item_id, model_id, dims, vector, source_hash, generator_version, computed_at
		FROM item_embeddings
		WHERE item_id = ? AND model_id = ?`,
		itemID, modelID,
	)
	return scanItemEmbedding(row.Scan)
}

// GetEmbeddingsForWindow retrieves all item embeddings for a specific model within a time window.
func (db *DB) GetEmbeddingsForWindow(
	ctx context.Context, modelID string, since, until time.Time,
) ([]*ItemEmbedding, error) {
	query := `
		SELECT e.item_id, e.model_id, e.dims, e.vector, e.source_hash, e.generator_version, e.computed_at
		FROM item_embeddings e
		JOIN items i ON e.item_id = i.id
		WHERE e.model_id = ?
		` + effectiveDateSinceClause + effectiveDateUntilClause + `
		ORDER BY e.item_id ASC
	`
	rows, err := db.conn.QueryContext(
		ctx, query,
		modelID,
		formatDatabaseTime(since),
		formatDatabaseTime(until),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query embeddings for window: %w", err)
	}
	defer rows.Close()

	var embeddings []*ItemEmbedding
	for rows.Next() {
		emb, err := scanItemEmbedding(rows.Scan)
		if err != nil {
			return nil, err
		}
		embeddings = append(embeddings, emb)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return embeddings, nil
}

// It takes the Scan method itself so *sql.Row and *sql.Rows can both use it;
// they share no interface in database/sql.
func scanItemEmbedding(scan func(...any) error) (*ItemEmbedding, error) {
	var (
		embedding ItemEmbedding
		blob      []byte
	)
	err := scan(
		&embedding.ItemID, &embedding.ModelID, &embedding.Dims, &blob,
		&embedding.SourceHash, &embedding.GeneratorVersion,
		scanNullableTime(&embedding.ComputedAt),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEmbedding
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan embedding: %w", err)
	}

	vector, err := DecodeVector(blob, embedding.Dims)
	if err != nil {
		return nil, fmt.Errorf("item %d model %q: %w", embedding.ItemID, embedding.ModelID, err)
	}
	embedding.Vector = vector
	return &embedding, nil
}
