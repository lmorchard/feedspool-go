package feedlist

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testExistingUserAgent = "Existing Reader/1.0"
	testNewUserAgent      = "New Reader/2.0"
	testCategory          = "tech"
	testEnabled           = "true"
	testURL1              = "https://example.com/feed.xml"
	testURL2              = "https://another.com/rss"
	testURL3              = "https://third.com/atom.xml"
	testScrapeSelector    = ".post"
	testNestedSelector    = ".entry > h2"
)

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		filename string
		expected Format
	}{
		{"feeds.opml", FormatOPML},
		{"feeds.xml", FormatOPML},
		{"feeds.txt", FormatText},
		{"feeds.text", FormatText},
		{"feeds.unknown", FormatText}, // Default to text
		{"feeds", FormatText},         // No extension defaults to text
	}

	for _, tt := range tests {
		result := DetectFormat(tt.filename)
		if result != tt.expected {
			t.Errorf("DetectFormat(%s) = %v, want %v", tt.filename, result, tt.expected)
		}
	}
}

func TestNewFeedList(t *testing.T) {
	// Test OPML creation
	opmlList := NewFeedList(FormatOPML)
	if opmlList == nil {
		t.Fatal("NewFeedList(FormatOPML) returned nil")
	}

	urls := opmlList.GetURLs()
	if len(urls) != 0 {
		t.Errorf("New OPML feed list should be empty, got %d URLs", len(urls))
	}

	// Test Text creation
	textList := NewFeedList(FormatText)
	if textList == nil {
		t.Fatal("NewFeedList(FormatText) returned nil")
	}

	urls = textList.GetURLs()
	if len(urls) != 0 {
		t.Errorf("New text feed list should be empty, got %d URLs", len(urls))
	}

	// Test invalid format defaults to text
	invalidList := NewFeedList("invalid")
	if invalidList == nil {
		t.Fatal("NewFeedList with invalid format returned nil")
	}
}

func TestTextFeedListOperations(t *testing.T) {
	list := NewFeedList(FormatText)

	// Test adding URLs
	err := list.AddURL(testURL1)
	if err != nil {
		t.Errorf("AddURL() error = %v", err)
	}

	err = list.AddURL(testURL2)
	if err != nil {
		t.Errorf("AddURL() error = %v", err)
	}

	urls := list.GetURLs()
	if len(urls) != 2 {
		t.Errorf("Expected 2 URLs, got %d", len(urls))
	}

	// Test adding duplicate URL (should not error, just ignore)
	err = list.AddURL(testURL1)
	if err != nil {
		t.Errorf("AddURL() duplicate should not error, got %v", err)
	}

	urls = list.GetURLs()
	if len(urls) != 2 {
		t.Errorf("Expected 2 URLs after duplicate add, got %d", len(urls))
	}

	// Test removing URL
	err = list.RemoveURL(testURL1)
	if err != nil {
		t.Errorf("RemoveURL() error = %v", err)
	}

	urls = list.GetURLs()
	if len(urls) != 1 {
		t.Errorf("Expected 1 URL after removal, got %d", len(urls))
	}

	if urls[0] != testURL2 {
		t.Errorf("Expected remaining URL to be '%s', got %s", testURL2, urls[0])
	}

	// Test removing non-existent URL (should not error)
	err = list.RemoveURL("https://nonexistent.com/feed")
	if err != nil {
		t.Errorf("RemoveURL() non-existent should not error, got %v", err)
	}
}

func TestOPMLFeedListOperations(t *testing.T) {
	list := NewFeedList(FormatOPML)

	// Test adding URLs
	err := list.AddURL(testURL1)
	if err != nil {
		t.Errorf("AddURL() error = %v", err)
	}

	err = list.AddURL(testURL2)
	if err != nil {
		t.Errorf("AddURL() error = %v", err)
	}

	urls := list.GetURLs()
	if len(urls) != 2 {
		t.Errorf("Expected 2 URLs, got %d", len(urls))
	}

	// Test adding duplicate URL (should not error, just ignore)
	err = list.AddURL(testURL1)
	if err != nil {
		t.Errorf("AddURL() duplicate should not error, got %v", err)
	}

	urls = list.GetURLs()
	if len(urls) != 2 {
		t.Errorf("Expected 2 URLs after duplicate add, got %d", len(urls))
	}

	// Test removing URL
	err = list.RemoveURL(testURL1)
	if err != nil {
		t.Errorf("RemoveURL() error = %v", err)
	}

	urls = list.GetURLs()
	if len(urls) != 1 {
		t.Errorf("Expected 1 URL after removal, got %d", len(urls))
	}

	if urls[0] != testURL2 {
		t.Errorf("Expected remaining URL to be '%s', got %s", testURL2, urls[0])
	}
}

