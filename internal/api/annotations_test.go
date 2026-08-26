package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lmorchard/feedspool-go/internal/ids"
)

const bodySeen = `{"kind":"seen"}`

func annotationsPath(guid string) string {
	return "/api/v1/items/" + ids.ItemID(testFeedURL, guid) + "/annotations"
}

func decodeArray(t *testing.T, payload []byte) []any {
	t.Helper()
	var decoded []any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("response is not a JSON array: %v (%s)", err, payload)
	}
	return decoded
}

func TestListAnnotationsEmptyIsArray(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	status, payload := h.get(t, annotationsPath("guid-0"))
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}
	if strings.TrimSpace(string(payload)) != emptyJSON {
		t.Errorf("body = %s, want an empty array", payload)
	}
}

func TestAddAnnotationCreatesThenReportsExisting(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)
	path := annotationsPath("guid-0")

	status, payload := h.do(t, http.MethodPost, path, jsonType, bodySeen)
	if status != http.StatusCreated {
		t.Fatalf("first POST status = %d, want 201: %s", status, payload)
	}
	decoded := decodeMap(t, payload)
	if decoded["kind"] != kindSeen {
		t.Errorf("kind = %v", decoded["kind"])
	}
	if decoded["value"] != nil || decoded["actor"] != nil {
		t.Errorf("value/actor = (%v, %v), want null", decoded["value"], decoded["actor"])
	}

	// Idempotent since migration 10: the second call is a no-op, and the
	// status is what distinguishes it.
	status, payload = h.do(t, http.MethodPost, path, jsonType, bodySeen)
	if status != http.StatusOK {
		t.Fatalf("second POST status = %d, want 200: %s", status, payload)
	}

	status, payload = h.get(t, path)
	if status != http.StatusOK {
		t.Fatal(status)
	}
	if got := decodeArray(t, payload); len(got) != 1 {
		t.Errorf("annotations = %d, want 1 after two identical POSTs", len(got))
	}
}

func TestAddAnnotationWithValueAndActor(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	status, payload := h.do(t, http.MethodPost, annotationsPath("guid-0"), jsonType,
		`{"kind":"tag","value":"later","actor":"me"}`)
	if status != http.StatusCreated {
		t.Fatalf("status = %d: %s", status, payload)
	}
	decoded := decodeMap(t, payload)
	if decoded["value"] != "later" || decoded["actor"] != "me" {
		t.Errorf("value/actor = (%v, %v)", decoded["value"], decoded["actor"])
	}
}

func TestDeleteAnnotationIsIdempotent(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)
	path := annotationsPath("guid-0")

	if status, payload := h.do(t, http.MethodPost, path, jsonType, bodySeen); status != http.StatusCreated {
		t.Fatalf("setup POST failed: %d %s", status, payload)
	}

	if status, _ := h.do(t, http.MethodDelete, path+"/seen", "", ""); status != http.StatusNoContent {
		t.Errorf("first DELETE status = %d, want 204", status)
	}
	// Deleting again must still be a 204, not a 404.
	if status, _ := h.do(t, http.MethodDelete, path+"/seen", "", ""); status != http.StatusNoContent {
		t.Errorf("second DELETE status = %d, want 204", status)
	}

	_, payload := h.get(t, path)
	if got := decodeArray(t, payload); len(got) != 0 {
		t.Errorf("annotations after delete = %d, want 0", len(got))
	}
}

// Mirrors RemoveAnnotation: ?value=x removes only that value, and the bare
// form removes only rows whose value is NULL.
func TestDeleteAnnotationRespectsValue(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)
	path := annotationsPath("guid-0")

	for _, body := range []string{`{"kind":"tag","value":"later"}`, `{"kind":"tag","value":"urgent"}`, `{"kind":"tag"}`} {
		if status, payload := h.do(t, http.MethodPost, path, jsonType, body); status != http.StatusCreated {
			t.Fatalf("setup POST %s failed: %d %s", body, status, payload)
		}
	}

	if status, _ := h.do(t, http.MethodDelete, path+"/tag?value=later", "", ""); status != http.StatusNoContent {
		t.Fatal("delete with value failed")
	}
	_, payload := h.get(t, path)
	if got := decodeArray(t, payload); len(got) != 2 {
		t.Errorf("annotations = %d, want 2 (urgent and the NULL-valued one)", len(got))
	}

	// The bare form takes the NULL-valued row, leaving "urgent" alone.
	if status, _ := h.do(t, http.MethodDelete, path+"/tag", "", ""); status != http.StatusNoContent {
		t.Fatal("bare delete failed")
	}
	_, payload = h.get(t, path)
	remaining := decodeArray(t, payload)
	if len(remaining) != 1 {
		t.Fatalf("annotations = %d, want 1", len(remaining))
	}
	if remaining[0].(map[string]any)["value"] != "urgent" {
		t.Errorf("surviving annotation = %v, want the urgent one", remaining[0])
	}
}

func TestAddAnnotationRequiresJSONContentType(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	status, payload := h.do(t, http.MethodPost, annotationsPath("guid-0"), "text/plain", bodySeen)
	if status != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415: %s", status, payload)
	}
	assertErrorCode(t, payload, codeUnsupportedMediaType)
}

func TestAddAnnotationAcceptsCharsetOnContentType(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	status, payload := h.do(t, http.MethodPost, annotationsPath("guid-0"),
		"application/json; charset=utf-8", bodySeen)
	if status != http.StatusCreated {
		t.Errorf("status = %d, want 201: %s", status, payload)
	}
}

