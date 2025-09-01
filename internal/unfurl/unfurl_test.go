package unfurl

import (
	"net/url"
	"strings"
	"testing"
)

func TestUnfurler_makeAbsoluteURL(t *testing.T) {
	unfurler := NewUnfurler(nil)
	
	tests := []struct {
		name     string
		href     string
		baseURL  string
		expected string
	}{
		{
			name:     "absolute URL stays absolute",
			href:     "https://example.com/image.jpg",
			baseURL:  "https://test.com/page",
			expected: "https://example.com/image.jpg",
		},
		{
			name:     "relative URL becomes absolute",
			href:     "/image.jpg",
			baseURL:  "https://test.com/page",
			expected: "https://test.com/image.jpg",
		},
		{
			name:     "relative path becomes absolute",
			href:     "image.jpg",
			baseURL:  "https://test.com/folder/page",
			expected: "https://test.com/folder/image.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, _ := parseURL(tt.baseURL)
			result := unfurler.makeAbsoluteURL(tt.href, base)
			if result != tt.expected {
				t.Errorf("makeAbsoluteURL() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestUnfurler_parseHTMLMetadata(t *testing.T) {
	unfurler := NewUnfurler(nil)
	
	html := `
	<html>
	<head>
		<title>Test Page</title>
		<meta name="description" content="Test description">
		<link rel="icon" href="/favicon.ico">
	</head>
	</html>
	`
	
	baseURL, _ := parseURL("https://example.com/page")
	meta := unfurler.parseHTMLMetadata(strings.NewReader(html), baseURL)
	
	if meta.Title != "Test Page" {
		t.Errorf("Title = %v, want %v", meta.Title, "Test Page")
	}
	
	if meta.Description != "Test description" {
		t.Errorf("Description = %v, want %v", meta.Description, "Test description")
	}
	
	expectedFavicon := "https://example.com/favicon.ico"
	if meta.FaviconURL != expectedFavicon {
		t.Errorf("FaviconURL = %v, want %v", meta.FaviconURL, expectedFavicon)
	}
}

func TestUnfurler_ToURLMetadata(t *testing.T) {
	unfurler := NewUnfurler(nil)
	
	result := &Result{
		Title:       "Test Title",
		Description: "Test Description",
		ImageURL:    "https://example.com/image.jpg",
		FaviconURL:  "https://example.com/favicon.ico",
		Metadata:    map[string]interface{}{"og:type": "article"},
	}
	
	metadata, err := unfurler.ToURLMetadata("https://example.com", result, 200, nil)
	if err != nil {
		t.Fatalf("ToURLMetadata() error = %v", err)
	}
	
	if !metadata.Title.Valid || metadata.Title.String != "Test Title" {
		t.Errorf("Title not set correctly")
	}
	
	if !metadata.Description.Valid || metadata.Description.String != "Test Description" {
		t.Errorf("Description not set correctly")
	}
	
	if !metadata.ImageURL.Valid || metadata.ImageURL.String != "https://example.com/image.jpg" {
		t.Errorf("ImageURL not set correctly")
	}
	
	if !metadata.FaviconURL.Valid || metadata.FaviconURL.String != "https://example.com/favicon.ico" {
		t.Errorf("FaviconURL not set correctly")
	}
	
	if !metadata.FetchStatusCode.Valid || metadata.FetchStatusCode.Int64 != 200 {
		t.Errorf("FetchStatusCode not set correctly")
	}
}

// Helper function to avoid import cycle
func parseURL(rawURL string) (*url.URL, error) {
	return url.Parse(rawURL)
}