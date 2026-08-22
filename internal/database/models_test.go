package database

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
)

// Shared test fixtures, hoisted so goconst stays quiet.
const (
	fixtureFeedURL      = "https://example.com/feed.xml"
	fixtureFeedURL1     = "https://example1.com/feed.xml"
	fixtureItemLink     = "https://example.com/item"
	fixtureFeedTitle    = "Test Feed"
	fixtureItemContent  = "Test content"
	fixtureItemSummary  = "Test summary"
	fixtureGUID         = "test-guid"
	fixtureGUID1        = "item1"
	fixtureGUID2        = "item2"
	fixtureGUID3        = "item3"
	fixtureJSONNull     = "null"
	fixtureJSONArray    = "[1, 2, 3]"
	fixtureJSONObject   = "{\"test\": \"value\"}"
	caseNilJSON         = "nil JSON"
	caseValidJSONArray  = "valid JSON array"
	caseValidJSONObject = "valid JSON object"
)

const (
	testItemTitle = "Test Item"
	testBBCLink   = "https://www.bbc.com/news/articles/ce9y1747z3go"
)

func TestJSONValue(t *testing.T) {
	tests := []struct {
		name string
		j    JSON
		want string
	}{
		{
			name: caseNilJSON,
			j:    nil,
			want: "",
		},
		{
			name: "valid JSON",
			j:    JSON(fixtureJSONObject),
			want: fixtureJSONObject,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.j.Value()
			if err != nil {
				t.Errorf("JSON.Value() error = %v", err)
				return
			}
			if tt.j == nil {
				if got != nil {
					t.Errorf("JSON.Value() = %v, want nil", got)
				}
			} else {
				gotStr := string(got.([]byte))
				if gotStr != tt.want {
					t.Errorf("JSON.Value() = %v, want %v", gotStr, tt.want)
				}
			}
		})
	}
}

func TestJSONScan(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  string
	}{
		{
			name:  "nil value",
			value: nil,
			want:  fixtureJSONNull,
		},
		{
			name:  "byte slice",
			value: []byte(fixtureJSONObject),
			want:  fixtureJSONObject,
		},
		{
			name:  "string value",
			value: fixtureJSONObject,
			want:  fixtureJSONObject,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JSON
			err := j.Scan(tt.value)
			if err != nil {
				t.Errorf("JSON.Scan() error = %v", err)
				return
			}
			if string(j) != tt.want {
				t.Errorf("JSON.Scan() = %v, want %v", string(j), tt.want)
			}
		})
	}
}

func TestJSONScanError(t *testing.T) {
	var j JSON
	err := j.Scan(123) // invalid type
	if err == nil {
		t.Errorf("JSON.Scan() should have returned an error for invalid type")
	}
}

func TestJSONMarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		j    JSON
		want string
	}{
		{
			name: caseNilJSON,
			j:    nil,
			want: fixtureJSONNull,
		},
		{
			name: "empty JSON",
			j:    JSON{},
			want: fixtureJSONNull,
		},
		{
			name: caseValidJSONObject,
			j:    JSON(`{"test": "value", "number": 42}`),
			want: `{"test": "value", "number": 42}`,
		},
		{
			name: caseValidJSONArray,
			j:    JSON(fixtureJSONArray),
			want: fixtureJSONArray,
		},
		{
			name: "nested JSON",
			j:    JSON(`{"outer": {"inner": "value"}}`),
			want: `{"outer": {"inner": "value"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.j.MarshalJSON()
			if err != nil {
				t.Errorf("JSON.MarshalJSON() error = %v", err)
				return
			}
			if string(got) != tt.want {
				t.Errorf("JSON.MarshalJSON() = %v, want %v", string(got), tt.want)
			}
		})
	}
}

func TestJSONUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: caseValidJSONObject,
			data: []byte(fixtureJSONObject),
			want: fixtureJSONObject,
		},
		{
			name: caseValidJSONArray,
			data: []byte(fixtureJSONArray),
			want: fixtureJSONArray,
		},
		{
			name: "null JSON",
			data: []byte(fixtureJSONNull),
			want: fixtureJSONNull,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JSON
			err := j.UnmarshalJSON(tt.data)
			if err != nil {
				t.Errorf("JSON.UnmarshalJSON() error = %v", err)
				return
			}
			if string(j) != tt.want {
				t.Errorf("JSON.UnmarshalJSON() = %v, want %v", string(j), tt.want)
			}
		})
	}
}

func TestJSONMarshalUnmarshalRoundTrip(t *testing.T) {
	// Test that marshaling and then unmarshaling preserves the JSON
	originalData := `{"title":"` + testItemTitle + `","link":"https://example.com","array":[1,2,3],"nested":{"key":"value"}}`

	// Create a JSON value
	j1 := JSON(originalData)

	// Marshal it
	marshaled, err := json.Marshal(j1)
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}

	// Unmarshal it back
	var j2 JSON
	err = json.Unmarshal(marshaled, &j2)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	// They should be equal
	if string(j1) != string(j2) {
		t.Errorf("Round trip failed: original = %v, result = %v", string(j1), string(j2))
	}

	// Verify the marshaled data is valid JSON (not base64)
	var testObj map[string]interface{}
	err = json.Unmarshal(marshaled, &testObj)
	if err != nil {
		t.Errorf("Marshaled data is not valid JSON: %v", err)
	}
}

func TestItemJSONMarshalNotBase64(t *testing.T) {
	// This test ensures that ItemJSON is not base64 encoded when marshaled
	item := &Item{
		ID:            1,
		FeedURL:       fixtureFeedURL,
		GUID:          fixtureGUID,
		Title:         testItemTitle,
		Link:          fixtureItemLink,
		PublishedDate: time.Now(),
		ItemJSON:      JSON(`{"title":"` + testItemTitle + `","custom":{"field":"value"}}`),
	}

	// Marshal the item
	marshaled, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("Failed to marshal Item: %v", err)
	}

	// Unmarshal to a map to check the ItemJSON field
	var result map[string]interface{}
	err = json.Unmarshal(marshaled, &result)
	if err != nil {
		t.Fatalf("Failed to unmarshal to map: %v", err)
	}

	// ItemJSON should be a map (parsed JSON), not a string (base64)
	itemJSONField, ok := result["ItemJSON"]
	if !ok {
		t.Fatal("ItemJSON field not found in marshaled data")
	}

	// If it's a string, it means it was base64 encoded (bad)
	// If it's a map, it means it was properly marshaled as JSON (good)
	if _, isString := itemJSONField.(string); isString {
		t.Errorf("ItemJSON was marshaled as base64 string, should be JSON object")
	}

	if itemJSONMap, isMap := itemJSONField.(map[string]interface{}); isMap {
		// Verify the content
		if title, ok := itemJSONMap["title"].(string); !ok || title != testItemTitle {
			t.Errorf("ItemJSON content incorrect: %v", itemJSONMap)
		}
	} else {
		t.Errorf("ItemJSON should be a JSON object, got %T", itemJSONField)
	}
}

func TestFeedFromGofeed(t *testing.T) {
	const (
		testFeedTitle       = fixtureFeedTitle
		testFeedURL         = fixtureFeedURL
		testFeedDescription = "Test Description"
	)

	now := time.Now()
	gofeedData := &gofeed.Feed{
		Title:         testFeedTitle,
		Description:   testFeedDescription,
		UpdatedParsed: &now,
	}

	feed, err := FeedFromGofeed(gofeedData, testFeedURL)
	if err != nil {
		t.Errorf("FeedFromGofeed() error = %v", err)
		return
	}

	if feed.URL != testFeedURL {
		t.Errorf("Feed.URL = %v, want %v", feed.URL, testFeedURL)
	}

	if feed.Title != testFeedTitle {
		t.Errorf("Feed.Title = %v, want %v", feed.Title, testFeedTitle)
	}

	if feed.Description != testFeedDescription {
		t.Errorf("Feed.Description = %v, want %v", feed.Description, testFeedDescription)
	}

	if !feed.LastUpdated.Equal(now) {
		t.Errorf("Feed.LastUpdated = %v, want %v", feed.LastUpdated, now)
	}

	// Check that JSON was properly marshaled
	var feedData gofeed.Feed
	err = json.Unmarshal(feed.FeedJSON, &feedData)
	if err != nil {
		t.Errorf("Failed to unmarshal FeedJSON: %v", err)
	}
	if feedData.Title != testFeedTitle {
		t.Errorf("FeedJSON.Title = %v, want %v", feedData.Title, testFeedTitle)
	}
}

func TestItemFromGofeed(t *testing.T) {
	const testItemURL = fixtureFeedURL

	now := time.Now()
	gofeedItem := &gofeed.Item{
		GUID:            fixtureGUID,
		Title:           testItemTitle,
		Link:            fixtureItemLink,
		Content:         fixtureItemContent,
		Description:     fixtureItemSummary,
		PublishedParsed: &now,
	}

	item, err := ItemFromGofeed(gofeedItem, testItemURL)
	if err != nil {
		t.Errorf("ItemFromGofeed() error = %v", err)
		return
	}

	if item.FeedURL != testItemURL {
		t.Errorf("Item.FeedURL = %v, want %v", item.FeedURL, testItemURL)
	}

	if item.GUID != fixtureGUID {
		t.Errorf("Item.GUID = %v, want %v", item.GUID, fixtureGUID)
	}

	if item.Title != testItemTitle {
		t.Errorf("Item.Title = %v, want %v", item.Title, testItemTitle)
	}

	if !item.PublishedDate.Equal(now) {
		t.Errorf("Item.PublishedDate = %v, want %v", item.PublishedDate, now)
	}
}

func TestItemFromGofeedNoGUID(t *testing.T) {
	gofeedItem := &gofeed.Item{
		Title: testItemTitle,
		Link:  fixtureItemLink,
	}

	item, err := ItemFromGofeed(gofeedItem, fixtureFeedURL)
	if err != nil {
		t.Errorf("ItemFromGofeed() error = %v", err)
		return
	}

	// Should generate GUID from link+title
	if item.GUID == "" {
		t.Errorf("Item.GUID should not be empty when no GUID provided")
	}

	// Should be consistent
	item2, _ := ItemFromGofeed(gofeedItem, fixtureFeedURL)
	if item.GUID != item2.GUID {
		t.Errorf("Generated GUID should be consistent: %v != %v", item.GUID, item2.GUID)
	}
}

func TestGenerateGUID(t *testing.T) {
	link := fixtureItemLink
	title := testItemTitle

	guid1 := generateGUID(link, title)
	guid2 := generateGUID(link, title)

	if guid1 != guid2 {
		t.Errorf("generateGUID should be deterministic: %v != %v", guid1, guid2)
	}

	if len(guid1) != 64 { // SHA256 hex string length
		t.Errorf("generateGUID should return 64-char hex string, got %d chars", len(guid1))
	}

	// Different inputs should produce different GUIDs
	guid3 := generateGUID("different-link", title)
	if guid1 == guid3 {
		t.Errorf("Different inputs should produce different GUIDs")
	}
}

func TestNormalizeGUID(t *testing.T) {
	tests := []struct {
		name        string
		guid        string
		link        string
		title       string
		description string
	}{
		{
			name:        "BBC-style GUID with fragment",
			guid:        "https://www.bbc.com/news/articles/ce9y1747z3go#0",
			link:        testBBCLink,
			title:       testItemTitle,
			description: "Should normalize BBC-style GUIDs with incrementing fragments",
		},
		{
			name:        "BBC-style GUID with different fragment number",
			guid:        "https://www.bbc.com/news/articles/ce9y1747z3go#5",
			link:        testBBCLink,
			title:       testItemTitle,
			description: "Should produce same result regardless of fragment number",
		},
		{
			name:        "Normal GUID not matching link",
			guid:        "some-unique-guid-12345",
			link:        fixtureItemLink,
			title:       testItemTitle,
			description: "Should leave normal GUIDs unchanged",
		},
		{
			name:        "Empty link",
			guid:        "https://example.com#5",
			link:        "",
			title:       testItemTitle,
			description: "Should return GUID unchanged when link is empty",
		},
		{
			name:        "GUID without fragment",
			guid:        fixtureItemLink,
			link:        fixtureItemLink,
			title:       testItemTitle,
			description: "Should return GUID unchanged when no fragment present",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized := normalizeGUID(tt.guid, tt.link, tt.title)

			// Verify it's not empty
			if normalized == "" {
				t.Errorf("normalizeGUID() returned empty string")
			}

			// For BBC-style GUIDs, verify normalization produces consistent results
			if tt.link != "" && len(tt.guid) > len(tt.link) &&
				tt.guid[:len(tt.link)] == tt.link && tt.guid[len(tt.link)] == '#' {
				// This is a BBC-style GUID - should be normalized
				expected := generateGUID(tt.link, tt.title)
				if normalized != expected {
					t.Errorf("normalizeGUID() = %v, want %v", normalized, expected)
				}
			}
		})
	}
}

func TestNormalizeGUIDConsistency(t *testing.T) {
	// Test that BBC-style GUIDs with different fragments produce the same normalized GUID
	link := testBBCLink
	title := testItemTitle

	guid1 := normalizeGUID(link+"#0", link, title)
	guid2 := normalizeGUID(link+"#5", link, title)
	guid3 := normalizeGUID(link+"#10", link, title)

	if guid1 != guid2 || guid2 != guid3 {
		t.Errorf("BBC-style GUIDs with different fragments should normalize to same value: %v, %v, %v",
			guid1, guid2, guid3)
	}
}

func TestItemFromGofeedWithBBCStyleGUID(t *testing.T) {
	// Test that BBC-style GUIDs are normalized during item creation
	link := testBBCLink

	gofeedItem1 := &gofeed.Item{
		GUID:  link + "#0",
		Title: testItemTitle,
		Link:  link,
	}

	gofeedItem2 := &gofeed.Item{
		GUID:  link + "#5",
		Title: testItemTitle,
		Link:  link,
	}

	item1, err := ItemFromGofeed(gofeedItem1, "https://feeds.bbci.co.uk/news/rss.xml")
	if err != nil {
		t.Errorf("ItemFromGofeed() error = %v", err)
		return
	}

	item2, err := ItemFromGofeed(gofeedItem2, "https://feeds.bbci.co.uk/news/rss.xml")
	if err != nil {
		t.Errorf("ItemFromGofeed() error = %v", err)
		return
	}

	// Items with same link/title but different BBC-style fragment GUIDs should have same normalized GUID
	if item1.GUID != item2.GUID {
		t.Errorf("Items with BBC-style GUIDs should have same normalized GUID: %v != %v",
			item1.GUID, item2.GUID)
	}
}
