package unfurl

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lmorchard/feedspool-go/internal/httpclient"
	"github.com/sirupsen/logrus"
)

// RobotsChecker handles robots.txt checking with caching.
type RobotsChecker struct {
	client    *httpclient.Client
	cache     map[string]*robotsEntry
	cacheMu   sync.RWMutex
	userAgent string
	cacheTTL  time.Duration
}

type robotsEntry struct {
	rules     *robotsRules
	fetchedAt time.Time
}

type robotsRules struct {
	allowedPaths    []string
	disallowedPaths []string
	crawlDelay      time.Duration
}

// NewRobotsChecker creates a new robots.txt checker.
func NewRobotsChecker(client *httpclient.Client, userAgent string) *RobotsChecker {
	if userAgent == "" {
		userAgent = "feedspool"
	}
	return &RobotsChecker{
		client:    client,
		cache:     make(map[string]*robotsEntry),
		userAgent: userAgent,
		cacheTTL:  1 * time.Hour,
	}
}

// IsAllowed checks if the URL is allowed according to robots.txt.
func (rc *RobotsChecker) IsAllowed(targetURL string) (bool, error) {
	logrus.Debugf("Checking robots.txt permissions for %s", targetURL)
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		logrus.Debugf("Failed to parse URL for robots check: %s - %v", targetURL, err)
		return false, fmt.Errorf("invalid URL: %w", err)
	}

	// Get robots.txt URL
	robotsURL := fmt.Sprintf("%s://%s/robots.txt", parsedURL.Scheme, parsedURL.Host)
	logrus.Debugf("Robots.txt URL: %s", robotsURL)

	// Check cache
	rc.cacheMu.RLock()
	entry, exists := rc.cache[robotsURL]
	rc.cacheMu.RUnlock()

	if exists && time.Since(entry.fetchedAt) < rc.cacheTTL {
		// Use cached rules
		allowed := rc.checkRules(entry.rules, parsedURL.Path)
		logrus.Debugf("Using cached robots.txt for %s: allowed=%v", targetURL, allowed)
		return allowed, nil
	}

	// Fetch and parse robots.txt
	logrus.Debugf("Fetching robots.txt from %s (cache miss)", robotsURL)
	rules, err := rc.fetchAndParseRobots(robotsURL)
	if err != nil {
		// If we can't fetch robots.txt, assume allowed
		logrus.Debugf("Failed to fetch robots.txt from %s: %v (assuming allowed)", robotsURL, err)
		return true, err
	}
	logrus.Debugf("Successfully parsed robots.txt from %s", robotsURL)

	// Cache the rules
	rc.cacheMu.Lock()
	rc.cache[robotsURL] = &robotsEntry{
		rules:     rules,
		fetchedAt: time.Now(),
	}
	rc.cacheMu.Unlock()

	allowed := rc.checkRules(rules, parsedURL.Path)
	logrus.Debugf("Robots.txt check result for %s: allowed=%v", targetURL, allowed)
	return allowed, nil
}

// fetchAndParseRobots fetches and parses robots.txt.
func (rc *RobotsChecker) fetchAndParseRobots(robotsURL string) (*robotsRules, error) {
	logrus.Debugf("HTTP GET %s", robotsURL)
	resp, err := rc.client.Get(robotsURL)
	if err != nil {
		logrus.Debugf("HTTP request failed for %s: %v", robotsURL, err)
		return nil, err
	}
	defer resp.Body.Close()

	logrus.Debugf("Robots.txt HTTP response: %d %s", resp.StatusCode, robotsURL)

	if resp.StatusCode == http.StatusNotFound {
		// No robots.txt means everything is allowed
		logrus.Debugf("No robots.txt found at %s - allowing all", robotsURL)
		return &robotsRules{}, nil
	}

	if resp.StatusCode != http.StatusOK {
		logrus.Debugf("Robots.txt fetch failed with status %d for %s", resp.StatusCode, robotsURL)
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return rc.parseRobots(resp.BodyReader)
}

// parseRobots parses robots.txt content.
func (rc *RobotsChecker) parseRobots(r io.Reader) (*robotsRules, error) { //nolint:cyclop // Complex robots.txt parsing
	rules := &robotsRules{}
	scanner := bufio.NewScanner(r)

	var currentAgent string
	applyToUs := false
	applyToAll := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split directive and value
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		directive := strings.TrimSpace(strings.ToLower(parts[0]))
		value := strings.TrimSpace(parts[1])

		switch directive {
		case "user-agent":
			currentAgent = strings.ToLower(value)
			applyToUs = strings.EqualFold(currentAgent, rc.userAgent) ||
				strings.HasPrefix(strings.ToLower(rc.userAgent), currentAgent)
			applyToAll = currentAgent == "*"

		case "disallow":
			if (applyToUs || (applyToAll && len(rules.disallowedPaths) == 0)) && value != "" {
				rules.disallowedPaths = append(rules.disallowedPaths, value)
			}

		case "allow":
			if (applyToUs || (applyToAll && len(rules.allowedPaths) == 0)) && value != "" {
				rules.allowedPaths = append(rules.allowedPaths, value)
			}

		case "crawl-delay":
			if applyToUs || applyToAll {
				if delay, err := time.ParseDuration(value + "s"); err == nil {
					rules.crawlDelay = delay
				}
			}
		}
	}

	return rules, scanner.Err()
}

// checkRules checks if a path is allowed according to the rules.
func (rc *RobotsChecker) checkRules(rules *robotsRules, path string) bool {
	if rules == nil {
		return true
	}

	// Check disallowed paths
	for _, pattern := range rules.disallowedPaths {
		if rc.matchesPattern(path, pattern) {
			// Check if there's a more specific allow rule
			for _, allowPattern := range rules.allowedPaths {
				if rc.matchesPattern(path, allowPattern) && len(allowPattern) > len(pattern) {
					return true
				}
			}
			return false
		}
	}

	return true
}

// matchesPattern checks if a path matches a robots.txt pattern.
func (rc *RobotsChecker) matchesPattern(path, pattern string) bool {
	// Simple pattern matching (robots.txt uses prefix matching)
	// TODO: Could be enhanced to support * wildcards
	return strings.HasPrefix(path, pattern)
}

// GetCrawlDelay returns the crawl delay for a URL's domain.
func (rc *RobotsChecker) GetCrawlDelay(targetURL string) time.Duration {
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return 0
	}

	robotsURL := fmt.Sprintf("%s://%s/robots.txt", parsedURL.Scheme, parsedURL.Host)

	rc.cacheMu.RLock()
	entry, exists := rc.cache[robotsURL]
	rc.cacheMu.RUnlock()

	if exists && entry.rules != nil {
		return entry.rules.crawlDelay
	}

	return 0
}
