package api

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
)

func TestWriteErrorEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeError(recorder, 404, codeNotFound, "no such item")

	if recorder.Code != 404 {
		t.Errorf("status = %d, want 404", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", contentType)
	}

	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != codeNotFound || envelope.Error.Message != "no such item" {
		t.Errorf("body = %+v", envelope.Error)
	}
}

func TestItemCursorRoundTrip(t *testing.T) {
	for _, original := range []database.ItemCursor{
		{DateRank: 0, EffectiveDate: 2460310.5, ID: 42},
		{DateRank: 1, EffectiveDate: 0, ID: 1},
		{DateRank: 0, EffectiveDate: 0.000001, ID: 9007199254740993},
	} {
		encoded := encodeItemCursor(&original)
		decoded, err := decodeItemCursor(encoded)
		if err != nil {
			t.Fatalf("decodeItemCursor(%q) error = %v", encoded, err)
		}
		if *decoded != original {
			t.Errorf("round trip = %+v, want %+v", *decoded, original)
		}
	}
}

// A bad cursor must be an error, never a zero value. Silently restarting from
// the beginning would make a paging loop repeat forever without complaining.
func TestDecodeItemCursorRejectsGarbage(t *testing.T) {
	tests := []struct{ name, input string }{
		{"not base64", "!!!not base64!!!"},
		{"base64 of non-JSON", "aGVsbG8gd29ybGQ"},
		{"base64 of wrong-shaped JSON", "eyJ4IjoxfQ"},
		{caseNameEmpty, ""},
		{"truncated", encodeItemCursor(&database.ItemCursor{ID: 5})[:4]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := decodeItemCursor(tt.input); err == nil {
				t.Errorf("decodeItemCursor(%q) = %+v, want an error", tt.input, got)
			}
		})
	}
}

func TestDecodeItemCursorRejectsUnsupportedVersion(t *testing.T) {
	payload, err := json.Marshal(map[string]any{"v": 99, "r": 0, "d": 1.0, "i": 1})
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	if _, err := decodeItemCursor(encoded); err == nil {
		t.Error("decodeItemCursor() accepted an unsupported cursor version")
	}
}

