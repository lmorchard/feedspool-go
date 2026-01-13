package renderer

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"

	configpkg "github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/feedlist"
)

// WorkflowConfig holds all configuration for rendering operations.
type WorkflowConfig struct {
	MaxAge          string
	Start           string
	End             string
	MinItemsPerFeed int // Minimum items to show per feed (0 = no minimum, use timespan only)
	OutputDir       string
	TemplatesDir    string
	AssetsDir       string
	FeedsFile       string
	Format          string
	Database        string
	Clean           bool
}

// ExecuteWorkflow performs the complete render operation with the given configuration.
func ExecuteWorkflow(config *WorkflowConfig) error {
	// Clean output directory if requested (do this early to avoid dependency issues)
	if config.Clean {
		if err := cleanOutputDirectory(config.OutputDir); err != nil {
			return err
		}
	}

	// Setup database
	db, err := database.New(config.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	if err := db.IsInitialized(); err != nil {
		return fmt.Errorf("database not initialized: %w", err)
	}

	// Parse time window
	startTime, endTime, err := database.ParseTimeWindow(config.MaxAge, config.Start, config.End)
	if err != nil {
		return fmt.Errorf("invalid time parameters: %w", err)
	}

	// Load feed URLs if specified
	feedURLs, err := loadFeedURLs(config.FeedsFile, config.Format)
	if err != nil {
		return err
	}

	// Create output directory
	if err := os.MkdirAll(config.OutputDir, configpkg.DefaultDirPerm); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Query data with minimum items per feed guarantee
	feeds, items, err := queryData(db, startTime, endTime, feedURLs, config.MinItemsPerFeed)
	if err != nil {
		return err
	}

	if len(feeds) == 0 {
		fmt.Println("No feeds found matching criteria") //nolint:forbidigo // User-facing output
		return nil
	}

	// Generate site
	return generateSite(config, feeds, items, startTime, endTime)
}

func loadFeedURLs(feedsFile, format string) ([]string, error) {
	if feedsFile == "" {
		return nil, nil
	}

	var feedFormat feedlist.Format
	switch format {
	case "opml":
		feedFormat = feedlist.FormatOPML
	case "text":
		feedFormat = feedlist.FormatText
	default:
		return nil, fmt.Errorf("unsupported feed format: %s (must be 'opml' or 'text')", format)
	}

	feedList, err := feedlist.LoadFeedList(feedFormat, feedsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load feed list: %w", err)
	}

	return feedList.GetURLs(), nil
}

func queryData(
	db *database.DB, startTime, endTime time.Time, feedURLs []string, minItemsPerFeed int,
) ([]database.Feed, map[string][]database.Item, error) {
	//nolint:forbidigo // User-facing output
	fmt.Printf("Rendering feeds from %s to %s...\n",
		startTime.Format("2006-01-02 15:04"), endTime.Format("2006-01-02 15:04"))
	if len(feedURLs) > 0 {
		fmt.Printf("Using %d feeds from feed list\n", len(feedURLs)) //nolint:forbidigo // User-facing output
	}
	if minItemsPerFeed > 0 {
		fmt.Printf("Ensuring at least %d items per feed\n", minItemsPerFeed) //nolint:forbidigo // User-facing output
	}

	feeds, items, err := db.GetFeedsWithItemsMinimum(startTime, endTime, feedURLs, minItemsPerFeed)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query feeds and items: %w", err)
	}

	fmt.Printf("Found %d feeds with items\n", len(feeds)) //nolint:forbidigo // User-facing output
	return feeds, items, nil
}

