package subscription

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/feedlist"
)

const (
	testURL1 = "https://example.com/feed.xml"
	testURL2 = "https://another.com/rss"
	testURL3 = "https://third.com/atom.xml"
)

func TestNew(t *testing.T) {
	cfg := &config.Config{}
	manager := New(cfg)

	if manager == nil {
		t.Fatal("New() returned nil")
	}

	if manager.config != cfg {
		t.Error("Manager should store the provided config")
	}
}

func TestResolveFormatAndFilename(t *testing.T) {
	tests := []struct {
		name           string
		config         *config.Config
		inputFormat    string
		inputFilename  string
		expectedFormat string
		expectedFile   string
		shouldError    bool
	}{
		{
			name: "Both provided",
			config: &config.Config{
				FeedList: config.FeedListConfig{Format: "opml", Filename: "default.opml"},
			},
			inputFormat:    "text",
			inputFilename:  "custom.txt",
			expectedFormat: "text",
			expectedFile:   "custom.txt",
			shouldError:    false,
		},
		{
			name: "Use defaults when empty",
			config: &config.Config{
				FeedList: config.FeedListConfig{Format: "opml", Filename: "default.opml"},
			},
			inputFormat:    "",
			inputFilename:  "",
			expectedFormat: "opml",
			expectedFile:   "default.opml",
			shouldError:    false,
		},
		{
			name: "Mix provided and default",
			config: &config.Config{
				FeedList: config.FeedListConfig{Format: "text", Filename: "default.txt"},
			},
			inputFormat:    "opml",
			inputFilename:  "",
			expectedFormat: "opml",
			expectedFile:   "default.txt",
			shouldError:    false,
		},
		{
			name:          "No defaults available",
			config:        &config.Config{FeedList: config.FeedListConfig{Format: "", Filename: ""}},
			inputFormat:   "",
			inputFilename: "",
			shouldError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := New(tt.config)
			format, filename, err := manager.ResolveFormatAndFilename(tt.inputFormat, tt.inputFilename)

			if tt.shouldError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if format != tt.expectedFormat {
				t.Errorf("Expected format %s, got %s", tt.expectedFormat, format)
			}

			if filename != tt.expectedFile {
				t.Errorf("Expected filename %s, got %s", tt.expectedFile, filename)
			}
		})
	}
}

