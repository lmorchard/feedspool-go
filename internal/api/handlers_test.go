package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/ids"
)

type testHarness struct {
	server *httptest.Server
	db     *database.DB
}

func newTestHarness(t *testing.T, token string) *testHarness {
	t.Helper()

	db, err := database.New(filepath.Join(t.TempDir(), "api_test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.InitSchema(); err != nil {
		t.Fatal(err)
	}

	apiServer := NewServer(Config{DB: db, Token: token, Version: testVersion})
	server := httptest.NewServer(apiServer.Handler())
	t.Cleanup(server.Close)

	return &testHarness{server: server, db: db}
}

func (h *testHarness) seed(t *testing.T) {
	t.Helper()
	for _, feedURL := range []string{testFeedURL, testFeedURL2} {
		if err := h.db.UpsertFeed(&database.Feed{
			URL: feedURL, Title: "Feed " + feedURL, Type: database.FeedTypeRSS,
		}); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 5 {
		if err := h.db.UpsertItem(&database.Item{
			FeedURL:       testFeedURL,
			GUID:          fmt.Sprintf("guid-%d", i),
			Title:         fmt.Sprintf("Article %d", i),
			Link:          fmt.Sprintf("https://example.com/%d", i),
			Content:       fmt.Sprintf("<p>body %d</p>", i),
			Summary:       fmt.Sprintf("summary %d", i),
			PublishedDate: base.Add(time.Duration(i) * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func (h *testHarness) get(t *testing.T, path string) (status int, payload []byte) {
	t.Helper()
	return h.do(t, http.MethodGet, path, "", "")
}

func (h *testHarness) do(t *testing.T, method, path, contentType, body string) (status int, payload []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := h.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err = io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, payload
}

func decodeMap(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("response is not a JSON object: %v (%s)", err, payload)
	}
	return decoded
}

func decodeCollection(t *testing.T, payload []byte) (data []any, nextCursor string, limit int) {
	t.Helper()
	decoded := decodeMap(t, payload)
	items, ok := decoded["data"].([]any)
	if !ok {
		t.Fatalf("data is not an array: %s", payload)
	}
	if raw, present := decoded["next_cursor"]; present && raw != nil {
		nextCursor, _ = raw.(string)
	}
	if raw, ok := decoded["limit"].(float64); ok {
		limit = int(raw)
	}
	return items, nextCursor, limit
}

func assertErrorCode(t *testing.T, payload []byte, want string) {
	t.Helper()
	decoded := decodeMap(t, payload)
	envelope, ok := decoded["error"].(map[string]any)
	if !ok {
		t.Fatalf("response has no error envelope: %s", payload)
	}
	if envelope["code"] != want {
		t.Errorf("error code = %v, want %q (%s)", envelope["code"], want, payload)
	}
}

func TestServiceRoot(t *testing.T) {
	h := newTestHarness(t, "")

	status, payload := h.get(t, "/api/v1/")
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}
	decoded := decodeMap(t, payload)
	if decoded["api_version"] != "v1" {
		t.Errorf("api_version = %v, want v1", decoded["api_version"])
	}
	if decoded["feedspool_version"] != testVersion {
		t.Errorf("feedspool_version = %v", decoded["feedspool_version"])
	}
}

func TestStatusEndpoint(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	status, payload := h.get(t, "/api/v1/status")
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}
	decoded := decodeMap(t, payload)
	if decoded["feed_count"] != float64(2) {
		t.Errorf("feed_count = %v, want 2", decoded["feed_count"])
	}
	if decoded["item_count"] != float64(5) {
		t.Errorf("item_count = %v, want 5", decoded["item_count"])
	}
}

func TestListFeeds(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	status, payload := h.get(t, "/api/v1/feeds")
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}
	data, next, limit := decodeCollection(t, payload)
	if len(data) != 2 {
		t.Errorf("feeds = %d, want 2", len(data))
	}
	if next != "" {
		t.Errorf("next_cursor = %q, want null on a complete page", next)
	}
	if limit != defaultFeedLimit {
		t.Errorf("limit = %d, want %d", limit, defaultFeedLimit)
	}
}

func TestGetFeedByID(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	status, payload := h.get(t, "/api/v1/feeds/"+ids.FeedID(testFeedURL))
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}
	decoded := decodeMap(t, payload)
	if decoded["url"] != testFeedURL {
		t.Errorf("url = %v, want %q", decoded["url"], testFeedURL)
	}
	// Single resources are bare objects, not wrapped in the collection envelope.
	if _, wrapped := decoded["data"]; wrapped {
		t.Error("single resource is wrapped in a collection envelope")
	}
}

func TestGetFeedUnknownIsNotFound(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	status, payload := h.get(t, "/api/v1/feeds/ffffffff")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", status, payload)
	}
	assertErrorCode(t, payload, codeNotFound)
}

func TestGetFeedCounts(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)
	if err := h.db.AddAnnotation(testFeedURL, "guid-0", kindSeen, nullStr(), nullStr()); err != nil {
		t.Fatal(err)
	}

	status, payload := h.get(t, "/api/v1/feeds/"+ids.FeedID(testFeedURL)+"?include=counts")
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}
	decoded := decodeMap(t, payload)
	if decoded["item_count"] != float64(5) {
		t.Errorf("item_count = %v, want 5", decoded["item_count"])
	}
	if decoded["unseen_count"] != float64(4) {
		t.Errorf("unseen_count = %v, want 4", decoded["unseen_count"])
	}
}