func TestFeedCursorRoundTrip(t *testing.T) {
	encoded := encodeFeedCursor("https://example.com/feed.xml")
	decoded, err := decodeFeedCursor(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != "https://example.com/feed.xml" {
		t.Errorf("round trip = %q", decoded)
	}
}

func TestParseLimit(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{"empty uses default", "", 50, false},
		{"in range", "10", 10, false},
		{"at max", "200", 200, false},
		{"above max clamps", "5000", 200, false},
		{"zero rejected", "0", 0, true},
		{"negative rejected", "-1", 0, true},
		{"non-numeric rejected", "ten", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLimit(tt.input, 50, 200)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseLimit(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseLimit(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseTriState(t *testing.T) {
	tests := []struct {
		input   string
		want    *bool
		wantErr bool
	}{
		{"", nil, false},
		{"any", nil, false},
		{"true", boolPtr(true), false},
		{"false", boolPtr(false), false},
		{"maybe", nil, true},
	}
	for _, tt := range tests {
		got, err := parseTriState(tt.input)
		if (err != nil) != tt.wantErr {
			t.Fatalf("parseTriState(%q) error = %v", tt.input, err)
		}
		if tt.wantErr {
			continue
		}
		if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
			t.Errorf("parseTriState(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseBoolFilterRejectsAny(t *testing.T) {
	if _, err := parseBoolFilter("any"); err == nil {
		t.Error("parseBoolFilter(\"any\") = nil error; only archived is tri-state")
	}
}

func TestParseInclude(t *testing.T) {
	allowed := []string{includeContent, includeAnnotations}

	set, err := parseInclude("content,annotations", allowed)
	if err != nil {
		t.Fatal(err)
	}
	if !set.has(includeContent) || !set.has(includeAnnotations) {
		t.Errorf("parseInclude() = %v, want both values set", set)
	}

	if _, err := parseInclude("bogus", allowed); err == nil {
		t.Error("parseInclude() accepted an unknown value")
	}
	// counts is a real include value, but not on this endpoint.
	if _, err := parseInclude(includeCounts, allowed); err == nil {
		t.Error("parseInclude() accepted a value the endpoint does not offer")
	}
}

func TestRejectUnknownParams(t *testing.T) {
	allowed := []string{paramLimit, paramCursor}

	if err := rejectUnknownParams(url.Values{paramLimit: {"5"}}, allowed); err != nil {
		t.Errorf("rejectUnknownParams() rejected a valid parameter: %v", err)
	}
	// The typo that motivates this: it would otherwise silently return the default page.
	if err := rejectUnknownParams(url.Values{"limitt": {"5"}}, allowed); err == nil {
		t.Error("rejectUnknownParams() accepted \"limitt\"")
	}
}

func TestParseRFC3339(t *testing.T) {
	if got, err := parseRFC3339(""); err != nil || !got.IsZero() {
		t.Errorf("parseRFC3339(\"\") = %v, %v; want zero time and no error", got, err)
	}
	if _, err := parseRFC3339("2026-08-25"); err == nil {
		t.Error("parseRFC3339() accepted a date without a time")
	}
	got, err := parseRFC3339("2026-08-25T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("parseRFC3339() = %v", got)
	}
}

// The whole reason DTOs are hand-written: the models render nullable columns
// as {"Time":...,"Valid":false} when marshaled directly.
func TestItemDTOEmitsNullNotNullTimeShape(t *testing.T) {
	item := &database.Item{FeedURL: dtoFeedURL, GUID: "g", Title: "T"}
	encoded, err := json.Marshal(itemDTO(item, includeSet{}, nil, nil))
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"first_seen", "published_date", "discovered_at"} {
		value, present := decoded[field]
		if !present {
			t.Errorf("%s is absent; it should be present and null", field)
		}
		if value != nil {
			t.Errorf("%s = %v, want null", field, value)
		}
	}
}

func TestItemDTOOmitsHeavyFieldsByDefault(t *testing.T) {
	item := &database.Item{FeedURL: dtoFeedURL, GUID: "g", Content: dtoBody}
	dto := itemDTO(item, includeSet{}, nil, nil)

	for _, field := range []string{"content", "item_json", "annotations", "metadata"} {
		if _, present := dto[field]; present {
			t.Errorf("%s is present without being requested", field)
		}
	}
}

func TestItemDTOIncludesContentWhenRequested(t *testing.T) {
	item := &database.Item{FeedURL: dtoFeedURL, GUID: "g", Content: dtoBody}
	dto := itemDTO(item, includeSet{includeContent: true}, nil, nil)

	if dto["content"] != dtoBody {
		t.Errorf("content = %v, want the body", dto["content"])
	}
}

// An empty annotation list must serialize as [] so clients can range over it
// without a nil check.
func TestItemDTOEmptyAnnotationsIsArrayNotNull(t *testing.T) {
	item := &database.Item{FeedURL: dtoFeedURL, GUID: "g"}
	encoded, err := json.Marshal(itemDTO(item, includeSet{includeAnnotations: true}, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if string(decoded["annotations"]) != emptyJSON {
		t.Errorf("annotations = %s, want an empty array", decoded["annotations"])
	}
}

func TestItemDTOUsesFirstSeenForDiscoveredAt(t *testing.T) {
	firstSeen := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	item := &database.Item{
		FeedURL:   dtoFeedURL,
		GUID:      "g",
		FirstSeen: sql.NullTime{Time: firstSeen, Valid: true},
	}
	dto := itemDTO(item, includeSet{}, nil, nil)

	if dto["published_date"] != nil {
		t.Errorf("published_date = %v, want null for a scraped item", dto["published_date"])
	}
	if dto["discovered_at"] != "2026-03-04T05:06:07Z" {
		t.Errorf("discovered_at = %v, want the first_seen value", dto["discovered_at"])
	}
}

func TestFeedDTOCarriesParserFields(t *testing.T) {
	feed := &database.Feed{
		URL:            dtoFeedURL,
		Type:           database.FeedTypeScrape,
		ScrapeSelector: dtoSelector,
	}
	dto := feedDTO(feed, includeSet{}, 0, 0)

	if dto["type"] != database.FeedTypeScrape {
		t.Errorf("type = %v", dto["type"])
	}
	if dto["scrape_selector"] != dtoSelector {
		t.Errorf("scrape_selector = %v", dto["scrape_selector"])
	}
	if _, present := dto["item_count"]; present {
		t.Error("item_count is present without include=counts")
	}
}

func TestFeedDTOIncludesCountsWhenRequested(t *testing.T) {
	feed := &database.Feed{URL: dtoFeedURL}
	dto := feedDTO(feed, includeSet{includeCounts: true}, 12, 3)

	if dto["item_count"] != 12 || dto["unseen_count"] != 3 {
		t.Errorf("counts = (%v, %v), want (12, 3)", dto["item_count"], dto["unseen_count"])
	}
}

func TestAnnotationDTOParsesSQLiteTimestamp(t *testing.T) {
	dto := annotationDTO(&database.ItemAnnotation{
		Kind:      kindSeen,
		CreatedAt: "2026-08-25 17:14:03",
	})
	if dto["created_at"] != "2026-08-25T17:14:03Z" {
		t.Errorf("created_at = %v, want RFC3339", dto["created_at"])
	}
	if dto["value"] != nil || dto["actor"] != nil {
		t.Errorf("unset value/actor = (%v, %v), want null", dto["value"], dto["actor"])
	}
}

// Losing a timestamp is worse than returning one the client has to interpret.
func TestAnnotationDTOPassesThroughUnparseableTimestamp(t *testing.T) {
	dto := annotationDTO(&database.ItemAnnotation{Kind: kindSeen, CreatedAt: unparseableTS})
	if dto["created_at"] != unparseableTS {
		t.Errorf("created_at = %v, want the raw value preserved", dto["created_at"])
	}
}

func boolPtr(value bool) *bool { return &value }
