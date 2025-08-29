package textlist

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseTextList(t *testing.T) {
	testContent := `# This is a comment
https://example.com/feed.xml
# Another comment

https://another.com/rss
# Final comment
https://third.com/atom.xml`

	reader := strings.NewReader(testContent)
	urls, err := ParseTextList(reader)
	if err != nil {
		t.Fatalf("ParseTextList() error = %v", err)
	}

	expectedURLs := []string{
		"https://example.com/feed.xml",
		"https://another.com/rss",
		"https://third.com/atom.xml",
	}

	if len(urls) != len(expectedURLs) {
		t.Errorf("len(urls) = %v, want %v", len(urls), len(expectedURLs))
	}

	for i, url := range urls {
		if i < len(expectedURLs) && url != expectedURLs[i] {
			t.Errorf("urls[%d] = %v, want %v", i, url, expectedURLs[i])
		}
	}
}

func TestParseTextListEmpty(t *testing.T) {
	reader := strings.NewReader("")
	urls, err := ParseTextList(reader)
	if err != nil {
		t.Fatalf("ParseTextList() error = %v", err)
	}

	if len(urls) != 0 {
		t.Errorf("len(urls) = %v, want %v", len(urls), 0)
	}
}

func TestParseTextListCommentsOnly(t *testing.T) {
	testContent := `# This is a comment
# Another comment
# Final comment`

	reader := strings.NewReader(testContent)
	urls, err := ParseTextList(reader)
	if err != nil {
		t.Fatalf("ParseTextList() error = %v", err)
	}

	if len(urls) != 0 {
		t.Errorf("len(urls) = %v, want %v", len(urls), 0)
	}
}

func TestParseTextListBlanksOnly(t *testing.T) {
	testContent := `

   

`

	reader := strings.NewReader(testContent)
	urls, err := ParseTextList(reader)
	if err != nil {
		t.Fatalf("ParseTextList() error = %v", err)
	}

	if len(urls) != 0 {
		t.Errorf("len(urls) = %v, want %v", len(urls), 0)
	}
}

func TestParseTextListInvalidURL(t *testing.T) {
	testContent := `https://example.com/feed.xml
not-a-valid-url
https://another.com/rss`

	reader := strings.NewReader(testContent)
	_, err := ParseTextList(reader)
	if err == nil {
		t.Error("ParseTextList() should have returned error for invalid URL")
	}

	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("Error should mention line 2, got: %v", err)
	}
}

func TestParseTextListMissingScheme(t *testing.T) {
	testContent := `https://example.com/feed.xml
example.com/feed
https://another.com/rss`

	reader := strings.NewReader(testContent)
	_, err := ParseTextList(reader)
	if err == nil {
		t.Error("ParseTextList() should have returned error for URL missing scheme")
	}

	if !strings.Contains(err.Error(), "URL missing scheme on line 2") {
		t.Errorf("Error should mention missing scheme on line 2, got: %v", err)
	}
}

func TestWriteTextList(t *testing.T) {
	urls := []string{
		"https://example.com/feed.xml",
		"https://another.com/rss",
		"https://third.com/atom.xml",
	}

	var buf bytes.Buffer
	err := WriteTextList(&buf, urls)
	if err != nil {
		t.Fatalf("WriteTextList() error = %v", err)
	}

	output := buf.String()

	// Check that all URLs are present
	for _, url := range urls {
		if !strings.Contains(output, url) {
			t.Errorf("Output should contain URL %s", url)
		}
	}

	// Check that header comment is present
	if !strings.Contains(output, "# Feed list generated on") {
		t.Error("Output should contain header comment")
	}

	// Check that each URL is on its own line
	lines := strings.Split(strings.TrimSpace(output), "\n")
	urlLines := make([]string, 0)
	for _, line := range lines {
		if !strings.HasPrefix(line, "#") && strings.TrimSpace(line) != "" {
			urlLines = append(urlLines, strings.TrimSpace(line))
		}
	}

	if len(urlLines) != len(urls) {
		t.Errorf("Expected %d URL lines, got %d", len(urls), len(urlLines))
	}

	for i, url := range urls {
		if i < len(urlLines) && urlLines[i] != url {
			t.Errorf("URL line %d = %v, want %v", i, urlLines[i], url)
		}
	}
}

func TestWriteTextListEmpty(t *testing.T) {
	urls := []string{}

	var buf bytes.Buffer
	err := WriteTextList(&buf, urls)
	if err != nil {
		t.Fatalf("WriteTextList() error = %v", err)
	}

	output := buf.String()

	// Should still have header comment
	if !strings.Contains(output, "# Feed list generated on") {
		t.Error("Output should contain header comment even for empty list")
	}

	// Should not have any non-comment lines
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, "#") && strings.TrimSpace(line) != "" {
			t.Errorf("Found unexpected non-comment line: %s", line)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	originalURLs := []string{
		"https://example.com/feed.xml",
		"https://another.com/rss",
		"https://third.com/atom.xml",
	}

	// Write to buffer
	var buf bytes.Buffer
	err := WriteTextList(&buf, originalURLs)
	if err != nil {
		t.Fatalf("WriteTextList() error = %v", err)
	}

	// Read back from buffer
	parsedURLs, err := ParseTextList(&buf)
	if err != nil {
		t.Fatalf("ParseTextList() error = %v", err)
	}

	// Compare
	if len(parsedURLs) != len(originalURLs) {
		t.Errorf("len(parsedURLs) = %v, want %v", len(parsedURLs), len(originalURLs))
	}

	for i, url := range parsedURLs {
		if i < len(originalURLs) && url != originalURLs[i] {
			t.Errorf("parsedURLs[%d] = %v, want %v", i, url, originalURLs[i])
		}
	}
}
