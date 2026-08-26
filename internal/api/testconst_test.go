package api

import "encoding/json"

// Shared test fixtures, hoisted so goconst stays quiet -- the same pattern
// internal/config's tests use.
const (
	testFeedURL  = "https://example.com/feed.xml"
	testFeedURL2 = "https://other.example.org/rss"
	jsonType     = "application/json"
	testVersion  = "test"

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
	fieldKindKey  = "kind"
)

// jsonBody marshals a request body for tests that build one dynamically.
func jsonBody(payload map[string]any) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
