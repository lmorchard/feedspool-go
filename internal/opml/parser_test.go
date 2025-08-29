package opml

import (
	"strings"
	"testing"
)

func TestParseOPML(t *testing.T) {
	opmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
    <head>
        <title>Test OPML</title>
    </head>
    <body>
        <outline text="Test Feed" type="rss" xmlUrl="https://example.com/feed.xml" />
        <outline text="Another Feed" type="rss" xmlUrl="https://another.com/rss" />
    </body>
</opml>`

	reader := strings.NewReader(opmlContent)
	opml, err := ParseOPML(reader)
	if err != nil {
		t.Fatalf("ParseOPML() error = %v", err)
	}

	if opml.Head.Title != "Test OPML" {
		t.Errorf("Head.Title = %v, want %v", opml.Head.Title, "Test OPML")
	}

	if len(opml.Body.Outlines) != 2 {
		t.Errorf("len(Body.Outlines) = %v, want %v", len(opml.Body.Outlines), 2)
	}

	urls := ExtractFeedURLs(opml)
	expectedURLs := []string{
		"https://example.com/feed.xml",
		"https://another.com/rss",
	}

	if len(urls) != len(expectedURLs) {
		t.Errorf("len(urls) = %v, want %v", len(urls), len(expectedURLs))
	}

	for i, url := range urls {
		if url != expectedURLs[i] {
			t.Errorf("urls[%d] = %v, want %v", i, url, expectedURLs[i])
		}
	}
}

func TestExtractFeedURLsEmpty(t *testing.T) {
	opml := &OPML{}
	urls := ExtractFeedURLs(opml)

	if len(urls) != 0 {
		t.Errorf("len(urls) = %v, want %v", len(urls), 0)
	}
}
