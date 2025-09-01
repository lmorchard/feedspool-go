package fetcher

import (
	"encoding/json"
	"fmt"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/feedlist"
)

// FetchSummary contains statistics about a fetch operation.
type FetchSummary struct {
	Mode         string `json:"mode"`
	TotalFeeds   int    `json:"totalFeeds"`
	Successful   int    `json:"successful"`
	Errors       int    `json:"errors"`
	Cached       int    `json:"cached"`
	TotalItems   int    `json:"totalItems"`
	RemovedFeeds int    `json:"removedFeeds,omitempty"`
}

// ProcessResults analyzes fetch results and returns summary statistics.
func ProcessResults(results []*FetchResult) FetchSummary {
	summary := FetchSummary{
		TotalFeeds: len(results),
	}

	for _, result := range results {
		if result.Error != nil {
			summary.Errors++
		} else if result.Cached {
			summary.Cached++
		} else {
			summary.Successful++
			summary.TotalItems += result.ItemCount
		}
	}

	return summary
}

// PrintSummary outputs the fetch summary in the appropriate format.
func (s FetchSummary) Print(cfg *config.Config) {
	if cfg.JSON {
		s.printJSON()
	} else {
		s.printText()
	}
}

// printJSON outputs the summary as JSON.
func (s FetchSummary) printJSON() {
	jsonData, _ := json.Marshal(s)
	//nolint:forbidigo // Required for command output
	fmt.Println(string(jsonData))
}

// printText outputs the summary as human-readable text.
func (s FetchSummary) printText() {
	//nolint:forbidigo // Required for command output
	fmt.Printf("\nSummary:\n")
	//nolint:forbidigo // Required for command output
	fmt.Printf("  Total feeds: %d\n", s.TotalFeeds)
	//nolint:forbidigo // Required for command output
	fmt.Printf("  Successful: %d\n", s.Successful)
	//nolint:forbidigo // Required for command output
	fmt.Printf("  Cached/Skipped: %d\n", s.Cached)
	//nolint:forbidigo // Required for command output
	fmt.Printf("  Errors: %d\n", s.Errors)
	//nolint:forbidigo // Required for command output
	fmt.Printf("  Total items: %d\n", s.TotalItems)
	if s.RemovedFeeds > 0 {
		//nolint:forbidigo // Required for command output
		fmt.Printf("  Removed feeds: %d\n", s.RemovedFeeds)
	}
}

// SingleURLOutput contains output data for single URL operations.
type SingleURLOutput struct {
	Mode        string `json:"mode"`
	URL         string `json:"url"`
	Cached      bool   `json:"cached"`
	Items       int    `json:"items"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Error       string `json:"error,omitempty"`
}

// PrintSingleResult outputs the result of a single URL fetch.
//
//nolint:nestif // Simple conditional output logic.
func PrintSingleResult(result *FetchResult, cfg *config.Config) {
	if cfg.JSON {
		output := SingleURLOutput{
			Mode:   "single",
			URL:    result.URL,
			Cached: result.Cached,
			Items:  result.ItemCount,
		}

		if result.Error != nil {
			output.Error = result.Error.Error()
		} else if result.Feed != nil {
			output.Title = result.Feed.Title
			output.Description = result.Feed.Description
		}

		jsonData, _ := json.Marshal(output)
		//nolint:forbidigo // Required for command output
		fmt.Println(string(jsonData))
	} else {
		if result.Error != nil {
			//nolint:forbidigo // Required for command output
			fmt.Printf("Failed to fetch feed: %v\n", result.Error)
		} else if result.Cached {
			//nolint:forbidigo // Required for command output
			fmt.Printf("Feed not modified: %s\n", result.URL)
		} else if result.Feed != nil {
			//nolint:forbidigo // Required for command output
			fmt.Printf("Feed fetched successfully: %s\n", result.Feed.Title)
			//nolint:forbidigo // Required for command output
			fmt.Printf("  URL: %s\n", result.URL)
			//nolint:forbidigo // Required for command output
			fmt.Printf("  Items: %d\n", result.ItemCount)
		}
	}
}

// FormatValidation handles feed format validation and parsing.
type FormatValidation struct{}

// ValidateFormat validates a feed list format string.
func (f FormatValidation) ValidateFormat(format string) (feedlist.Format, error) {
	switch format {
	case string(feedlist.FormatOPML):
		return feedlist.FormatOPML, nil
	case string(feedlist.FormatText):
		return feedlist.FormatText, nil
	default:
		return "", fmt.Errorf("unsupported format: %s (must be 'opml' or 'text')", format)
	}
}

// DetermineFormatAndFilename determines format and filename from flags and config.
//
//nolint:nestif // Configuration fallback logic.
func (f FormatValidation) DetermineFormatAndFilename(
	cfg *config.Config, format, filename string,
) (resultFormat, resultFilename string, err error) {
	if format == "" || filename == "" {
		if cfg.HasDefaultFeedList() {
			if format == "" {
				format, _ = cfg.GetDefaultFeedList()
			}
			if filename == "" {
				_, filename = cfg.GetDefaultFeedList()
			}
		} else {
			return "", "", fmt.Errorf("feed list format and filename must be specified " +
				"(use --format and --filename flags or configure defaults)")
		}
	}
	return format, filename, nil
}
