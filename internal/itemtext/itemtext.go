// Package itemtext derives the canonical text that represents a feed item:
// title, summary, and body with HTML stripped, entities decoded, whitespace
// collapsed, and each field truncated to a byte cap. It is pure and has no
// database dependency. Phase-2 indexing feeds its output into SQLite FTS5;
// issue #30's embedder is intended to call Derive with a smaller truncation
// cap for its own token limits.
package itemtext

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// Generator and Version identify this derivation in item_text bookkeeping.
// Bump Version whenever the output of Derive changes for the same input --
// that is what forces a reindex.
const (
	Generator = "itemtext"
	Version   = 1
)

// DefaultMaxTitleBytes, DefaultMaxSummaryBytes, and DefaultMaxBodyBytes are
// bloat guards, not recall limits: long-form articles run well under them.
// #30 will pass a far smaller cap for embedding token limits, which is why
// this is an option rather than a constant in Derive.
const (
	DefaultMaxTitleBytes   = 4 * 1024
	DefaultMaxSummaryBytes = 32 * 1024
	DefaultMaxBodyBytes    = 256 * 1024
)

// sourceHashLength matches the truncated-hash idiom used by
// internal/database.ItemHashID.
const sourceHashLength = 32

// sourceHashSep separates fields hashed by SourceHash so that, for example,
// ("ab", "c") and ("a", "bc") do not collide.
const sourceHashSep = "\x00"

// Options bounds how many bytes of each field Derive keeps.
type Options struct {
	MaxTitleBytes   int
	MaxSummaryBytes int
	MaxBodyBytes    int
}

// DefaultOptions returns the byte caps used for FTS5 indexing.
func DefaultOptions() Options {
	return Options{
		MaxTitleBytes:   DefaultMaxTitleBytes,
		MaxSummaryBytes: DefaultMaxSummaryBytes,
		MaxBodyBytes:    DefaultMaxBodyBytes,
	}
}

// Text holds the derived, HTML-free representation of an item.
type Text struct {
	Title   string
	Summary string
	Body    string
}

// Derive strips HTML, decodes entities, collapses whitespace and truncates.
func Derive(title, summary, content string, opts Options) Text {
	return Text{
		Title:   truncate(stripHTML(title), opts.MaxTitleBytes),
		Summary: truncate(stripHTML(summary), opts.MaxSummaryBytes),
		Body:    truncate(stripHTML(content), opts.MaxBodyBytes),
	}
}

// SourceHash fingerprints the raw inputs so an unchanged item on re-fetch is
// a string comparison rather than an HTML parse.
func SourceHash(title, summary, content string) string {
	sum := sha256.Sum256([]byte(title + sourceHashSep + summary + sourceHashSep + content))
	return hex.EncodeToString(sum[:])[:sourceHashLength]
}

// stripHTML removes markup and collapses whitespace, leaving plain text.
// Streaming tokenization keeps this cheap over a whole corpus; goquery would
// build a document tree per item, which does not scale to a migration.
//
// script/style/noscript/template are HTML raw-text elements: the tokenizer's
// lexer state machine treats everything after their start tag as element
// content until a matching close tag, even without our own skip-depth
// bookkeeping. If the input is malformed and one of these is never closed,
// that "content" is the rest of the document, and skipDepth never returns to
// 0 -- every subsequent TextToken, including real prose, is dropped. Feed
// <description> fields are frequently truncated mid-document by publishers,
// so this is not a rare edge case. When that happens, re-run the strip with
// raw-text skipping disabled: the tokenizer still merges the unclosed
// element's tag-adjacent text into one big Text token, so a little script or
// CSS ends up in the index, but the trailing prose is not silently lost.
// Indexing a little JavaScript beats indexing nothing. Well-formed documents
// take the first pass and are unaffected.
func stripHTML(raw string) string {
	if text, ok := stripHTMLPass(raw, true); ok {
		return text
	}
	text, _ := stripHTMLPass(raw, false)
	return text
}

// stripHTMLPass tokenizes raw once. When skipRawText is true, text inside
// script/style/noscript/template elements is dropped. It returns false if
// the document ended with an unclosed skipped element (skipDepth > 0),
// signaling that stripHTML should retry without raw-text skipping.
func stripHTMLPass(raw string, skipRawText bool) (string, bool) {
	tokenizer := html.NewTokenizer(strings.NewReader(raw))
	var out strings.Builder
	skipDepth := 0
	for {
		switch tokenizer.Next() {
		case html.ErrorToken: // includes io.EOF
			return strings.Join(strings.Fields(out.String()), " "), skipDepth == 0
		case html.StartTagToken:
			if skipRawText {
				if name, _ := tokenizer.TagName(); isSkippedElement(name) {
					skipDepth++
				}
			}
		case html.EndTagToken:
			if skipRawText {
				if name, _ := tokenizer.TagName(); isSkippedElement(name) && skipDepth > 0 {
					skipDepth--
				}
			}
		case html.TextToken:
			if skipDepth == 0 {
				out.Write(tokenizer.Text()) // already entity-decoded
				out.WriteByte(' ')
			}
		case html.SelfClosingTagToken, html.CommentToken, html.DoctypeToken:
			// No text content to extract from these token types.
		}
	}
}

// isSkippedElement drops elements whose text content is markup, not prose.
func isSkippedElement(name []byte) bool {
	switch string(name) {
	case "script", "style", "noscript", "template":
		return true
	}
	return false
}

// truncate cuts s to at most max bytes, backing off to a UTF-8 rune boundary
// so a multi-byte character is never split.
func truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := s[:maxBytes]
	for cut != "" {
		r, size := utf8.DecodeLastRuneInString(cut)
		if r != utf8.RuneError || size != 1 {
			break
		}
		cut = cut[:len(cut)-1]
	}
	return cut
}