func TestListItemsPaginatesAcrossRequests(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	seen := map[string]bool{}
	path := "/api/v1/items?limit=2"
	for requests := 0; ; requests++ {
		if requests > 10 {
			t.Fatal("pagination did not terminate")
		}
		status, payload := h.get(t, path)
		if status != http.StatusOK {
			t.Fatalf("status = %d: %s", status, payload)
		}
		data, next, _ := decodeCollection(t, payload)
		for _, entry := range data {
			id := entry.(map[string]any)["id"].(string)
			if seen[id] {
				t.Fatalf("item %s returned on two pages", id)
			}
			seen[id] = true
		}
		if next == "" {
			break
		}
		path = "/api/v1/items?limit=2&cursor=" + next
	}
	if len(seen) != 5 {
		t.Errorf("items paged = %d, want 5", len(seen))
	}
}

func TestListItemsRejectsUnknownParameter(t *testing.T) {
	h := newTestHarness(t, "")

	status, payload := h.get(t, "/api/v1/items?limitt=2")
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", status, payload)
	}
	assertErrorCode(t, payload, codeInvalidParameter)
}

func TestListItemsRejectsBadCursor(t *testing.T) {
	h := newTestHarness(t, "")

	status, payload := h.get(t, "/api/v1/items?cursor=notacursor")
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", status, payload)
	}
	assertErrorCode(t, payload, codeInvalidCursor)
}

func TestListItemsRejectsBadEnumValues(t *testing.T) {
	h := newTestHarness(t, "")

	for _, query := range []string{"seen=maybe", "archived=perhaps", "sort=sideways", "since=yesterday"} {
		t.Run(query, func(t *testing.T) {
			status, payload := h.get(t, "/api/v1/items?"+query)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", status, payload)
			}
			assertErrorCode(t, payload, codeInvalidParameter)
		})
	}
}

func TestListItemsRejectsConflictingFeedFilters(t *testing.T) {
	h := newTestHarness(t, "")

	status, payload := h.get(t, "/api/v1/items?feed_id=abcd1234&feed_url=https://x.example/f")
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", status, payload)
	}
	assertErrorCode(t, payload, codeInvalidParameter)
}

// searchPath builds an item search URL for searchTerm, with extra appended.
func searchPath(extra string) string {
	return pathItems + "?q=" + searchTerm + extra
}

