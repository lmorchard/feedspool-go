package ids

import "testing"

// These values were produced by the renderer's original generateFeedID before
// it moved here. They are published as static-site URLs (feeds/<id>.html), so
// a change to them silently breaks bookmarks. Treat a failure here as a
// compatibility break, not a stale fixture.
func TestFeedIDMatchesRendererGoldenValues(t *testing.T) {
	tests := []struct{ url, want string }{
		{"https://example.com/feed.xml", "7a775db7"},
		{"https://blog.example.org/atom", "28044623"},
	}
	for _, tt := range tests {
		if got := FeedID(tt.url); got != tt.want {
			t.Errorf("FeedID(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestFeedIDIsEightLowercaseHexChars(t *testing.T) {
	got := FeedID("https://example.com/feed.xml")
	if len(got) != feedIDLength {
		t.Errorf("FeedID() length = %d, want %d", len(got), feedIDLength)
	}
	assertLowerHex(t, got)
}

func TestItemIDIsSixteenLowercaseHexChars(t *testing.T) {
	got := ItemID("https://example.com/feed.xml", "guid-1")
	if len(got) != itemIDLength {
		t.Errorf("ItemID() length = %d, want %d", len(got), itemIDLength)
	}
	assertLowerHex(t, got)
}

// Without a separator, concatenation makes ("ab", "c") and ("a", "bc")
// indistinguishable, so two different items would share an ID.
func TestItemIDSeparatorPreventsShiftedBoundaryCollision(t *testing.T) {
	if ItemID("ab", "c") == ItemID("a", "bc") {
		t.Error("ItemID() collided across a shifted boundary; the separator is missing")
	}
}

func TestItemIDVariesByFeed(t *testing.T) {
	if ItemID("https://a.example/f", "g") == ItemID("https://b.example/f", "g") {
		t.Error("ItemID() must differ when the feed URL differs")
	}
}

func TestItemIDVariesByGUID(t *testing.T) {
	if ItemID("https://a.example/f", "g1") == ItemID("https://a.example/f", "g2") {
		t.Error("ItemID() must differ when the GUID differs")
	}
}

func assertLowerHex(t *testing.T, s string) {
	t.Helper()
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			t.Fatalf("got %q, want lowercase hex", s)
		}
	}
}
