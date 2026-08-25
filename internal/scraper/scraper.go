// Package scraper extracts feed-shaped entries from HTML pages.
//
// Portions adapted from github.com/Hyaxia/blogwatcher (MIT).
package scraper

import (
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/cascadia"
)

// ScrapedItem is the link metadata synthesized from one HTML match.
type ScrapedItem struct {
	Title string
	Link  string
}

// Parse applies selector to body and returns deduplicated, resolved links.
func Parse(body io.Reader, baseURL, selector string) ([]ScrapedItem, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL %q: %w", baseURL, err)
	}
	if !base.IsAbs() {
		return nil, fmt.Errorf("invalid base URL %q: URL must be absolute", baseURL)
	}

	selector = strings.TrimSpace(selector)
	if selector == "" {
		return nil, fmt.Errorf("scrape selector cannot be empty")
	}
	matcher, err := cascadia.Compile(selector)
	if err != nil {
		return nil, fmt.Errorf("invalid scrape selector %q: %w", selector, err)
	}

	document, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	items := make([]ScrapedItem, 0)
	seen := make(map[string]struct{})
	document.FindMatcher(matcher).Each(func(_ int, match *goquery.Selection) {
		link := nearestLink(match)
		if link.Length() == 0 {
			return
		}

		href, found := link.Attr("href")
		if !found || strings.TrimSpace(href) == "" {
			return
		}
		reference, parseErr := url.Parse(strings.TrimSpace(href))
		if parseErr != nil {
			return
		}
		resolved := base.ResolveReference(reference).String()
		if _, duplicate := seen[resolved]; duplicate {
			return
		}
		seen[resolved] = struct{}{}

		items = append(items, ScrapedItem{
			Title: extractTitle(link, match),
			Link:  resolved,
		})
	})

	return items, nil
}

func nearestLink(match *goquery.Selection) *goquery.Selection {
	if goquery.NodeName(match) == "a" {
		return match.First()
	}
	if descendant := match.Find("a").First(); descendant.Length() > 0 {
		return descendant
	}
	return match.ParentsFiltered("a").First()
}

func extractTitle(link, match *goquery.Selection) string {
	if title := normalizedText(link.Text()); title != "" {
		return title
	}
	if title, found := link.Attr("title"); found {
		if title = normalizedText(title); title != "" {
			return title
		}
	}
	return normalizedText(match.Text())
}

func normalizedText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
