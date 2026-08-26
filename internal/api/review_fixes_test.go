package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/ids"
)

// AnnotationExists and migration 10's unique index both compare through a
// COALESCE, so NULL and "" are the same annotation to the database.
// Treating them as distinct on read-back turned a successful no-op write into
// a 500.
func TestAddAnnotationEmptyValueMatchesNullValue(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)
	path := annotationsPath("guid-0")

	if status, payload := h.do(t, http.MethodPost, path, jsonType, bodySeen); status != http.StatusCreated {
		t.Fatalf("setup POST = %d: %s", status, payload)
	}

	// Same annotation, spelled with an explicit empty value.
	status, payload := h.do(t, http.MethodPost, path, jsonType, `{"kind":"seen","value":""}`)
	if status != http.StatusOK {
		t.Fatalf("POST with value:\"\" = %d, want 200: %s", status, payload)
	}

	if got := decodeArray(t, mustGet(t, h, path)); len(got) != 1 {
		t.Errorf("annotations = %d, want 1 -- NULL and \"\" are the same row", len(got))
	}
}

// The reverse order fails the same way if the comparison is naive.
func TestAddAnnotationNullValueMatchesEmptyValue(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)
	path := annotationsPath("guid-0")

	if status, payload := h.do(t, http.MethodPost, path, jsonType,
		`{"kind":"seen","value":""}`); status != http.StatusCreated {
		t.Fatalf("setup POST = %d: %s", status, payload)
	}

	status, payload := h.do(t, http.MethodPost, path, jsonType, bodySeen)
	if status != http.StatusOK {
		t.Fatalf("POST with value omitted = %d, want 200: %s", status, payload)
	}
}

// The catch-all handles unknown paths and method mismatches. Left unguarded it
// lets an anonymous client map the API by reading 404 against 405.
func TestCatchAllRequiresAuthWhenTokenIsSet(t *testing.T) {
	h := newTestHarness(t, testToken)

	tests := []struct {
		name, method, path string
	}{
		{"unknown path", http.MethodGet, "/api/v1/doesnotexist"},
		{"method mismatch on a real path", http.MethodPost, pathStatus},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, payload := h.do(t, tt.method, tt.path, jsonType, "")
			if status != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401 so the surface is not enumerable: %s", status, payload)
			}
		})
	}
}

// With no token configured the catch-all still has to distinguish the two.
func TestCatchAllStillDistinguishes404From405WhenOpen(t *testing.T) {
	h := newTestHarness(t, "")

	if status, _ := h.get(t, "/api/v1/doesnotexist"); status != http.StatusNotFound {
		t.Errorf("unknown path = %d, want 404", status)
	}
	if status, _ := h.do(t, http.MethodPost, pathStatus, jsonType, "{}"); status != http.StatusMethodNotAllowed {
		t.Errorf("method mismatch = %d, want 405", status)
	}
}

// Bulk resolves the whole batch in one scan now. This pins the behavior --
// tallies and ordering-independence -- so the optimization cannot regress
// correctness.
func TestBulkAnnotateResolvesBatchCorrectly(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	known := make([]string, 0, 5)
	for i := range 5 {
		known = append(known, ids.ItemID(testFeedURL, fmt.Sprintf("guid-%d", i)))
	}
	// Interleave misses with hits so a positional bug would show up.
	batch := []string{
		unknownItemID, known[0], "eeeeeeeeeeeeeeee", known[1], known[2],
		known[3], known[4], "dddddddddddddddd",
	}

	body, err := jsonBody(map[string]any{fieldItemIDs: batch, fieldKindKey: kindSeen})
	if err != nil {
		t.Fatal(err)
	}
	status, payload := h.do(t, http.MethodPost, "/api/v1/annotations", jsonType, body)
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}

	decoded := decodeMap(t, payload)
	if decoded["added"] != float64(5) {
		t.Errorf("added = %v, want 5", decoded["added"])
	}
	notFound, _ := decoded["not_found"].([]any)
	if len(notFound) != 3 {
		t.Errorf("not_found = %v, want the 3 unknown ids", decoded["not_found"])
	}

	// Every hit really got annotated, not just counted.
	_, page := h.get(t, "/api/v1/items?seen=true&limit=10")
	data, _, _ := decodeCollection(t, page)
	if len(data) != 5 {
		t.Errorf("items marked seen = %d, want 5", len(data))
	}
}

// A repeated id inside one batch must not double-count.
func TestBulkAnnotateHandlesDuplicateIDsInBatch(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	id := ids.ItemID(testFeedURL, "guid-0")
	body, err := jsonBody(map[string]any{fieldItemIDs: []string{id, id, id}, fieldKindKey: kindSeen})
	if err != nil {
		t.Fatal(err)
	}
	status, payload := h.do(t, http.MethodPost, "/api/v1/annotations", jsonType, body)
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}

	decoded := decodeMap(t, payload)
	if decoded["added"] != float64(1) || decoded["already_present"] != float64(2) {
		t.Errorf("tallies = (added %v, already_present %v), want (1, 2)",
			decoded["added"], decoded["already_present"])
	}
}

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
