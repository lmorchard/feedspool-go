package unfurl

import (
	"net/url"
	"strings"
	"testing"
)

// Shared test fixtures, hoisted so goconst stays quiet.
const (
	testImageURL    = "https://example.com/image.jpg"
	testPageURL     = "https://test.com/page"
	testDescription = "Test Description"
	testTitle       = "Test Title"
)

const testFaviconURL = "https://example.com/favicon.ico"

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
			href:     testImageURL,
			baseURL:  testPageURL,
			expected: testImageURL,
		},
		{
			name:     "relative URL becomes absolute",
			href:     "/image.jpg",
			baseURL:  testPageURL,
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

	if meta.FaviconURL != testFaviconURL {
		t.Errorf("FaviconURL = %v, want %v", meta.FaviconURL, testFaviconURL)
	}
}

func TestUnfurler_ToURLMetadata(t *testing.T) {
	unfurler := NewUnfurler(nil)

	result := &Result{
		Title:       testTitle,
		Description: testDescription,
		ImageURL:    testImageURL,
		FaviconURL:  testFaviconURL,
		Metadata:    map[string]interface{}{"og:type": "article"},
	}

	metadata, err := unfurler.ToURLMetadata("https://example.com", result, 200, nil)
	if err != nil {
		t.Fatalf("ToURLMetadata() error = %v", err)
	}

	if !metadata.Title.Valid || metadata.Title.String != testTitle {
		t.Errorf("Title not set correctly")
	}

	if !metadata.Description.Valid || metadata.Description.String != testDescription {
		t.Errorf("Description not set correctly")
	}

	if !metadata.ImageURL.Valid || metadata.ImageURL.String != testImageURL {
		t.Errorf("ImageURL not set correctly")
	}

	if !metadata.FaviconURL.Valid || metadata.FaviconURL.String != testFaviconURL {
		t.Errorf("FaviconURL not set correctly")
	}

	if !metadata.FetchStatusCode.Valid || metadata.FetchStatusCode.Int64 != 200 {
		t.Errorf("FetchStatusCode not set correctly")
	}
}

// Helper function to avoid import cycle.
func parseURL(rawURL string) (*url.URL, error) {
	return url.Parse(rawURL)
}