// relevanceSuffix is the query fragment that asks for relevance ordering.
func relevanceSuffix() string {
	return "&" + paramSort + "=" + database.SortRelevance
}

// seedSearchable inserts count items that all mention searchTerm -- in the
// title for every other one, in the body otherwise -- so relevance has
// something to rank and every item is a hit.
func (h *testHarness) seedSearchable(t *testing.T, count int) {
	t.Helper()
	if err := h.db.UpsertFeed(&database.Feed{
		URL: testFeedURL, Title: testFeedName, Type: database.FeedTypeRSS,
	}); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range count {
		title := fmt.Sprintf("Dispatch %02d", i)
		if i%2 == 0 {
			title = fmt.Sprintf("%s digest %02d", searchTerm, i)
		}
		if err := h.db.UpsertItem(&database.Item{
			FeedURL:       testFeedURL,
			GUID:          fmt.Sprintf("search-%02d", i),
			Title:         title,
			Link:          fmt.Sprintf("https://example.com/s/%02d", i),
			Summary:       "<p>Assorted notes on other subjects entirely.</p>",
			Content:       "<div>" + strings.Repeat(searchTerm+" ", 1+i%3) + "notes</div>",
			PublishedDate: base.Add(time.Duration(i) * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// itemIDsFrom pulls the item ids out of a decoded collection, in order.
func itemIDsFrom(t *testing.T, data []any) []string {
	t.Helper()
	out := make([]string, 0, len(data))
	for _, entry := range data {
		item, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("collection entry is not an object: %v", entry)
		}
		id, ok := item[fieldID].(string)
		if !ok {
			t.Fatalf("item carries no id: %v", item)
		}
		out = append(out, id)
	}
	return out
}

// getCollection issues a request the test expects to succeed and decodes it.
func (h *testHarness) getCollection(t *testing.T, path string) (data []any, nextCursor string) {
	t.Helper()
	status, payload := h.get(t, path)
	if status != http.StatusOK {
		t.Fatalf("GET %s status = %d: %s", path, status, payload)
	}
	data, nextCursor, _ = decodeCollection(t, payload)
	return data, nextCursor
}

// Replaces TestListItemsSearchMatchesTitleOnly. Rewritten rather than deleted:
// the old test pinned the title-substring contract, and this one is the record
// that the contract changed deliberately.
func TestListItemsSearchMatchesBodyAndSummary(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	// Each query pairs a term from one field with the digit that appears only
	// in item 3, so only that item matches all of them.
	for _, query := range []string{"Article+3", "body+3", "summary+3"} {
		t.Run(query, func(t *testing.T) {
			data, _ := h.getCollection(t, pathItems+"?q="+query)
			if len(data) != 1 {
				t.Fatalf("q=%s results = %d, want 1", query, len(data))
			}
			item, ok := data[0].(map[string]any)
			if !ok {
				t.Fatalf("collection entry is not an object: %v", data[0])
			}
			if item[fieldTitle] != "Article 3" {
				t.Errorf("q=%s matched %v, want Article 3", query, item[fieldTitle])
			}
		})
	}
}

// Relevance paginates by an offset carried inside the opaque cursor. Absent
// writes, that has to keep the same totality property the keyset cursor has:
// every match exactly once, in the order a single unpaged request gives.
func TestListItemsRelevancePaginatesWithoutGapsOrRepeats(t *testing.T) {
	h := newTestHarness(t, "")
	h.seedSearchable(t, searchCorpusSize)

	unpaged, next := h.getCollection(t, searchPath(relevanceSuffix()+"&limit=100"))
	if next != "" {
		t.Fatalf("unpaged request returned a cursor with only %d matches", searchCorpusSize)
	}
	want := itemIDsFrom(t, unpaged)
	if len(want) != searchCorpusSize {
		t.Fatalf("unpaged results = %d, want %d", len(want), searchCorpusSize)
	}

	var got []string
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("relevance pagination did not terminate")
		}
		path := searchPath(relevanceSuffix() + "&limit=10")
		if cursor != "" {
			path += "&" + paramCursor + "=" + url.QueryEscape(cursor)
		}
		data, nextCursor := h.getCollection(t, path)
		got = append(got, itemIDsFrom(t, data)...)
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	if !slices.Equal(got, want) {
		t.Errorf("paged relevance = %v, want the unpaged order %v", got, want)
	}
	distinct := map[string]bool{}
	for _, id := range got {
		if distinct[id] {
			t.Fatalf("item %s was returned twice across a page boundary", id)
		}
		distinct[id] = true
	}
	if len(distinct) != searchCorpusSize {
		t.Errorf("distinct ids paged = %d, want %d", len(distinct), searchCorpusSize)
	}
}

// A cursor from one ordering means nothing in another: a relevance cursor
// carries an offset, a date cursor carries a keyset position. Replaying the
// wrong one has to be an error rather than a silently wrong page.
func TestListItemsRejectsCursorFromAnotherSort(t *testing.T) {
	h := newTestHarness(t, "")
	h.seedSearchable(t, searchCorpusSize)

	_, relevanceCursor := h.getCollection(t, searchPath(relevanceSuffix()+"&limit=5"))
	_, dateCursor := h.getCollection(t, searchPath("&limit=5"))
	if relevanceCursor == "" || dateCursor == "" {
		t.Fatalf("cursors = (%q, %q), want both non-empty", relevanceCursor, dateCursor)
	}

	cases := []struct{ name, path string }{
		{
			"relevance cursor replayed as newest",
			searchPath("&limit=5&" + paramCursor + "=" + url.QueryEscape(relevanceCursor)),
		},
		{
			"date cursor replayed as relevance",
			searchPath(relevanceSuffix() + "&limit=5&" + paramCursor + "=" + url.QueryEscape(dateCursor)),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, payload := h.get(t, tc.path)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", status, payload)
			}
			assertErrorCode(t, payload, codeInvalidCursor)
		})
	}
}

func TestListItemsRelevanceRequiresQuery(t *testing.T) {
	h := newTestHarness(t, "")
	h.seedSearchable(t, searchCorpusSize)

	status, payload := h.get(t, pathItems+"?"+paramSort+"="+database.SortRelevance)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", status, payload)
	}
	assertErrorCode(t, payload, codeInvalidParameter)

	// A bare "*" is a non-empty q that the parser reduces to no expression, so
	// it slips past the check above and leaves relevance with nothing to rank.
	// That is still the caller's mistake, not a 500.
	status, payload = h.get(t, pathItems+"?q=*&"+paramSort+"="+database.SortRelevance)
	if status != http.StatusBadRequest {
		t.Fatalf("q=* status = %d, want 400: %s", status, payload)
	}
	assertErrorCode(t, payload, codeInvalidParameter)

	// The same sort with a query is accepted, so the rejections above are about
	// the missing query rather than about relevance not being a known ordering.
	if data, _ := h.getCollection(t, searchPath(relevanceSuffix())); len(data) == 0 {
		t.Error("sort=relevance with a query returned nothing")
	}
}

