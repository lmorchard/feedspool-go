package textlist

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

// ParseTextList reads lines from the input and returns a slice of feed URLs.
// It ignores blank lines and comment lines starting with '#'.
// Each URL is validated for proper formatting.
func ParseTextList(reader io.Reader) ([]string, error) {
	var urls []string
	scanner := bufio.NewScanner(reader)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip blank lines
		if line == "" {
			continue
		}

		// Skip comment lines starting with '#'
		if strings.HasPrefix(line, "#") {
			continue
		}

		// Validate URL format
		if _, err := url.Parse(line); err != nil {
			return nil, fmt.Errorf("invalid URL on line %d: %s - %w", lineNum, line, err)
		}

		// Basic URL validation - must have scheme
		parsedURL, _ := url.Parse(line)
		if parsedURL.Scheme == "" {
			return nil, fmt.Errorf("URL missing scheme on line %d: %s", lineNum, line)
		}

		urls = append(urls, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading text list: %w", err)
	}

	return urls, nil
}

// WriteTextList writes URLs to the writer, one per line.
// It adds a header comment with timestamp.
func WriteTextList(writer io.Writer, urls []string) error {
	// Write header comment with timestamp
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	header := fmt.Sprintf("# Feed list generated on %s\n# One feed URL per line, comments start with #\n\n", timestamp)

	if _, err := writer.Write([]byte(header)); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	// Write URLs one per line
	for _, url := range urls {
		line := fmt.Sprintf("%s\n", url)
		if _, err := writer.Write([]byte(line)); err != nil {
			return fmt.Errorf("failed to write URL %s: %w", url, err)
		}
	}

	return nil
}
