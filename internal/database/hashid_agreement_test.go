package database

import (
	"testing"

	"github.com/lmorchard/feedspool-go/internal/ids"
)

const testGUID = "guid-1"

// FeedHashID and ItemHashID duplicate internal/ids so that internal/database
// does not have to import it. internal/ids is the canonical definition; if
// these ever disagree, an item addressable through the API would resolve to a
// different row than the same ID resolves to elsewhere. This test is the only
// thing keeping the two copies honest, so do not delete it along with the
// duplication -- delete it only if the duplication itself goes away.
func TestHashIDsAgreeWithIDsPackage(t *testing.T) {
	feedURLs := []string{
		annotationTestFeed,
		"https://blog.example.org/atom",
		"",
	}
	for _, feedURL := range feedURLs {
		if got, want := FeedHashID(feedURL), ids.FeedID(feedURL); got != want {
			t.Errorf("FeedHashID(%q) = %q, ids.FeedID() = %q", feedURL, got, want)
		}
	}

	itemKeys := []struct{ feedURL, guid string }{
		{annotationTestFeed, testGUID},
		{annotationTestFeed, ""},
		{"", testGUID},
		{"ab", "c"},
		{"a", "bc"},
	}
	for _, key := range itemKeys {
		got := ItemHashID(key.feedURL, key.guid)
		want := ids.ItemID(key.feedURL, key.guid)
		if got != want {
			t.Errorf("ItemHashID(%q, %q) = %q, ids.ItemID() = %q", key.feedURL, key.guid, got, want)
		}
	}
}