// A query with nothing to match is the user's mistake, not the server's.
func TestListItemsOnlyExclusionsIsBadRequest(t *testing.T) {
	h := newTestHarness(t, "")
	h.seedSearchable(t, searchCorpusSize)

	status, payload := h.get(t, pathItems+"?q=-draft")
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", status, payload)
	}
	assertErrorCode(t, payload, codeInvalidParameter)
}

// A query does not imply relevance. This is an explicit spec decision -- a
// feed reader's search is usually "what is new about X" -- and it keeps the
// default path on the keyset cursor rather than quietly moving every search
// onto offset pagination. Asserting it makes a later change to the default
// deliberate.
func TestListItemsDefaultsToNewestWithAQuery(t *testing.T) {
	h := newTestHarness(t, "")
	h.seedSearchable(t, searchCorpusSize)

	data, next := h.getCollection(t, searchPath("&limit=5"))
	if len(data) != 5 {
		t.Fatalf("results = %d, want 5", len(data))
	}

	previous := ""
	for _, entry := range data {
		item, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("collection entry is not an object: %v", entry)
		}
		published, ok := item["published_date"].(string)
		if !ok {
			t.Fatalf("item carries no published_date: %v", item)
		}
		if previous != "" && published >= previous {
			t.Errorf("published_date %s follows %s; the default ordering is not newest-first",
				published, previous)
		}
		previous = published
	}

	cursor, err := decodeItemCursor(next)
	if err != nil {
		t.Fatalf("decodeItemCursor(%q) error = %v", next, err)
	}
	if cursor.Relevance || cursor.Offset != 0 {
		t.Errorf("cursor = %+v, want a keyset cursor; a query must not imply relevance", *cursor)
	}
	if cursor.ID == 0 {
		t.Errorf("cursor = %+v, want a keyset position", *cursor)
	}
}