func generateSite(config *WorkflowConfig, feeds []database.Feed, items map[string][]database.Item,
	startTime, endTime time.Time,
) error {
	db, err := database.New(config.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	r := NewRenderer(config.TemplatesDir, config.AssetsDir)

	// Fetch metadata and favicons
	metadata, feedFavicon := fetchMetadataAndFavicons(db, feeds, items)

	// Generate template context
	context := createTemplateContext(feeds, items, metadata, feedFavicon, startTime, endTime, config.MaxAge)

	// Render main index file
	outputFile := filepath.Join(config.OutputDir, "index.html")
	if err := renderIndexFile(r, outputFile, context); err != nil {
		return err
	}

	// Copy assets
	if err := r.CopyAssets(config.OutputDir); err != nil {
		return fmt.Errorf("failed to copy assets: %w", err)
	}

	// Render individual feed pages (only if feed.html template exists)
	feedsGenerated := 0
	if hasFeedTemplate(config.TemplatesDir) {
		feedsDir := filepath.Join(config.OutputDir, "feeds")
		if err := renderIndividualFeeds(r, feedsDir, feeds, items, metadata, feedFavicon, endTime,
			getTimeWindow(startTime, endTime, config.MaxAge)); err != nil {
			return err
		}
		feedsGenerated = len(feeds)
	}

	printSuccessMessage(feedsGenerated, config.OutputDir, outputFile)
	return nil
}

func fetchMetadataAndFavicons(db *database.DB, feeds []database.Feed,
	items map[string][]database.Item,
) (metadata map[string]*database.URLMetadata, feedFavicon map[string]string) {
	metadata = make(map[string]*database.URLMetadata)
	for _, feedItems := range items {
		for i := range feedItems {
			if feedItems[i].Link != "" {
				if meta, err := db.GetMetadata(feedItems[i].Link); err == nil && meta != nil {
					metadata[feedItems[i].Link] = meta
				}
			}
		}
	}

	feedFavicon = make(map[string]string)
	for i := range feeds {
		if favicon, err := db.GetFeedFavicon(feeds[i].URL); err == nil && favicon != "" {
			feedFavicon[feeds[i].URL] = favicon
		}
	}

	return metadata, feedFavicon
}

func createTemplateContext(feeds []database.Feed, items map[string][]database.Item,
	metadata map[string]*database.URLMetadata, feedFavicon map[string]string,
	startTime, endTime time.Time, maxAge string,
) *TemplateContext {
	feedsWithIDs := make([]FeedWithID, len(feeds))
	for i := range feeds {
		feedsWithIDs[i] = FeedWithID{
			Feed: feeds[i],
			ID:   generateFeedID(feeds[i].URL),
		}
	}

	return &TemplateContext{
		Feeds:       feedsWithIDs,
		Items:       items,
		Metadata:    metadata,
		FeedFavicon: feedFavicon,
		GeneratedAt: endTime,
		TimeWindow:  getTimeWindow(startTime, endTime, maxAge),
	}
}

func renderIndexFile(r *Renderer, outputFile string, context *TemplateContext) error {
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	if err := r.Render(file, "index.html", context); err != nil {
		return fmt.Errorf("failed to render template: %w", err)
	}

	return nil
}

func renderIndividualFeeds(r *Renderer, feedsDir string, feeds []database.Feed,
	items map[string][]database.Item, metadata map[string]*database.URLMetadata,
	feedFavicon map[string]string, generatedAt time.Time, timeWindow string,
) error {
	if err := os.MkdirAll(feedsDir, configpkg.DefaultDirPerm); err != nil {
		return fmt.Errorf("failed to create feeds directory: %w", err)
	}

	for i := range feeds {
		feed := &feeds[i]
		feedItems := items[feed.URL]
		if len(feedItems) == 0 {
			continue
		}

		if err := renderSingleFeed(r, feedsDir, feed, feedItems, metadata,
			feedFavicon[feed.URL], generatedAt, timeWindow); err != nil {
			return err
		}
	}

	return nil
}

func renderSingleFeed(r *Renderer, feedsDir string, feed *database.Feed,
	feedItems []database.Item, metadata map[string]*database.URLMetadata,
	favicon string, generatedAt time.Time, timeWindow string,
) error {
	feedID := generateFeedID(feed.URL)
	feedContext := &FeedTemplateContext{
		Feed:        *feed,
		Items:       feedItems,
		Metadata:    metadata,
		FeedFavicon: favicon,
		GeneratedAt: generatedAt,
		TimeWindow:  timeWindow,
		FeedID:      feedID,
	}

	feedFile := filepath.Join(feedsDir, fmt.Sprintf("%s.html", feedID))
	file, err := os.Create(feedFile)
	if err != nil {
		return fmt.Errorf("failed to create feed file %s: %w", feedFile, err)
	}
	defer file.Close()

	if err := r.Render(file, "feed.html", feedContext); err != nil {
		return fmt.Errorf("failed to render feed template for %s: %w", feed.Title, err)
	}

	return nil
}

func getTimeWindow(startTime, endTime time.Time, maxAge string) string {
	if maxAge != "" {
		return fmt.Sprintf("Last %s", maxAge)
	}
	return fmt.Sprintf("From %s to %s",
		startTime.Format("2006-01-02 15:04"), endTime.Format("2006-01-02 15:04"))
}

func hasFeedTemplate(templatesDir string) bool {
	// If no custom template directory specified, embedded templates always have feed.html
	if templatesDir == "" {
		return true
	}

	// Check if feed.html exists in custom template directory
	feedTemplatePath := filepath.Join(templatesDir, "feed.html")
	_, err := os.Stat(feedTemplatePath)
	return err == nil
}

func printSuccessMessage(feedCount int, outputDir, outputFile string) {
	if feedCount > 0 {
		//nolint:forbidigo // User-facing output
		fmt.Printf("Generated %d individual feed pages\n", feedCount)
		//nolint:forbidigo // User-facing output
		fmt.Printf("Multi-page site generated successfully in: %s\n", outputDir)
	} else {
		//nolint:forbidigo // User-facing output
		fmt.Printf("Single-page site generated successfully in: %s\n", outputDir)
		//nolint:forbidigo // User-facing output
		fmt.Printf("(feed.html template not found - skipped individual feed pages)\n")
	}
	//nolint:forbidigo // User-facing output
	fmt.Printf("Open %s in your browser to view the site\n", outputFile)
}

// generateFeedID creates a consistent ID from a feed URL using SHA-256.
// Returns first 8 characters of the hex-encoded hash.
func generateFeedID(feedURL string) string {
	hash := sha256.Sum256([]byte(feedURL))
	return fmt.Sprintf("%x", hash)[:8]
}

func cleanOutputDirectory(outputDir string) error {
	// Check if directory exists
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		// Directory doesn't exist, nothing to clean
		return nil
	}

	fmt.Printf("Cleaning output directory: %s\n", outputDir) //nolint:forbidigo // User-facing output

	// Remove the entire directory
	if err := os.RemoveAll(outputDir); err != nil {
		return fmt.Errorf("failed to remove output directory: %w", err)
	}

	return nil
}