func TestTextFeedListSaveAndLoad(t *testing.T) {
	// Create temporary file
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_feeds.txt")

	// Create list with some URLs
	list := NewFeedList(FormatText)
	list.AddURL(testURL1)
	list.AddURL(testURL2)

	// Save to file
	err := list.Save(filename)
	if err != nil {
		t.Errorf("Save() error = %v", err)
	}

	// Load from file
	loadedList, err := LoadFeedList(FormatText, filename)
	if err != nil {
		t.Errorf("LoadFeedList() error = %v", err)
	}

	// Compare URLs
	originalURLs := list.GetURLs()
	loadedURLs := loadedList.GetURLs()

	if len(originalURLs) != len(loadedURLs) {
		t.Errorf("Expected %d URLs, got %d", len(originalURLs), len(loadedURLs))
	}

	for i, url := range originalURLs {
		if i < len(loadedURLs) && loadedURLs[i] != url {
			t.Errorf("URL %d: expected %s, got %s", i, url, loadedURLs[i])
		}
	}
}

func TestOPMLFeedListSaveAndLoad(t *testing.T) {
	// Create temporary file
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_feeds.opml")

	// Create list with some URLs
	list := NewFeedList(FormatOPML)
	list.AddURL(testURL1)
	list.AddURL(testURL2)

	// Save to file
	err := list.Save(filename)
	if err != nil {
		t.Errorf("Save() error = %v", err)
	}

	// Verify file was created and contains OPML
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Errorf("Failed to read saved file: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "<?xml version=\"1.0\"") {
		t.Error("Saved OPML file should contain XML declaration")
	}

	if !strings.Contains(contentStr, "<opml version=\"2.0\">") {
		t.Error("Saved OPML file should contain OPML declaration")
	}

	if !strings.Contains(contentStr, testURL1) {
		t.Error("Saved OPML file should contain first URL")
	}

	if !strings.Contains(contentStr, testURL2) {
		t.Error("Saved OPML file should contain second URL")
	}

	// Load from file
	loadedList, err := LoadFeedList(FormatOPML, filename)
	if err != nil {
		t.Errorf("LoadFeedList() error = %v", err)
	}

	// Compare URLs
	originalURLs := list.GetURLs()
	loadedURLs := loadedList.GetURLs()

	if len(originalURLs) != len(loadedURLs) {
		t.Errorf("Expected %d URLs, got %d", len(originalURLs), len(loadedURLs))
	}

	for i, url := range originalURLs {
		if i < len(loadedURLs) && loadedURLs[i] != url {
			t.Errorf("URL %d: expected %s, got %s", i, url, loadedURLs[i])
		}
	}
}

