package feedlist

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lmorchard/feedspool-go/internal/opml"
	"github.com/lmorchard/feedspool-go/internal/textlist"
)

// Format represents the supported feed list formats.
type Format string

const (
	FormatOPML Format = "opml"
	FormatText Format = "text"
)

// String returns the string representation of the format.
func (f Format) String() string {
	return string(f)
}

// FeedList interface provides unified access to different feed list formats.
type FeedList interface {
	GetURLs() []string
	AddURL(url string) error
	RemoveURL(url string) error
	Save(filename string) error
}

// OPMLFeedList wraps OPML functionality.
type OPMLFeedList struct {
	opml *opml.OPML
	urls []string
}

// TextFeedList uses the text parser.
type TextFeedList struct {
	urls []string
}

// LoadFeedList loads a feed list from a file based on the specified format.
func LoadFeedList(format Format, filename string) (FeedList, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open feed list file %s: %w", filename, err)
	}
	defer file.Close()

	switch format {
	case FormatOPML:
		return loadOPMLFeedList(file)
	case FormatText:
		return loadTextFeedList(file)
	default:
		return nil, fmt.Errorf("unsupported feed list format: %s", format)
	}
}

// NewFeedList creates a new empty feed list of the specified format.
func NewFeedList(format Format) FeedList {
	switch format {
	case FormatOPML:
		return &OPMLFeedList{
			opml: &opml.OPML{
				Head: opml.Head{Title: "Feed List"},
				Body: opml.Body{Outlines: []opml.Outline{}},
			},
			urls: []string{},
		}
	case FormatText:
		return &TextFeedList{
			urls: []string{},
		}
	default:
		// Default to text format if invalid format provided
		return &TextFeedList{
			urls: []string{},
		}
	}
}

// DetectFormat attempts to detect the format based on file extension.
func DetectFormat(filename string) Format {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".opml", ".xml":
		return FormatOPML
	case ".txt", ".text":
		return FormatText
	default:
		// Default to text format
		return FormatText
	}
}

// loadOPMLFeedList loads an OPML feed list from a reader.
func loadOPMLFeedList(reader io.Reader) (FeedList, error) {
	opmlData, err := opml.ParseOPML(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OPML: %w", err)
	}

	urls := opml.ExtractFeedURLs(opmlData)
	return &OPMLFeedList{
		opml: opmlData,
		urls: urls,
	}, nil
}

// loadTextFeedList loads a text feed list from a reader.
func loadTextFeedList(reader io.Reader) (FeedList, error) {
	urls, err := textlist.ParseTextList(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse text list: %w", err)
	}

	return &TextFeedList{
		urls: urls,
	}, nil
}

// OPMLFeedList methods.

// GetURLs returns all URLs in the OPML feed list.
func (ofl *OPMLFeedList) GetURLs() []string {
	return ofl.urls
}

// AddURL adds a URL to the OPML feed list.
func (ofl *OPMLFeedList) AddURL(url string) error {
	// Check if URL already exists
	for _, existingURL := range ofl.urls {
		if existingURL == url {
			return nil // URL already exists, no error
		}
	}

	// Add to URLs slice
	ofl.urls = append(ofl.urls, url)

	// Add to OPML structure
	outline := opml.Outline{
		Text:    url,
		Title:   url,
		Type:    "rss",
		XMLURL:  url,
		HTMLURL: "",
	}
	ofl.opml.Body.Outlines = append(ofl.opml.Body.Outlines, outline)

	return nil
}

// RemoveURL removes a URL from the OPML feed list.
func (ofl *OPMLFeedList) RemoveURL(url string) error {
	// Remove from URLs slice
	newURLs := make([]string, 0)
	for _, existingURL := range ofl.urls {
		if existingURL != url {
			newURLs = append(newURLs, existingURL)
		}
	}
	ofl.urls = newURLs

	// Remove from OPML structure
	newOutlines := make([]opml.Outline, 0)
	for _, outline := range ofl.opml.Body.Outlines {
		if outline.XMLURL != url {
			newOutlines = append(newOutlines, outline)
		}
	}
	ofl.opml.Body.Outlines = newOutlines

	return nil
}

// Save saves the OPML feed list to a file.
func (ofl *OPMLFeedList) Save(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create OPML file %s: %w", filename, err)
	}
	defer file.Close()

	// Write OPML XML header
	header := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
    <head>
        <title>` + ofl.opml.Head.Title + `</title>
    </head>
    <body>
`
	if _, err := file.WriteString(header); err != nil {
		return fmt.Errorf("failed to write OPML header: %w", err)
	}

	// Write outlines
	for _, outline := range ofl.opml.Body.Outlines {
		line := fmt.Sprintf(`        <outline text=%q type=%q xmlUrl=%q />%s`,
			outline.Text, outline.Type, outline.XMLURL, "\n")
		if _, err := file.WriteString(line); err != nil {
			return fmt.Errorf("failed to write OPML outline: %w", err)
		}
	}

	// Write OPML footer
	footer := `    </body>
</opml>
`
	if _, err := file.WriteString(footer); err != nil {
		return fmt.Errorf("failed to write OPML footer: %w", err)
	}

	return nil
}

// TextFeedList methods.

// GetURLs returns all URLs in the text feed list.
func (tfl *TextFeedList) GetURLs() []string {
	return tfl.urls
}

// AddURL adds a URL to the text feed list.
func (tfl *TextFeedList) AddURL(url string) error {
	// Check if URL already exists
	for _, existingURL := range tfl.urls {
		if existingURL == url {
			return nil // URL already exists, no error
		}
	}

	tfl.urls = append(tfl.urls, url)
	return nil
}

// RemoveURL removes a URL from the text feed list.
func (tfl *TextFeedList) RemoveURL(url string) error {
	newURLs := make([]string, 0)
	for _, existingURL := range tfl.urls {
		if existingURL != url {
			newURLs = append(newURLs, existingURL)
		}
	}
	tfl.urls = newURLs
	return nil
}

// Save saves the text feed list to a file.
func (tfl *TextFeedList) Save(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create text file %s: %w", filename, err)
	}
	defer file.Close()

	return textlist.WriteTextList(file, tfl.urls)
}
