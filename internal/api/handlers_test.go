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

func TestListItemsSearchMatchesTitleOnly(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	status, payload := h.get(t, "/api/v1/items?q=Article+3")
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}
	data, _, _ := decodeCollection(t, payload)
	if len(data) != 1 {
		t.Fatalf("results = %d, want 1", len(data))
	}

	// "body 3" appears in the content but not the title, so q must not match.
	status, payload = h.get(t, "/api/v1/items?q=body+3")
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}
	data, _, _ = decodeCollection(t, payload)
	if len(data) != 0 {
		t.Errorf("q matched content; results = %d, want 0", len(data))
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

	// Everything strictly after the newest item's discovery time: nothing.
	_, payload = h.get(t, "/api/v1/items?since="+discoveredAt)
	data, _, _ = decodeCollection(t, payload)
	for _, entry := range data {
		if entry.(map[string]any)["id"] != newest["id"] {
			t.Errorf("since=max(discovered_at) returned an unexpected item: %v",
				entry.(map[string]any)["id"])
		}
	}
}

// Real first_seen values carry microseconds. Emitting whole seconds broke the
// documented polling loop -- max(discovered_at) came back truncated, the
// exclusive `since` failed to exclude the boundary row, and every poll
// re-delivered the same batch. The unit tests missed it because they seeded
// whole-second timestamps; a run against a real database caught it.
func TestDiscoveredAtKeepsSubSecondPrecisionForPolling(t *testing.T) {
	h := newTestHarness(t, "")
	if err := h.db.UpsertFeed(&database.Feed{URL: testFeedURL, Title: "Feed"}); err != nil {
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