func TestListItemsIncludeContent(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	status, payload := h.get(t, "/api/v1/items?limit=1")
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}
	data, _, _ := decodeCollection(t, payload)
	if _, present := data[0].(map[string]any)["content"]; present {
		t.Error("list response carries content without include=content")
	}

	status, payload = h.get(t, "/api/v1/items?limit=1&include=content")
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}
	data, _, _ = decodeCollection(t, payload)
	if _, present := data[0].(map[string]any)["content"]; !present {
		t.Error("include=content did not add the field")
	}
}

func TestListItemsRejectsFeedOnlyInclude(t *testing.T) {
	h := newTestHarness(t, "")

	status, payload := h.get(t, "/api/v1/items?include=counts")
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", status, payload)
	}
	assertErrorCode(t, payload, codeInvalidParameter)
}

func TestGetItemDefaultsToContentAndAnnotations(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	status, payload := h.get(t, "/api/v1/items/"+ids.ItemID(testFeedURL, "guid-1"))
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}
	decoded := decodeMap(t, payload)
	if _, present := decoded["content"]; !present {
		t.Error("item detail omits content by default")
	}
	if _, present := decoded["annotations"]; !present {
		t.Error("item detail omits annotations by default")
	}
	if _, present := decoded["item_json"]; present {
		t.Error("item detail includes raw without being asked")
	}
}

func TestGetItemUnknownIsNotFound(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	status, payload := h.get(t, "/api/v1/items/ffffffffffffffff")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", status, payload)
	}
	assertErrorCode(t, payload, codeNotFound)
}

// Archived items are excluded by default, which differs from `feedspool items`.
func TestListItemsExcludesArchivedByDefault(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)
	if err := h.db.MarkItemsArchived(testFeedURL, []string{"guid-0", "guid-1", "guid-2", "guid-3"}); err != nil {
		t.Fatal(err)
	}

	status, payload := h.get(t, "/api/v1/items")
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}
	data, _, _ := decodeCollection(t, payload)
	if len(data) != 4 {
		t.Errorf("default results = %d, want the 4 unarchived items", len(data))
	}

	status, payload = h.get(t, "/api/v1/items?archived=any")
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}
	data, _, _ = decodeCollection(t, payload)
	if len(data) != 5 {
		t.Errorf("archived=any results = %d, want all 5", len(data))
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := newTestHarness(t, "")

	status, _ := h.do(t, http.MethodPost, "/api/v1/feeds", jsonType, "{}")
	if status != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", status)
	}
}

func TestUnknownEndpointIsJSONNotFound(t *testing.T) {
	h := newTestHarness(t, "")

	status, payload := h.get(t, "/api/v1/nope")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
	assertErrorCode(t, payload, codeNotFound)
}

