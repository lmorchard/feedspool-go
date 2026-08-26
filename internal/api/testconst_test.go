package api

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
)
