package database

import (
	"errors"
	"fmt"

	"github.com/lmorchard/feedspool-go/internal/search"
)

// These three constants are the single source of truth for what a search
// matches and how it ranks. buildItemsQuery (the CLI) builds from them today
// and itemPageConditions (the API) will too, at which point a structural test
// asserts the two surfaces cannot drift. The predecessor to this file was a
// duplicated instr() expression in two places held in step only by a comment.
//
// The table is joined unaliased on purpose. MATCH and bm25() do not take a
// table so much as the FTS table's hidden column, which is named after the
// table itself -- so an alias f leaves "f MATCH ?" and "bm25(f, ...)" failing
// with "no such column: f". Only "items_fts" or the wordier "f.items_fts"
// resolve, and the unaliased spelling keeps all three fragments consistent.
const (
	itemsFTSJoin  = " JOIN items_fts ON items_fts.rowid = i.id"
	itemsFTSMatch = "items_fts MATCH ?"
	// Title outranks summary outranks body. Without column weights, a body
	// mention buries an exact title match.
	itemsFTSRank = "bm25(items_fts, 10.0, 4.0, 1.0)"
)

// SortNewest and friends name the orderings both surfaces accept.
const (
	SortNewest    = "newest"
	SortOldest    = "oldest"
	SortRelevance = "relevance"
)

// errRelevanceNeedsSearch guards the one combination the fragments above
// cannot express: bm25 reads the joined FTS table, and without a search there
// is no join for it to read.
var errRelevanceNeedsSearch = errors.New("sort by relevance requires a search query")

// itemsSearchExpression translates a raw search box into the FTS5 MATCH
// expression the query binds. An empty result with a nil error means the query
// carries no search filter, so neither the join nor the MATCH is added. A parse
// error propagates to the caller rather than degrading into "match everything".
func itemsSearchExpression(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	expr, err := search.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("failed to parse search query: %w", err)
	}
	return expr, nil
}

// itemsOrderByClause returns the ORDER BY for an item query. bm25 scores are
// negative-better, so relevance ascends; the effective-date and id tiebreaks
// make the ordering total for rows that score alike.
func itemsOrderByClause(sortOrder string) string {
	if sortOrder == SortRelevance {
		return " ORDER BY " + itemsFTSRank + " ASC, " + aliasedEffectiveDateExpression + " DESC, i.id DESC"
	}
	return " ORDER BY " + aliasedEffectiveDateExpression + " DESC"
}