func TestOPMLFeedUserAgentRoundTrip(t *testing.T) {
	const source = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
    <head><title>Protected Feeds</title></head>
    <body>
        <outline text="Category">
            <outline text="Existing Feed" type="rss" xmlUrl="https://example.com/feed.xml" userAgent="Existing Reader/1.0" />
        </outline>
    </body>
</opml>`

	list, err := loadOPMLFeedList(strings.NewReader(source))
	if err != nil {
		t.Fatalf("loadOPMLFeedList() error = %v", err)
	}
	feeds := list.GetFeeds()
	if len(feeds) != 1 {
		t.Fatalf("len(GetFeeds()) = %d, want 1", len(feeds))
	}
	if got := feeds[0].UserAgent; got != testExistingUserAgent {
		t.Errorf("GetFeeds()[0].UserAgent = %q, want %q", got, testExistingUserAgent)
	}

	if err := list.AddFeed(Feed{
		URL:       testURL2,
		UserAgent: testNewUserAgent,
	}); err != nil {
		t.Fatalf("AddFeed() error = %v", err)
	}

	filename := filepath.Join(t.TempDir(), "feeds.opml")
	if err := list.Save(filename); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	reloaded, err := LoadFeedList(FormatOPML, filename)
	if err != nil {
		t.Fatalf("LoadFeedList() error = %v", err)
	}
	want := []Feed{
		{URL: testURL1, UserAgent: testExistingUserAgent, Type: FeedTypeRSS},
		{URL: testURL2, UserAgent: testNewUserAgent, Type: FeedTypeRSS},
	}
	got := reloaded.GetFeeds()
	if len(got) != len(want) {
		t.Fatalf("len(GetFeeds()) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("GetFeeds()[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestOPMLScrapeFeedRoundTrip(t *testing.T) {
	const source = `<opml version="2.0"><body><outline text="Articles" type="scrape" xmlUrl="https://example.com/archive" selector="article.card" /></body></opml>`

	list, err := loadOPMLFeedList(strings.NewReader(source))
	if err != nil {
		t.Fatalf("loadOPMLFeedList() error = %v", err)
	}
	feeds := list.GetFeeds()
	if len(feeds) != 1 {
		t.Fatalf("len(GetFeeds()) = %d, want 1", len(feeds))
	}
	want := Feed{URL: "https://example.com/archive", Type: FeedTypeScrape, ScrapeSelector: "article.card"}
	if feeds[0] != want {
		t.Errorf("GetFeeds()[0] = %#v, want %#v", feeds[0], want)
	}

	if err := list.AddFeed(Feed{
		URL:            "https://another.example/archive",
		Type:           FeedTypeScrape,
		ScrapeSelector: testNestedSelector,
	}); err != nil {
		t.Fatalf("AddFeed() error = %v", err)
	}

	filename := filepath.Join(t.TempDir(), "feeds.opml")
	if err := list.Save(filename); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := LoadFeedList(FormatOPML, filename)
	if err != nil {
		t.Fatalf("LoadFeedList() error = %v", err)
	}
	got := reloaded.GetFeeds()
	if len(got) != 2 {
		t.Fatalf("len(GetFeeds()) after round trip = %d, want 2", len(got))
	}
	if got[1].Type != FeedTypeScrape || got[1].ScrapeSelector != testNestedSelector {
		t.Errorf("round-tripped scrape config = %#v", got[1])
	}
}

func TestOPMLFeedListSetFeedTypeUpdatesNestedDuplicates(t *testing.T) {
	const source = `<opml version="2.0"><body><outline xmlUrl="https://example.com/feed.xml" /><outline text="Category"><outline xmlUrl="https://example.com/feed.xml" type="rss" /></outline></body></opml>`

	list, err := loadOPMLFeedList(strings.NewReader(source))
	if err != nil {
		t.Fatalf("loadOPMLFeedList() error = %v", err)
	}
	if found := list.SetFeedType(testURL1, FeedTypeScrape, testScrapeSelector); !found {
		t.Fatal("SetFeedType() = false, want true")
	}
	for i, feed := range list.GetFeeds() {
		if feed.Type != FeedTypeScrape || feed.ScrapeSelector != testScrapeSelector {
			t.Errorf("GetFeeds()[%d] = %#v, want scrape config", i, feed)
		}
	}

	if found := list.SetFeedType(testURL1, FeedTypeRSS, ""); !found {
		t.Fatal("SetFeedType() RSS = false, want true")
	}
	for i, feed := range list.GetFeeds() {
		if feed.Type != FeedTypeRSS || feed.ScrapeSelector != "" {
			t.Errorf("GetFeeds()[%d] = %#v, want RSS config", i, feed)
		}
	}
}

func TestDeduplicateFeedsRejectsConflictingParserConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		feeds []Feed
	}{
		{
			name: "type",
			feeds: []Feed{
				{URL: testURL1, Type: FeedTypeRSS},
				{URL: testURL1, Type: FeedTypeScrape, ScrapeSelector: testScrapeSelector},
			},
		},
		{
			name: "selector",
			feeds: []Feed{
				{URL: testURL1, Type: FeedTypeScrape, ScrapeSelector: testScrapeSelector},
				{URL: testURL1, Type: FeedTypeScrape, ScrapeSelector: ".entry"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DeduplicateFeeds(test.feeds); err == nil {
				t.Fatal("DeduplicateFeeds() error = nil, want conflict")
			}
		})
	}
}

func TestOPMLFeedListSetUserAgent(t *testing.T) {
	const source = `<opml version="2.0"><body><outline text="Category"><outline xmlUrl="https://example.com/feed.xml" userAgent="Old Reader/1.0" /></outline></body></opml>`

	list, err := loadOPMLFeedList(strings.NewReader(source))
	if err != nil {
		t.Fatalf("loadOPMLFeedList() error = %v", err)
	}
	if found := list.SetUserAgent(testURL1, testNewUserAgent); !found {
		t.Fatal("SetUserAgent() = false, want true for nested existing feed")
	}
	if got := list.GetFeeds()[0].UserAgent; got != testNewUserAgent {
		t.Errorf("updated UserAgent = %q, want %q", got, testNewUserAgent)
	}

	if found := list.SetUserAgent(testURL1, ""); !found {
		t.Fatal("SetUserAgent() = false, want true when clearing existing feed")
	}
	if got := list.GetFeeds()[0].UserAgent; got != "" {
		t.Errorf("cleared UserAgent = %q, want empty string", got)
	}

	if found := list.SetUserAgent("https://missing.example/feed.xml", "Reader/1.0"); found {
		t.Error("SetUserAgent() = true, want false for missing feed")
	}
}

func TestOPMLFeedListSetUserAgentUpdatesDuplicateEntries(t *testing.T) {
	const source = `<opml version="2.0"><body><outline xmlUrl="https://example.com/feed.xml" userAgent="First Reader/1.0" /><outline text="Category"><outline xmlUrl="https://example.com/feed.xml" userAgent="Second Reader/1.0" /></outline></body></opml>`

	list, err := loadOPMLFeedList(strings.NewReader(source))
	if err != nil {
		t.Fatalf("loadOPMLFeedList() error = %v", err)
	}
	if found := list.SetUserAgent(testURL1, testNewUserAgent); !found {
		t.Fatal("SetUserAgent() = false, want true")
	}
	feeds := list.GetFeeds()
	if len(feeds) != 2 {
		t.Fatalf("len(GetFeeds()) = %d, want 2", len(feeds))
	}
	for i, feed := range feeds {
		if feed.UserAgent != testNewUserAgent {
			t.Errorf("GetFeeds()[%d].UserAgent = %q, want %q", i, feed.UserAgent, testNewUserAgent)
		}
	}
}

func TestOPMLFeedListRemoveNestedURL(t *testing.T) {
	const source = `<opml version="2.0"><body><outline text="Category"><outline xmlUrl="https://example.com/feed.xml" /><outline xmlUrl="https://another.com/rss" /></outline></body></opml>`

	list, err := loadOPMLFeedList(strings.NewReader(source))
	if err != nil {
		t.Fatalf("loadOPMLFeedList() error = %v", err)
	}
	if err := list.RemoveURL(testURL1); err != nil {
		t.Fatalf("RemoveURL() error = %v", err)
	}
	urls := list.GetURLs()
	if len(urls) != 1 || urls[0] != testURL2 {
		t.Errorf("GetURLs() after nested removal = %v, want [%s]", urls, testURL2)
	}
	opmlList := list.(*OPMLFeedList)
	if len(opmlList.opml.Body.Outlines) != 1 || opmlList.opml.Body.Outlines[0].Text != "Category" {
		t.Errorf("category outline was not preserved: %#v", opmlList.opml.Body.Outlines)
	}
}

func TestOPMLUserAgentUpdatePreservesExtensionMetadata(t *testing.T) {
	const source = `<opml version="2.0" customRoot="root-value">
<head><title>Feeds</title><dateCreated>2026-08-25</dateCreated><ownerId scheme="opaque">owner-1</ownerId></head>
<body customBody="body-value"><outline text="Category" category="tech" customCategory="category-value"><outline xmlUrl="https://example.com/feed.xml" userAgent="Old Reader/1.0" customFeed="feed-value"><extension enabled="true">payload</extension></outline></outline></body>
</opml>`

	list, err := loadOPMLFeedList(strings.NewReader(source))
	if err != nil {
		t.Fatalf("loadOPMLFeedList() error = %v", err)
	}
	if found := list.SetUserAgent(testURL1, testNewUserAgent); !found {
		t.Fatal("SetUserAgent() = false, want true")
	}
	filename := filepath.Join(t.TempDir(), "feeds.opml")
	if err := list.Save(filename); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		CustomRoot string `xml:"customRoot,attr"`
		Head       struct {
			DateCreated string `xml:"dateCreated"`
			OwnerID     struct {
				Scheme string `xml:"scheme,attr"`
				Value  string `xml:",chardata"`
			} `xml:"ownerId"`
		} `xml:"head"`
		Body struct {
			CustomBody string `xml:"customBody,attr"`
			Category   struct {
				Category       string `xml:"category,attr"`
				CustomCategory string `xml:"customCategory,attr"`
				Feed           struct {
					CustomFeed string `xml:"customFeed,attr"`
					UserAgent  string `xml:"userAgent,attr"`
					Extension  struct {
						Enabled string `xml:"enabled,attr"`
						Value   string `xml:",chardata"`
					} `xml:"extension"`
				} `xml:"outline"`
			} `xml:"outline"`
		} `xml:"body"`
	}
	if err := xml.Unmarshal(data, &got); err != nil {
		t.Fatalf("xml.Unmarshal() error = %v", err)
	}
	if got.CustomRoot != "root-value" || got.Body.CustomBody != "body-value" {
		t.Errorf("root/body extension attributes were not preserved: %#v", got)
	}
	if got.Head.DateCreated != "2026-08-25" || got.Head.OwnerID.Scheme != "opaque" || got.Head.OwnerID.Value != "owner-1" {
		t.Errorf("head extension elements were not preserved: %#v", got.Head)
	}
	if got.Body.Category.Category != testCategory || got.Body.Category.CustomCategory != "category-value" {
		t.Errorf("category attributes were not preserved: %#v", got.Body.Category)
	}
	feed := got.Body.Category.Feed
	if feed.CustomFeed != "feed-value" || feed.UserAgent != testNewUserAgent ||
		feed.Extension.Enabled != testEnabled || feed.Extension.Value != "payload" {
		t.Errorf("feed extension metadata was not preserved: %#v", feed)
	}
}

func TestOPMLUserAgentUpdatePreservesNamespacedExtensions(t *testing.T) {
	const source = `<opml version="2.0" xmlns:ext="urn:example">
<head><title>Feeds</title><ext:metadata ext:kind="owner"><ext:child>head payload</ext:child></ext:metadata></head>
<body><outline xmlUrl="https://example.com/feed.xml" userAgent="Old Reader/1.0" ext:category="tech"><ext:data ext:enabled="true"><ext:child>feed payload</ext:child></ext:data></outline></body>
</opml>`

	list, err := loadOPMLFeedList(strings.NewReader(source))
	if err != nil {
		t.Fatalf("loadOPMLFeedList() error = %v", err)
	}
	if found := list.SetUserAgent(testURL1, testNewUserAgent); !found {
		t.Fatal("SetUserAgent() = false, want true")
	}
	filename := filepath.Join(t.TempDir(), "feeds.opml")
	if err := list.Save(filename); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Head struct {
			Meta struct {
				Kind  string `xml:"urn:example kind,attr"`
				Child string `xml:"urn:example child"`
			} `xml:"urn:example metadata"`
		} `xml:"head"`
		Body struct {
			Feed struct {
				Category  string `xml:"urn:example category,attr"`
				UserAgent string `xml:"userAgent,attr"`
				Data      struct {
					Enabled string `xml:"urn:example enabled,attr"`
					Child   string `xml:"urn:example child"`
				} `xml:"urn:example data"`
			} `xml:"outline"`
		} `xml:"body"`
	}
	if err := xml.Unmarshal(data, &got); err != nil {
		t.Fatalf("xml.Unmarshal() namespaced output error = %v\n%s", err, data)
	}
	if got.Head.Meta.Kind != "owner" || got.Head.Meta.Child != "head payload" {
		t.Errorf("namespaced head extension was not preserved: %#v\n%s", got.Head.Meta, data)
	}
	feed := got.Body.Feed
	if feed.Category != testCategory || feed.UserAgent != testNewUserAgent ||
		feed.Data.Enabled != testEnabled || feed.Data.Child != "feed payload" {
		t.Errorf("namespaced feed extension was not preserved: %#v\n%s", feed, data)
	}
}

func TestLoadNonExistentFile(t *testing.T) {
	_, err := LoadFeedList(FormatText, "/non/existent/file.txt")
	if err == nil {
		t.Error("LoadFeedList() should return error for non-existent file")
	}
}

func TestLoadInvalidFormat(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test.txt")

	// Create empty file
	os.WriteFile(filename, []byte(""), 0o644)

	_, err := LoadFeedList("invalid", filename)
	if err == nil {
		t.Error("LoadFeedList() should return error for invalid format")
	}
}

func TestOPMLFeedListTitle(t *testing.T) {
	const withTitle = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
    <head><title>  Tech Blogs  </title></head>
    <body><outline text="A" type="rss" xmlUrl="https://example.com/feed.xml" /></body>
</opml>`

	list, err := loadOPMLFeedList(strings.NewReader(withTitle))
	if err != nil {
		t.Fatalf("loadOPMLFeedList() error = %v", err)
	}
	if got := list.Title(); got != "Tech Blogs" {
		t.Errorf("Title() = %q, want %q", got, "Tech Blogs")
	}
}