func TestValidateFormat(t *testing.T) {
	manager := New(&config.Config{})

	tests := []struct {
		input       string
		expected    feedlist.Format
		shouldError bool
	}{
		{"opml", feedlist.FormatOPML, false},
		{"text", feedlist.FormatText, false},
		{"invalid", "", true},
		{"", "", true},
		{"OPML", "", true}, // Case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := manager.ValidateFormat(tt.input)

			if tt.shouldError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("Expected format %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestLoadOrCreateFeedList(t *testing.T) {
	manager := New(&config.Config{})
	tmpDir := t.TempDir()

	t.Run("Create new when file doesn't exist", func(t *testing.T) {
		filename := filepath.Join(tmpDir, "new_feeds.txt")
		list, createdNew := manager.LoadOrCreateFeedList(feedlist.FormatText, filename)

		if list == nil {
			t.Fatal("LoadOrCreateFeedList returned nil")
		}

		if !createdNew {
			t.Error("Expected createdNew to be true for non-existent file")
		}

		urls := list.GetURLs()
		if len(urls) != 0 {
			t.Errorf("Expected empty list, got %d URLs", len(urls))
		}
	})

	t.Run("Load existing file", func(t *testing.T) {
		filename := filepath.Join(tmpDir, "existing_feeds.txt")

		// Create a file with content
		existingList := feedlist.NewFeedList(feedlist.FormatText)
		existingList.AddURL(testURL1)
		existingList.Save(filename)

		list, createdNew := manager.LoadOrCreateFeedList(feedlist.FormatText, filename)

		if list == nil {
			t.Fatal("LoadOrCreateFeedList returned nil")
		}

		if createdNew {
			t.Error("Expected createdNew to be false for existing file")
		}

		urls := list.GetURLs()
		if len(urls) != 1 {
			t.Errorf("Expected 1 URL, got %d", len(urls))
		}

		if len(urls) > 0 && urls[0] != testURL1 {
			t.Errorf("Expected URL %s, got %s", testURL1, urls[0])
		}
	})
}

func TestSubscribe(t *testing.T) {
	tmpDir := t.TempDir()
	manager := New(&config.Config{})

	t.Run("Subscribe to new feed list", func(t *testing.T) {
		filename := filepath.Join(tmpDir, "subscribe_new.txt")
		urls := []string{testURL1, testURL2}

		result, err := manager.Subscribe("text", filename, urls)
		if err != nil {
			t.Errorf("Subscribe() error = %v", err)
			return
		}

		if result == nil {
			t.Fatal("Subscribe() returned nil result")
		}

		if !result.CreatedNew {
			t.Error("Expected CreatedNew to be true")
		}

		if result.AddedCount != 2 {
			t.Errorf("Expected AddedCount 2, got %d", result.AddedCount)
		}

		if result.TotalURLs != 2 {
			t.Errorf("Expected TotalURLs 2, got %d", result.TotalURLs)
		}

		if len(result.Warnings) != 0 {
			t.Errorf("Expected no warnings, got %d", len(result.Warnings))
		}

		// Verify file was created and contains URLs
		if _, err := os.Stat(filename); os.IsNotExist(err) {
			t.Error("Feed list file was not created")
		}
	})

	t.Run("Subscribe to existing feed list", func(t *testing.T) {
		filename := filepath.Join(tmpDir, "subscribe_existing.txt")

		// Create existing list
		existingList := feedlist.NewFeedList(feedlist.FormatText)
		existingList.AddURL(testURL1)
		existingList.Save(filename)

		urls := []string{testURL2, testURL3}
		result, err := manager.Subscribe("text", filename, urls)
		if err != nil {
			t.Errorf("Subscribe() error = %v", err)
			return
		}

		if result.CreatedNew {
			t.Error("Expected CreatedNew to be false for existing file")
		}

		if result.AddedCount != 2 {
			t.Errorf("Expected AddedCount 2, got %d", result.AddedCount)
		}
	})

	t.Run("Subscribe with duplicates", func(t *testing.T) {
		filename := filepath.Join(tmpDir, "subscribe_duplicates.txt")

		// Create existing list with one URL
		existingList := feedlist.NewFeedList(feedlist.FormatText)
		existingList.AddURL(testURL1)
		existingList.Save(filename)

		// Try to add the same URL plus a new one
		urls := []string{testURL1, testURL2}
		result, err := manager.Subscribe("text", filename, urls)
		if err != nil {
			t.Errorf("Subscribe() error = %v", err)
			return
		}

		if result.AddedCount != 1 {
			t.Errorf("Expected AddedCount 1 (only new URL), got %d", result.AddedCount)
		}

		if len(result.Warnings) != 1 {
			t.Errorf("Expected 1 warning for duplicate URL, got %d", len(result.Warnings))
		}
	})

	t.Run("Subscribe with invalid format", func(t *testing.T) {
		filename := filepath.Join(tmpDir, "invalid_format.txt")
		urls := []string{testURL1}

		_, err := manager.Subscribe("invalid", filename, urls)

		if err == nil {
			t.Error("Expected error for invalid format")
		}
	})
}

func TestUnsubscribe(t *testing.T) {
	tmpDir := t.TempDir()
	manager := New(&config.Config{})

	t.Run("Unsubscribe existing URL", func(t *testing.T) {
		filename := filepath.Join(tmpDir, "unsubscribe_existing.txt")

		// Create list with URLs
		list := feedlist.NewFeedList(feedlist.FormatText)
		list.AddURL(testURL1)
		list.AddURL(testURL2)
		list.Save(filename)

		result, err := manager.Unsubscribe("text", filename, testURL1)
		if err != nil {
			t.Errorf("Unsubscribe() error = %v", err)
			return
		}

		if result == nil {
			t.Fatal("Unsubscribe() returned nil result")
		}

		if !result.Found {
			t.Error("Expected Found to be true")
		}

		if !result.Removed {
			t.Error("Expected Removed to be true")
		}

		// Verify URL was actually removed
		loadedList, _ := feedlist.LoadFeedList(feedlist.FormatText, filename)
		urls := loadedList.GetURLs()
		if len(urls) != 1 {
			t.Errorf("Expected 1 URL remaining, got %d", len(urls))
		}

		if len(urls) > 0 && urls[0] != testURL2 {
			t.Errorf("Expected remaining URL %s, got %s", testURL2, urls[0])
		}
	})

	t.Run("Unsubscribe non-existent URL", func(t *testing.T) {
		filename := filepath.Join(tmpDir, "unsubscribe_missing.txt")

		// Create list with one URL
		list := feedlist.NewFeedList(feedlist.FormatText)
		list.AddURL(testURL1)
		list.Save(filename)

		result, err := manager.Unsubscribe("text", filename, testURL2)
		if err != nil {
			t.Errorf("Unsubscribe() error = %v", err)
			return
		}

		if result.Found {
			t.Error("Expected Found to be false for non-existent URL")
		}

		if result.Removed {
			t.Error("Expected Removed to be false for non-existent URL")
		}

		// Verify original URL is still there
		loadedList, _ := feedlist.LoadFeedList(feedlist.FormatText, filename)
		urls := loadedList.GetURLs()
		if len(urls) != 1 {
			t.Errorf("Expected 1 URL remaining, got %d", len(urls))
		}
	})

	t.Run("Unsubscribe from non-existent file", func(t *testing.T) {
		filename := filepath.Join(tmpDir, "non_existent.txt")

		_, err := manager.Unsubscribe("text", filename, testURL1)

		if err == nil {
			t.Error("Expected error for non-existent feed list")
		}
	})

	t.Run("Unsubscribe with invalid URL", func(t *testing.T) {
		filename := filepath.Join(tmpDir, "invalid_url.txt")

		_, err := manager.Unsubscribe("text", filename, "not-a-valid-url")

		if err == nil {
			t.Error("Expected error for invalid URL")
		}
	})

	t.Run("Unsubscribe with invalid format", func(t *testing.T) {
		filename := filepath.Join(tmpDir, "invalid_format.txt")

		_, err := manager.Unsubscribe("invalid", filename, testURL1)

		if err == nil {
			t.Error("Expected error for invalid format")
		}
	})
}

func TestDiscoverFeeds(t *testing.T) {
	manager := New(&config.Config{})

	// Create test server with HTML containing feed links
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		html := `
<!DOCTYPE html>
<html>
<head>
	<title>Test Blog</title>
	<link rel="alternate" type="application/rss+xml" title="RSS Feed" href="/feed.rss">
	<link rel="alternate" type="application/atom+xml" title="Atom Feed" href="/feed.atom">
	<link rel="alternate" type="application/rss+xml" title="Comments" href="https://external.com/comments.rss">
</head>
<body>
	<h1>Welcome to my blog</h1>
</body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	}))
	defer server.Close()

	t.Run("Discover feeds from HTML", func(t *testing.T) {
		feeds, err := manager.DiscoverFeeds(server.URL)
		if err != nil {
			t.Errorf("DiscoverFeeds() error = %v", err)
			return
		}

		if len(feeds) != 3 {
			t.Errorf("Expected 3 feeds, got %d", len(feeds))
		}

		expectedFeeds := []string{
			server.URL + "/feed.rss",
			server.URL + "/feed.atom",
			"https://external.com/comments.rss",
		}

		for _, expectedFeed := range expectedFeeds {
			found := false
			for _, actualFeed := range feeds {
				if actualFeed == expectedFeed {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected to find feed %s in discovered feeds", expectedFeed)
			}
		}
	})

	t.Run("Discover feeds from non-existent server", func(t *testing.T) {
		_, err := manager.DiscoverFeeds("http://non-existent-server-12345.com")

		if err == nil {
			t.Error("Expected error for non-existent server")
		}
	})

	t.Run("Discover feeds with HTTP error", func(t *testing.T) {
		errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer errorServer.Close()

		_, err := manager.DiscoverFeeds(errorServer.URL)

		if err == nil {
			t.Error("Expected error for HTTP 404")
		}
	})
}

func TestParseFeedLinks(t *testing.T) {
	manager := New(&config.Config{})

	tests := []struct {
		name     string
		html     string
		expected []string
	}{
		{
			name: "RSS and Atom feeds",
			html: `<link rel="alternate" type="application/rss+xml" href="/feed.rss">
				   <link rel="alternate" type="application/atom+xml" href="/feed.atom">`,
			expected: []string{"/feed.rss", "/feed.atom"},
		},
		{
			name: "Feeds with different attribute order",
			html: `<link href="/feed.rss" rel="alternate" type="application/rss+xml">
				   <link type="application/atom+xml" href="/feed.atom" rel="alternate">`,
			expected: []string{"/feed.rss", "/feed.atom"},
		},
		{
			name: "No feeds",
			html: `<link rel="stylesheet" type="text/css" href="/style.css">
				   <link rel="icon" type="image/png" href="/favicon.png">`,
			expected: []string{},
		},
		{
			name: "Duplicate feeds",
			html: `<link rel="alternate" type="application/rss+xml" href="/feed.rss">
				   <link rel="alternate" type="application/rss+xml" href="/feed.rss">`,
			expected: []string{"/feed.rss"},
		},
		{
			name: "Mixed case and quotes",
			html: `<link rel="alternate" type="application/RSS+xml" href='/feed.rss'>
				   <link rel='alternate' type='application/ATOM+XML' href="/feed.atom">`,
			expected: []string{"/feed.rss", "/feed.atom"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			feeds := manager.parseFeedLinks(tt.html)

			if len(feeds) != len(tt.expected) {
				t.Errorf("Expected %d feeds, got %d", len(tt.expected), len(feeds))
				return
			}

			for i, expected := range tt.expected {
				if i < len(feeds) && feeds[i] != expected {
					t.Errorf("Feed %d: expected %s, got %s", i, expected, feeds[i])
				}
			}
		})
	}
}

func TestAddURLsToList(t *testing.T) {
	manager := New(&config.Config{})

	t.Run("Add URLs to empty list", func(t *testing.T) {
		list := feedlist.NewFeedList(feedlist.FormatText)
		urls := []string{testURL1, testURL2}

		addedCount, warnings := manager.addURLsToList(list, urls)

		if addedCount != 2 {
			t.Errorf("Expected 2 URLs added, got %d", addedCount)
		}

		if len(warnings) != 0 {
			t.Errorf("Expected no warnings, got %d", len(warnings))
		}

		listURLs := list.GetURLs()
		if len(listURLs) != 2 {
			t.Errorf("Expected 2 URLs in list, got %d", len(listURLs))
		}
	})

	t.Run("Add URLs with some duplicates", func(t *testing.T) {
		list := feedlist.NewFeedList(feedlist.FormatText)
		list.AddURL(testURL1) // Pre-existing URL

		urls := []string{testURL1, testURL2, testURL3}

		addedCount, warnings := manager.addURLsToList(list, urls)

		if addedCount != 2 {
			t.Errorf("Expected 2 URLs added (excluding duplicate), got %d", addedCount)
		}

		if len(warnings) != 1 {
			t.Errorf("Expected 1 warning for duplicate, got %d", len(warnings))
		}

		if len(warnings) > 0 {
			expected := fmt.Sprintf("Feed URL already exists in list: %s", testURL1)
			if warnings[0] != expected {
				t.Errorf("Expected warning message '%s', got '%s'", expected, warnings[0])
			}
		}

		listURLs := list.GetURLs()
		if len(listURLs) != 3 {
			t.Errorf("Expected 3 URLs in list, got %d", len(listURLs))
		}
	})

	t.Run("Add empty URL list", func(t *testing.T) {
		list := feedlist.NewFeedList(feedlist.FormatText)
		urls := []string{}

		addedCount, warnings := manager.addURLsToList(list, urls)

		if addedCount != 0 {
			t.Errorf("Expected 0 URLs added, got %d", addedCount)
		}

		if len(warnings) != 0 {
			t.Errorf("Expected no warnings, got %d", len(warnings))
		}
	})
}
