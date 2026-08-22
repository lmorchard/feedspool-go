package config

import (
	"testing"
	"time"
)

// Shared test fixtures, hoisted so goconst stays quiet.
const (
	testFeedsTxt    = "feeds.txt"
	testMyFeedsOPML = "my-feeds.opml"
	testFormatOPML  = "opml"
	testFormatText  = "text"
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
				FeedList: FeedListConfig{Format: testFormatText, Filename: ""},
			},
			expected: false,
		},
		{
			name: "Filename only",
			config: Config{
				FeedList: FeedListConfig{Format: "", Filename: testFeedsTxt},
			},
			expected: false,
		},
		{
			name: "Both configured",
			config: Config{
				FeedList: FeedListConfig{Format: testFormatText, Filename: testFeedsTxt},
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
			Format:   testFormatOPML,
			Filename: testMyFeedsOPML,
		},
	}

	format, filename := config.GetDefaultFeedList()

	if format != testFormatOPML {
		t.Errorf("GetDefaultFeedList() format = %v, want %v", format, testFormatOPML)
	}

	if filename != testMyFeedsOPML {
		t.Errorf("GetDefaultFeedList() filename = %v, want %v", filename, testMyFeedsOPML)
	}
}