func TestOPMLFeedListTitleAbsent(t *testing.T) {
	const noTitle = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
    <head></head>
    <body><outline text="A" type="rss" xmlUrl="https://example.com/feed.xml" /></body>
</opml>`

	list, err := loadOPMLFeedList(strings.NewReader(noTitle))
	if err != nil {
		t.Fatalf("loadOPMLFeedList() error = %v", err)
	}
	if got := list.Title(); got != "" {
		t.Errorf("Title() = %q, want empty string", got)
	}
}

func TestTextFeedListTitle(t *testing.T) {
	list, err := loadTextFeedList(strings.NewReader(testURL1 + "\n"))
	if err != nil {
		t.Fatalf("loadTextFeedList() error = %v", err)
	}
	if got := list.Title(); got != "" {
		t.Errorf("Title() = %q, want empty string", got)
	}
}

func TestDeduplicateFeedsRejectsConflictingUserAgents(t *testing.T) {
	feeds := []Feed{
		{URL: testURL1, UserAgent: "First Reader/1.0"},
		{URL: testURL2, UserAgent: "Other Reader/1.0"},
		{URL: testURL1, UserAgent: "Second Reader/2.0"},
	}

	_, err := DeduplicateFeeds(feeds)
	if err == nil {
		t.Fatal("DeduplicateFeeds() error = nil, want conflicting User-Agent error")
	}
	if !strings.Contains(err.Error(), testURL1) {
		t.Errorf("DeduplicateFeeds() error = %q, want conflicting URL", err)
	}
}

func TestDeduplicateFeedsPreservesFirstSeenOrder(t *testing.T) {
	feeds := []Feed{
		{URL: testURL1, UserAgent: testNewUserAgent},
		{URL: testURL2},
		{URL: testURL1, UserAgent: testNewUserAgent},
	}

	got, err := DeduplicateFeeds(feeds)
	if err != nil {
		t.Fatalf("DeduplicateFeeds() error = %v", err)
	}
	want := []Feed{
		{URL: testURL1, UserAgent: testNewUserAgent, Type: FeedTypeRSS},
		{URL: testURL2, Type: FeedTypeRSS},
	}
	if len(got) != len(want) {
		t.Fatalf("DeduplicateFeeds() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("DeduplicateFeeds()[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}
