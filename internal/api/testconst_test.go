package api

import "encoding/json"

// Shared test fixtures, hoisted so goconst stays quiet -- the same pattern
// internal/config's tests use.
const (
	testFeedURL  = "https://example.com/feed.xml"
	testFeedURL2 = "https://other.example.org/rss"
	jsonType     = "application/json"
	testVersion  = "test"
	testFeedName = "Feed"

	kindSeen  = "seen"
	kindTag   = "tag"
	emptyJSON = "[]"

	dtoFeedURL     = "https://example.com/f"
	dtoBody        = "<p>body</p>"
	dtoSelector    = "article a"
	unparseableTS  = "not a timestamp"
	caseNameEmpty  = "empty"
	valueLater     = "later"
	valueUrgentTag = "urgent"

	unknownItemID = "ffffffffffffffff"
	fieldItemIDs  = "item_ids"
	pathStatus    = "/api/v1/status"
	pathItems     = "/api/v1/items"
	fieldKindKey  = "kind"

	// searchTerm appears in every item seedSearchable inserts, and
	// searchCorpusSize is how many of them there are.
	searchTerm       = "kubernetes"
	searchCorpusSize = 25
)

// jsonBody marshals a request body for tests that build one dynamically.
func jsonBody(payload map[string]any) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
