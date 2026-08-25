package opml

import (
	"encoding/xml"
	"fmt"
	"io"
)

const xmlnsNamespace = "xmlns"

type OPML struct {
	XMLName    xml.Name   `xml:"opml"`
	Version    string     `xml:"version,attr,omitempty"`
	OtherAttrs []xml.Attr `xml:",any,attr"`
	Head       Head       `xml:"head"`
	Body       Body       `xml:"body"`
}

type Head struct {
	Title         string       `xml:"title"`
	OtherElements []RawElement `xml:",any"`
}

type Body struct {
	OtherAttrs    []xml.Attr   `xml:",any,attr"`
	Outlines      []Outline    `xml:"outline"`
	OtherElements []RawElement `xml:",any"`
}

type Outline struct {
	Text          string       `xml:"text,attr,omitempty"`
	Title         string       `xml:"title,attr,omitempty"`
	Type          string       `xml:"type,attr,omitempty"`
	XMLURL        string       `xml:"xmlUrl,attr,omitempty"`
	HTMLURL       string       `xml:"htmlUrl,attr,omitempty"`
	UserAgent     string       `xml:"userAgent,attr,omitempty"`
	OtherAttrs    []xml.Attr   `xml:",any,attr"`
	Outlines      []Outline    `xml:"outline"`
	OtherElements []RawElement `xml:",any"`
}

// RawElement preserves OPML extension elements that feedspool does not interpret.
type RawElement struct {
	XMLName xml.Name
	Attrs   []xml.Attr  `xml:",any,attr"`
	Tokens  []xml.Token `xml:"-"`
}

// UnmarshalXML stores namespace-aware tokens so extension elements can be
// written back without interpreting their contents.
func (element *RawElement) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	element.XMLName = start.Name
	element.Attrs = extensionAttrs(start.Attr)
	element.Tokens = nil

	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return err
		}

		switch value := token.(type) {
		case xml.StartElement:
			depth++
			value.Attr = extensionAttrs(value.Attr)
			element.Tokens = append(element.Tokens, xml.CopyToken(value))
		case xml.EndElement:
			depth--
			if depth > 0 {
				element.Tokens = append(element.Tokens, xml.CopyToken(value))
			}
		default:
			element.Tokens = append(element.Tokens, xml.CopyToken(token))
		}
	}

	return nil
}

// MarshalXML restores the captured extension element and its inner tokens.
func (element *RawElement) MarshalXML(encoder *xml.Encoder, _ xml.StartElement) error {
	start := xml.StartElement{Name: element.XMLName, Attr: extensionAttrs(element.Attrs)}
	if err := encoder.EncodeToken(start); err != nil {
		return err
	}
	for _, token := range element.Tokens {
		if err := encoder.EncodeToken(token); err != nil {
			return err
		}
	}
	return encoder.EncodeToken(start.End())
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
	removeNamespaceDeclarations(opml)
	return opml, nil
}

func removeNamespaceDeclarations(document *OPML) {
	document.OtherAttrs = extensionAttrs(document.OtherAttrs)
	document.Body.OtherAttrs = extensionAttrs(document.Body.OtherAttrs)
	removeOutlineNamespaceDeclarations(document.Body.Outlines)
}

func removeOutlineNamespaceDeclarations(outlines []Outline) {
	for i := range outlines {
		outlines[i].OtherAttrs = extensionAttrs(outlines[i].OtherAttrs)
		removeOutlineNamespaceDeclarations(outlines[i].Outlines)
	}
}

func extensionAttrs(attrs []xml.Attr) []xml.Attr {
	kept := make([]xml.Attr, 0, len(attrs))
	for _, attr := range attrs {
		if attr.Name.Space == xmlnsNamespace || (attr.Name.Space == "" && attr.Name.Local == xmlnsNamespace) {
			continue
		}
		kept = append(kept, attr)
	}
	return kept
}

func ExtractFeedURLs(opml *OPML) []string {
	urls := []string{}
	extractFromOutlines(opml.Body.Outlines, &urls)
	return urls
}

func extractFromOutlines(outlines []Outline, urls *[]string) {
	for i := range outlines {
		outline := &outlines[i]
		if outline.XMLURL != "" {
			*urls = append(*urls, outline.XMLURL)
		}
		if len(outline.Outlines) > 0 {
			extractFromOutlines(outline.Outlines, urls)
		}
	}
}
