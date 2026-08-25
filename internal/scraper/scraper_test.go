package scraper

import (
	"strings"
	"testing"
)

const (
	testBaseURL      = "https://example.com"
	testBaseURLError = "base URL"
	testSelectorText = "selector"
)

func TestParseExtractsResolvesAndDeduplicatesLinks(t *testing.T) {
	const document = `
		<html><body>
			<article class="post"><a href="/posts/one"><span> First post </span></a></article>
			<article class="post"><a href="posts/two" title="Second post"></a></article>
			<article class="post"><a href="/posts/one">Duplicate title</a></article>
			<a href="/posts/three"><span class="post">Third post</span></a>
			<div class="post">No link here</div>
		</body></html>`

	items, err := Parse(strings.NewReader(document), "https://example.com/archive/", ".post")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := []ScrapedItem{
		{Title: "First post", Link: "https://example.com/posts/one"},
		{Title: "Second post", Link: "https://example.com/archive/posts/two"},
		{Title: "Third post", Link: "https://example.com/posts/three"},
	}
	if len(items) != len(want) {
		t.Fatalf("len(Parse()) = %d, want %d: %#v", len(items), len(want), items)
	}
	for i := range want {
		if items[i] != want[i] {
			t.Errorf("items[%d] = %#v, want %#v", i, items[i], want[i])
		}
	}
}

func TestParseUsesMatchedElementTextAsTitleFallback(t *testing.T) {
	const document = `<div class="post">Fallback heading <a href="/post"></a></div>`

	items, err := Parse(strings.NewReader(document), "https://example.com/", ".post")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(Parse()) = %d, want 1", len(items))
	}
	if items[0].Title != "Fallback heading" {
		t.Errorf("Title = %q, want %q", items[0].Title, "Fallback heading")
	}
}

func TestParseRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		selector string
		wantText string
	}{
		{name: testBaseURLError, baseURL: "://bad", selector: "a", wantText: testBaseURLError},
		{name: "relative base URL", baseURL: "/archive", selector: "a", wantText: "absolute"},
		{name: "empty selector", baseURL: testBaseURL, selector: "", wantText: testSelectorText},
		{name: "invalid selector", baseURL: testBaseURL, selector: "[", wantText: testSelectorText},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(`<a href="/post">Post</a>`), test.baseURL, test.selector)
			if err == nil {
				t.Fatal("Parse() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.wantText) {
				t.Errorf("Parse() error = %q, want it to contain %q", err, test.wantText)
			}
		})
	}
}

func TestParseReturnsEmptySliceWhenNothingMatches(t *testing.T) {
	items, err := Parse(strings.NewReader(`<a href="/post">Post</a>`), "https://example.com", ".missing")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if items == nil {
		t.Fatal("Parse() = nil, want empty non-nil slice")
	}
	if len(items) != 0 {
		t.Errorf("len(Parse()) = %d, want 0", len(items))
	}
}
