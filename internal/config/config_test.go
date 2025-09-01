package config

import (
	"testing"
	"time"
)

func TestGetDefault(t *testing.T) {
	cfg := GetDefault()

	tests := []struct {
		name     string
		actual   interface{}
		expected interface{}
	}{
		{"Database", cfg.Database, "./feeds.db"},
		{"Timeout", cfg.Timeout, 30 * time.Second},
		{"Fetch.Concurrency", cfg.Fetch.Concurrency, 32},
		{"Fetch.MaxItems", cfg.Fetch.MaxItems, 100},
		{"Verbose", cfg.Verbose, false},
		{"Debug", cfg.Debug, false},
		{"JSON", cfg.JSON, false},
		{"FeedList.Format", cfg.FeedList.Format, ""},
		{"FeedList.Filename", cfg.FeedList.Filename, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.actual != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.actual, tt.expected)
			}
		})
	}
}

func TestHasDefaultFeedList(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected bool
	}{
		{
			name: "Both empty",
			config: Config{
				FeedList: FeedListConfig{Format: "", Filename: ""},
			},
			expected: false,
		},
		{
			name: "Format only",
			config: Config{
				FeedList: FeedListConfig{Format: "text", Filename: ""},
			},
			expected: false,
		},
		{
			name: "Filename only",
			config: Config{
				FeedList: FeedListConfig{Format: "", Filename: "feeds.txt"},
			},
			expected: false,
		},
		{
			name: "Both configured",
			config: Config{
				FeedList: FeedListConfig{Format: "text", Filename: "feeds.txt"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.config.HasDefaultFeedList()
			if actual != tt.expected {
				t.Errorf("HasDefaultFeedList() = %v, want %v", actual, tt.expected)
			}
		})
	}
}

func TestGetDefaultFeedList(t *testing.T) {
	config := Config{
		FeedList: FeedListConfig{
			Format:   "opml",
			Filename: "my-feeds.opml",
		},
	}

	format, filename := config.GetDefaultFeedList()

	if format != "opml" {
		t.Errorf("GetDefaultFeedList() format = %v, want %v", format, "opml")
	}

	if filename != "my-feeds.opml" {
		t.Errorf("GetDefaultFeedList() filename = %v, want %v", filename, "my-feeds.opml")
	}
}