func TestAddAnnotationRejectsBadKinds(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	tests := []struct{ name, body string }{
		{caseNameEmpty, `{"kind":""}`},
		{"missing", `{}`},
		{"too long", fmt.Sprintf(`{"kind":%q}`, strings.Repeat("a", 65))},
		{"contains a slash", `{"kind":"a/b"}`},
		{"contains a space", `{"kind":"a b"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, payload := h.do(t, http.MethodPost, annotationsPath("guid-0"), jsonType, tt.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", status, payload)
			}
			assertErrorCode(t, payload, codeInvalidParameter)
		})
	}
}

func TestAddAnnotationRejectsUnknownFields(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	status, payload := h.do(t, http.MethodPost, annotationsPath("guid-0"), jsonType,
		`{"kind":"seen","kindd":"typo"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", status, payload)
	}
}

func TestAnnotationOnUnknownItemIsNotFound(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	status, payload := h.do(t, http.MethodPost,
		"/api/v1/items/ffffffffffffffff/annotations", jsonType, bodySeen)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", status, payload)
	}
	assertErrorCode(t, payload, codeNotFound)
}

func TestBulkAnnotate(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	first := ids.ItemID(testFeedURL, "guid-0")
	second := ids.ItemID(testFeedURL, "guid-1")

	// Pre-mark one so already_present is exercised alongside added.
	if status, _ := h.do(t, http.MethodPost, annotationsPath("guid-0"), jsonType, bodySeen); status != http.StatusCreated {
		t.Fatal("setup failed")
	}

	body := fmt.Sprintf(`{"item_ids":[%q,%q,%q],"kind":"seen"}`, first, second, unknownItemID)
	status, payload := h.do(t, http.MethodPost, "/api/v1/annotations", jsonType, body)
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}

	decoded := decodeMap(t, payload)
	if decoded["added"] != float64(1) {
		t.Errorf("added = %v, want 1", decoded["added"])
	}
	if decoded["already_present"] != float64(1) {
		t.Errorf("already_present = %v, want 1", decoded["already_present"])
	}
	notFound, _ := decoded["not_found"].([]any)
	if len(notFound) != 1 || notFound[0] != unknownItemID {
		t.Errorf("not_found = %v, want the one unknown id", decoded["not_found"])
	}
}

// An unknown id is a tally entry, not a failure -- one bad id must not lose
// the writes for every good one in the batch.
func TestBulkAnnotateUnknownIDsDoNotFailTheRequest(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	body := fmt.Sprintf(`{"item_ids":[%q,"eeeeeeeeeeeeeeee"],"kind":"seen"}`, unknownItemID)
	status, payload := h.do(t, http.MethodPost, "/api/v1/annotations", jsonType, body)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, payload)
	}
	if decodeMap(t, payload)["added"] != float64(0) {
		t.Error("added should be 0 when no id resolved")
	}
}

func TestBulkAnnotateRejectsEmptyAndOversizedBatches(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	status, payload := h.do(t, http.MethodPost, "/api/v1/annotations", jsonType,
		`{"item_ids":[],"kind":"seen"}`)
	if status != http.StatusBadRequest {
		t.Errorf("empty batch status = %d, want 400: %s", status, payload)
	}

	oversized := make([]string, maxBulkItems+1)
	for i := range oversized {
		oversized[i] = fmt.Sprintf("%016x", i)
	}
	encoded, err := json.Marshal(map[string]any{fieldItemIDs: oversized, "kind": "seen"})
	if err != nil {
		t.Fatal(err)
	}
	status, payload = h.do(t, http.MethodPost, "/api/v1/annotations", jsonType, string(encoded))
	if status != http.StatusBadRequest {
		t.Errorf("oversized batch status = %d, want 400: %s", status, payload)
	}
	assertErrorCode(t, payload, codeInvalidParameter)
}

func TestAnnotationsAppearInItemInclude(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)
	if status, _ := h.do(t, http.MethodPost, annotationsPath("guid-2"), jsonType, bodySeen); status != http.StatusCreated {
		t.Fatal("setup failed")
	}

	status, payload := h.get(t, "/api/v1/items/"+ids.ItemID(testFeedURL, "guid-2"))
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}
	annotations, ok := decodeMap(t, payload)["annotations"].([]any)
	if !ok || len(annotations) != 1 {
		t.Fatalf("annotations = %v, want one entry", decodeMap(t, payload)["annotations"])
	}
	if annotations[0].(map[string]any)["kind"] != kindSeen {
		t.Errorf("kind = %v", annotations[0])
	}
}

// The seen filter has to observe writes made through the API, which is the
// round trip a reader UI actually depends on.
func TestSeenFilterReflectsAPIWrites(t *testing.T) {
	h := newTestHarness(t, "")
	h.seed(t)

	if status, _ := h.do(t, http.MethodPost, annotationsPath("guid-3"), jsonType, bodySeen); status != http.StatusCreated {
		t.Fatal("setup failed")
	}

	status, payload := h.get(t, "/api/v1/items?seen=true")
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, payload)
	}
	data, _, _ := decodeCollection(t, payload)
	if len(data) != 1 {
		t.Fatalf("seen=true results = %d, want 1", len(data))
	}

	_, payload = h.get(t, "/api/v1/items?seen=false")
	data, _, _ = decodeCollection(t, payload)
	if len(data) != 4 {
		t.Errorf("seen=false results = %d, want 4", len(data))
	}
}
