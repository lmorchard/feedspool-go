package sitegroup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lmorchard/feedspool-go/internal/feedlist"
)

const (
	feedA = "https://a.example.com/feed.xml"
	feedB = "https://b.example.com/feed.xml"
	feedC = "https://c.example.com/feed.xml"

	techBlogsOPML      = "tech-blogs.opml"
	techBlogsSpaceOPML = "tech blogs.opml"
	scratchSlug        = "scratch"
	badOPML            = "bad.opml"
)

// opmlWith builds a minimal OPML document with the given title and feed URLs.
func opmlWith(title string, urls ...string) string {
	doc := `<?xml version="1.0" encoding="UTF-8"?>` + "\n<opml version=\"2.0\">\n<head><title>" +
		title + "</title></head>\n<body>\n"
	for _, u := range urls {
		doc += `<outline text="x" type="rss" xmlUrl="` + u + `" />` + "\n"
	}
	return doc + "</body>\n</opml>\n"
}

// writeDir creates a temp directory containing the given name->content files.
func writeDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDiscoverMixedFormats(t *testing.T) {
	dir := writeDir(t, map[string]string{
		techBlogsOPML: opmlWith("Tech Blogs", feedA, feedB),
		"scratch.txt": feedC + "\n",
		"README.md":   "ignore me",
	})
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o750); err != nil {
		t.Fatal(err)
	}

	sites, skipped, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("len(skipped) = %d, want 0", len(skipped))
	}
	if len(sites) != 2 {
		t.Fatalf("len(sites) = %d, want 2 (README.md and subdir must be ignored)", len(sites))
	}

	// Sorted by filename: scratch.txt before tech-blogs.opml.
	if sites[0].Slug != scratchSlug {
		t.Errorf("sites[0].Slug = %q, want %q", sites[0].Slug, scratchSlug)
	}
	if sites[0].Title != scratchSlug {
		t.Errorf("sites[0].Title = %q, want %q (text lists fall back to filename)", sites[0].Title, scratchSlug)
	}
	if sites[0].Format != feedlist.FormatText {
		t.Errorf("sites[0].Format = %v, want %v", sites[0].Format, feedlist.FormatText)
	}

	if sites[1].Slug != "tech-blogs" {
		t.Errorf("sites[1].Slug = %q, want %q", sites[1].Slug, "tech-blogs")
	}
	if sites[1].Title != "Tech Blogs" {
		t.Errorf("sites[1].Title = %q, want %q", sites[1].Title, "Tech Blogs")
	}
	if len(sites[1].URLs) != 2 {
		t.Errorf("len(sites[1].URLs) = %d, want 2", len(sites[1].URLs))
	}
}

func TestDiscoverSlugifiesFilename(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"Tech Blogs & Friends.opml": opmlWith("", feedA),
	})

	sites, _, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if sites[0].Slug != "tech-blogs-friends" {
		t.Errorf("Slug = %q, want %q", sites[0].Slug, "tech-blogs-friends")
	}
	if sites[0].Title != "Tech Blogs & Friends" {
		t.Errorf("Title = %q, want the verbatim filename base", sites[0].Title)
	}
}

func TestDiscoverSlugCollision(t *testing.T) {
	dir := writeDir(t, map[string]string{
		techBlogsSpaceOPML: opmlWith("A", feedA),
		techBlogsOPML:      opmlWith("B", feedB),
	})

	_, _, err := Discover(dir)
	if err == nil {
		t.Fatal("Discover() error = nil, want a slug collision error")
	}
	for _, want := range []string{techBlogsSpaceOPML, techBlogsOPML} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err.Error(), want)
		}
	}
}

func TestDiscoverEmptyDirectory(t *testing.T) {
	dir := writeDir(t, map[string]string{"notes.md": "hi"})

	_, _, err := Discover(dir)
	if err == nil {
		t.Fatal("Discover() error = nil, want an error for a directory with no feed lists")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not name the directory", err.Error())
	}
}

func TestDiscoverMissingDirectory(t *testing.T) {
	_, _, err := Discover(filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("Discover() error = nil, want an error for a missing directory")
	}
}

func TestDiscoverPathIsFile(t *testing.T) {
	dir := writeDir(t, map[string]string{"a.opml": opmlWith("A", feedA)})

	_, _, err := Discover(filepath.Join(dir, "a.opml"))
	if err == nil {
		t.Fatal("Discover() error = nil, want an error when the path is a file")
	}
}

func TestDiscoverSkipsMalformedFile(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"good.opml": opmlWith("Good", feedA),
		badOPML:     "<opml><head><title>unclosed",
	})

	sites, skipped, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover() error = %v, want nil (a bad file must not fail the run)", err)
	}
	if len(sites) != 1 || sites[0].Slug != "good" {
		t.Fatalf("sites = %+v, want only the good site", sites)
	}
	if len(skipped) != 1 {
		t.Fatalf("len(skipped) = %d, want 1", len(skipped))
	}
	if filepath.Base(skipped[0].Path) != badOPML {
		t.Errorf("skipped[0].Path = %q, want %s", skipped[0].Path, badOPML)
	}
	if skipped[0].Err == nil {
		t.Error("skipped[0].Err = nil, want the parse error")
	}
}

func TestDiscoverEmptyFeedListIsStillASite(t *testing.T) {
	dir := writeDir(t, map[string]string{"empty.opml": opmlWith("Empty")})

	sites, _, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(sites) != 1 {
		t.Fatalf("len(sites) = %d, want 1", len(sites))
	}
	if len(sites[0].URLs) != 0 {
		t.Errorf("len(URLs) = %d, want 0", len(sites[0].URLs))
	}
}

func TestUnionURLs(t *testing.T) {
	sites := []Site{
		{Slug: "one", URLs: []string{feedA, feedB}},
		{Slug: "two", URLs: []string{feedB, feedC}},
		{Slug: "three", URLs: []string{feedA}},
	}

	got := UnionURLs(sites)
	want := []string{feedA, feedB, feedC}

	if len(got) != len(want) {
		t.Fatalf("UnionURLs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("UnionURLs()[%d] = %q, want %q (first-seen order)", i, got[i], want[i])
		}
	}
}

func TestUnionURLsEmpty(t *testing.T) {
	if got := UnionURLs(nil); len(got) != 0 {
		t.Errorf("UnionURLs(nil) = %v, want empty", got)
	}
}
