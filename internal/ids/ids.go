// Package ids derives the stable public identifiers feedspool uses for feeds
// and items in URLs and API payloads.
//
// They are computed from natural keys rather than stored, so they survive a
// purge-and-refetch cycle that would reassign items.id, and any caller holding
// a feed URL or a (feed URL, GUID) pair can recompute one without a lookup.
package ids

import (
	"crypto/sha256"
	"encoding/hex"
)

const (
	feedIDLength = 8
	itemIDLength = 16
)

// itemIDSeparator keeps ("ab", "c") from hashing the same as ("a", "bc"). A
// newline is safe because it cannot appear unescaped in a URL.
const itemIDSeparator = "\n"

// FeedID returns the public identifier for a feed URL.
//
// The length is 8 rather than something roomier because these values are
// already published as static-site URLs (feeds/<id>.html) by the renderer.
// Widening it would break existing bookmarks.
func FeedID(feedURL string) string {
	sum := sha256.Sum256([]byte(feedURL))
	return hex.EncodeToString(sum[:])[:feedIDLength]
}

// ItemID returns the public identifier for an item, keyed on the same
// (feed URL, GUID) pair that uniquely identifies its row.
func ItemID(feedURL, guid string) string {
	sum := sha256.Sum256([]byte(feedURL + itemIDSeparator + guid))
	return hex.EncodeToString(sum[:])[:itemIDLength]
}
