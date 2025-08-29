package feedlist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	err := list.AddURL("https://example.com/feed.xml")
	if err != nil {
		t.Errorf("AddURL() error = %v", err)
	}

	err = list.AddURL("https://another.com/rss")
	if err != nil {
		t.Errorf("AddURL() error = %v", err)
	}

	urls := list.GetURLs()
	if len(urls) != 2 {
		t.Errorf("Expected 2 URLs, got %d", len(urls))
	}

	// Test adding duplicate URL (should not error, just ignore)
	err = list.AddURL("https://example.com/feed.xml")
	if err != nil {
		t.Errorf("AddURL() duplicate should not error, got %v", err)
	}

	urls = list.GetURLs()
	if len(urls) != 2 {
		t.Errorf("Expected 2 URLs after duplicate add, got %d", len(urls))
	}

	// Test removing URL
	err = list.RemoveURL("https://example.com/feed.xml")
	if err != nil {
		t.Errorf("RemoveURL() error = %v", err)
	}

	urls = list.GetURLs()
	if len(urls) != 1 {
		t.Errorf("Expected 1 URL after removal, got %d", len(urls))
	}

	if urls[0] != "https://another.com/rss" {
		t.Errorf("Expected remaining URL to be 'https://another.com/rss', got %s", urls[0])
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
	err := list.AddURL("https://example.com/feed.xml")
	if err != nil {
		t.Errorf("AddURL() error = %v", err)
	}

	err = list.AddURL("https://another.com/rss")
	if err != nil {
		t.Errorf("AddURL() error = %v", err)
	}

	urls := list.GetURLs()
	if len(urls) != 2 {
		t.Errorf("Expected 2 URLs, got %d", len(urls))
	}

	// Test adding duplicate URL (should not error, just ignore)
	err = list.AddURL("https://example.com/feed.xml")
	if err != nil {
		t.Errorf("AddURL() duplicate should not error, got %v", err)
	}

	urls = list.GetURLs()
	if len(urls) != 2 {
		t.Errorf("Expected 2 URLs after duplicate add, got %d", len(urls))
	}

	// Test removing URL
	err = list.RemoveURL("https://example.com/feed.xml")
	if err != nil {
		t.Errorf("RemoveURL() error = %v", err)
	}

	urls = list.GetURLs()
	if len(urls) != 1 {
		t.Errorf("Expected 1 URL after removal, got %d", len(urls))
	}

	if urls[0] != "https://another.com/rss" {
		t.Errorf("Expected remaining URL to be 'https://another.com/rss', got %s", urls[0])
	}
}

func TestTextFeedListSaveAndLoad(t *testing.T) {
	// Create temporary file
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_feeds.txt")

	// Create list with some URLs
	list := NewFeedList(FormatText)
	list.AddURL("https://example.com/feed.xml")
	list.AddURL("https://another.com/rss")

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
	list.AddURL("https://example.com/feed.xml")
	list.AddURL("https://another.com/rss")

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

	if !strings.Contains(contentStr, "https://example.com/feed.xml") {
		t.Error("Saved OPML file should contain first URL")
	}

	if !strings.Contains(contentStr, "https://another.com/rss") {
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
	os.WriteFile(filename, []byte(""), 0644)

	_, err := LoadFeedList("invalid", filename)
	if err == nil {
		t.Error("LoadFeedList() should return error for invalid format")
	}
}