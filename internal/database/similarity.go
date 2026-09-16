package database

import (
	"fmt"
	"slices"
	"strings"

	"github.com/sirupsen/logrus"
)

// Neighbor is one item ranked against a subject by embedding similarity.
type Neighbor struct {
	Item       *Item   `json:"item"`
	Similarity float64 `json:"similarity"`
}

// candidate is one row of the similarity scan, held before item rows are read.
type candidate struct {
	itemID     int64
	similarity float64
}

// NearestItems ranks every item carrying a vector for modelID against the
// subject item, most similar first.
//
// Similarity is a plain dot product, which for L2-normalized vectors IS cosine
// similarity -- internal/embed verifies at the provider that the model returns
// unit-length vectors, so the division cosine would otherwise need is always
// by one.
//
// The full scan is deliberate. A 1-3 day window is on the order of a thousand
// vectors, so this is a few million multiply-adds: microseconds. An ANN index
// would not start paying for itself until millions of vectors, and the C
// extensions that provide one (sqlite-vec, sqlite-vss) cannot load into
// modernc.org/sqlite under CGO_ENABLED=0 anyway.
//
// Returns an error satisfying IsNoEmbedding when the subject itself has no
// vector for the model, so a caller can say "run feedspool embed" rather than
// reporting a failure.
func (db *DB) NearestItems(modelID string, itemID int64, limit int) ([]Neighbor, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("neighbor limit is %d, want a positive count", limit)
	}

	subject, err := db.GetItemEmbedding(itemID, modelID)
	if err != nil {
		return nil, err
	}

	candidates, err := db.scanSimilarities(modelID, subject)
	if err != nil {
		return nil, err
	}

	// Descending by similarity, with the item id as a tiebreak so equally
	// similar neighbors come back in a stable order rather than whatever the
	// scan happened to produce.
	slices.SortFunc(candidates, func(a, b candidate) int {
		if a.similarity != b.similarity {
			if a.similarity > b.similarity {
				return -1
			}
			return 1
		}
		return int(a.itemID - b.itemID)
	})

	// Truncate after ranking, never during the scan: limiting the scan would
	// return an arbitrary subset that merely looks plausible.
	candidates = candidates[:min(limit, len(candidates))]

	return db.resolveNeighbors(candidates)
}

// scanSimilarities reads every candidate vector for the model and scores it.
//
// All rows are consumed before anything else touches the database, for the
// reason readItemTextSources documents: the pool is capped at one connection,
// so a query issued while this result set is open would contend with it.
func (db *DB) scanSimilarities(modelID string, subject *ItemEmbedding) ([]candidate, error) {
	rows, err := db.conn.Query(
		`SELECT item_id, dims, vector FROM item_embeddings
		 WHERE model_id = ? AND item_id <> ?`,
		modelID, subject.ItemID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan embeddings for model %q: %w", modelID, err)
	}
	defer rows.Close()

	var candidates []candidate
	for rows.Next() {
		var (
			id   int64
			dims int
			blob []byte
		)
		if err := rows.Scan(&id, &dims, &blob); err != nil {
			return nil, fmt.Errorf("failed to scan a candidate embedding: %w", err)
		}

		vector, err := DecodeVector(blob, dims)
		if err != nil {
			// One corrupt row must not break `related` for everything else.
			logrus.Warnf("Skipping item %d: %v", id, err)
			continue
		}
		similarity, err := DotProduct(subject.Vector, vector)
		if err != nil {
			// Only reachable by corruption: the model filter already
			// guarantees a single vector space.
			logrus.Warnf("Skipping item %d: %v", id, err)
			continue
		}

		candidates = append(candidates, candidate{itemID: id, similarity: float64(similarity)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate candidate embeddings: %w", err)
	}
	return candidates, nil
}

// resolveNeighbors reads the item rows for the ranked candidates in one query,
// then reassembles them in ranked order.
func (db *DB) resolveNeighbors(candidates []candidate) ([]Neighbor, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(candidates))
	args := make([]any, len(candidates))
	for i, c := range candidates {
		placeholders[i] = "?"
		args[i] = c.itemID
	}

	// Only the placeholder count is formatted in, never user input.
	query := fmt.Sprintf(
		`SELECT id, feed_url, guid, title, link, published_date, first_seen,
			content, summary, archived, item_json
		 FROM items WHERE id IN (%s)`,
		strings.Join(placeholders, ","),
	)
	items, err := db.queryItems(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to read neighbor items: %w", err)
	}

	byID := make(map[int64]*Item, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}

	// Rebuilt from the ranked candidates, not from the query result: SQLite is
	// free to return an IN (...) set in any order.
	neighbors := make([]Neighbor, 0, len(candidates))
	for _, c := range candidates {
		item, ok := byID[c.itemID]
		if !ok {
			// The item was deleted between the two queries. ON DELETE CASCADE
			// removes its embedding too, so this only loses a stale candidate.
			continue
		}
		neighbors = append(neighbors, Neighbor{Item: item, Similarity: c.similarity})
	}
	return neighbors, nil
}
