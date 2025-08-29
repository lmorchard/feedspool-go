package opml

import (
	"encoding/xml"
	"fmt"
	"io"
)

type OPML struct {
	XMLName xml.Name `xml:"opml"`
	Head    Head     `xml:"head"`
	Body    Body     `xml:"body"`
}

type Head struct {
	Title string `xml:"title"`
}

type Body struct {
	Outlines []Outline `xml:"outline"`
}

type Outline struct {
	Text     string    `xml:"text,attr"`
	Title    string    `xml:"title,attr"`
	Type     string    `xml:"type,attr"`
	XMLURL   string    `xml:"xmlUrl,attr"`
	HTMLURL  string    `xml:"htmlUrl,attr"`
	Outlines []Outline `xml:"outline"`
}

func ParseOPML(reader io.Reader) (*OPML, error) {
	opml := &OPML{}
	decoder := xml.NewDecoder(reader)

	// Be more lenient with HTML entities
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose
	decoder.Entity = xml.HTMLEntity

	if err := decoder.Decode(opml); err != nil {
		return nil, fmt.Errorf("failed to parse OPML: %w", err)
	}
	return opml, nil
}

func ExtractFeedURLs(opml *OPML) []string {
	urls := []string{}
	extractFromOutlines(opml.Body.Outlines, &urls)
	return urls
}

func extractFromOutlines(outlines []Outline, urls *[]string) {
	for _, outline := range outlines {
		if outline.XMLURL != "" {
			*urls = append(*urls, outline.XMLURL)
		}
		if len(outline.Outlines) > 0 {
			extractFromOutlines(outline.Outlines, urls)
		}
	}
}