func TestOpenAPIDocumentIsServed(t *testing.T) {
	h := newTestHarness(t, "")

	status, payload := h.get(t, "/api/v1/openapi.yaml")
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}
	if !strings.HasPrefix(string(payload), "openapi:") {
		t.Errorf("body does not look like an OpenAPI document: %.40s", payload)
	}
}

func nullStr() sql.NullString { return sql.NullString{} }

// discovered_at must be the field since/until compare against, so that
// "poll with since = max(discovered_at)" is actually correct.
func TestDiscoveredAtRoundTripsThroughSince(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	_, payload := h.get(t, "/api/v1/items?limit=1")
	data, _, _ := decodeCollection(t, payload)
	newest := data[0].(map[string]any)
	discoveredAt, ok := newest["discovered_at"].(string)
	if !ok {
		t.Fatalf("discovered_at = %v, want a timestamp", newest["discovered_at"])
	}

	// since is exclusive, so nothing is strictly after the newest item.
	_, payload = h.get(t, "/api/v1/items?since="+url.QueryEscape(discoveredAt))
	data, _, _ = decodeCollection(t, payload)
	if len(data) != 0 {
		t.Errorf("since=max(discovered_at) returned %d items, want 0", len(data))
	}
}

// Go's time.Parse accepts an optional fractional-seconds component under the
// RFC3339 layout, so the RFC3339Nano values the API emits parse back cleanly.
// Pinned as a test because it looks like it should be a mismatch: the emit
// layout and the parse layout are different constants, and it would be an easy
// "fix" to widen the parser or narrow the formatter and break round-tripping.
func TestSinceAcceptsTheTimestampsWeEmit(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	for _, stamp := range []string{
		"2026-08-25T19:56:20Z",
		"2026-08-25T19:56:20.5Z",
		"2026-08-25T19:56:20.530695Z",
		"2026-08-25T19:56:20.530695123Z",
		"2026-08-25T19:56:20.530695+07:00",
	} {
		t.Run(stamp, func(t *testing.T) {
			status, payload := h.get(t, "/api/v1/items?since="+url.QueryEscape(stamp))
			if status != http.StatusOK {
				t.Errorf("since=%s = %d, want 200: %s", stamp, status, payload)
			}
		})
	}
}

// Real first_seen values carry microseconds. Emitting whole seconds broke the
// documented polling loop -- max(discovered_at) came back truncated, the
// exclusive `since` failed to exclude the boundary row, and every poll
// re-delivered the same batch. The unit tests missed it because they seeded
// whole-second timestamps; a run against a real database caught it.
func TestDiscoveredAtKeepsSubSecondPrecisionForPolling(t *testing.T) {
	h := newTestHarness(t, "")
	if err := h.db.UpsertFeed(&database.Feed{URL: testFeedURL, Title: testFeedName}); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 8, 25, 19, 56, 20, 530695000, time.UTC)
	if err := h.db.UpsertItem(&database.Item{
		FeedURL:   testFeedURL,
		GUID:      "precise",
		Title:     "Precise",
		FirstSeen: sql.NullTime{Time: stamp, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}

	_, payload := h.get(t, "/api/v1/items?limit=1")
	data, _, _ := decodeCollection(t, payload)
	discoveredAt := data[0].(map[string]any)["discovered_at"].(string)

	if !strings.Contains(discoveredAt, ".") {
		t.Fatalf("discovered_at = %q, want sub-second precision preserved", discoveredAt)
	}

	// The whole point: feeding it straight back must exclude that row.
	_, payload = h.get(t, "/api/v1/items?since="+url.QueryEscape(discoveredAt))
	data, _, _ = decodeCollection(t, payload)
	if len(data) != 0 {
		t.Errorf("since=max(discovered_at) returned %d items, want 0", len(data))
	}
}

func mustGet(t *testing.T, h *testHarness, path string) []byte {
	t.Helper()
	status, payload := h.get(t, path)
	if status != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", path, status, payload)
	}
	return payload
}
