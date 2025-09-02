package renderer

import (
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
	MaxAge       string
	Start        string
	End          string
	OutputDir    string
	TemplatesDir string
	AssetsDir    string
	FeedsFile    string
	Format       string
	Database     string
	Clean        bool
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

	// Query data
	feeds, items, err := queryData(db, startTime, endTime, feedURLs)
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
	db *database.DB, startTime, endTime time.Time, feedURLs []string,
) ([]database.Feed, map[string][]database.Item, error) {
	//nolint:forbidigo // User-facing output
	fmt.Printf("Rendering feeds from %s to %s...\n",
		startTime.Format("2006-01-02 15:04"), endTime.Format("2006-01-02 15:04"))
	if len(feedURLs) > 0 {
		fmt.Printf("Using %d feeds from feed list\n", len(feedURLs)) //nolint:forbidigo // User-facing output
	}

	feeds, items, err := db.GetFeedsWithItemsByTimeRange(startTime, endTime, feedURLs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query feeds and items: %w", err)
	}

	fmt.Printf("Found %d feeds with items\n", len(feeds)) //nolint:forbidigo // User-facing output
	return feeds, items, nil
}

func generateSite(config *WorkflowConfig, feeds []database.Feed, items map[string][]database.Item,
	startTime, endTime time.Time,
) error {
	// Setup database for metadata queries
	db, err := database.New(config.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// Initialize renderer
	r := NewRenderer(config.TemplatesDir, config.AssetsDir)

	// Fetch metadata for all item URLs
	metadata := make(map[string]*database.URLMetadata)
	for _, feedItems := range items {
		for i := range feedItems {
			if feedItems[i].Link != "" {
				if meta, err := db.GetMetadata(feedItems[i].Link); err == nil && meta != nil {
					metadata[feedItems[i].Link] = meta
				}
			}
		}
	}

	// Fetch favicons for feeds
	feedFavicon := make(map[string]string)
	for i := range feeds {
		if favicon, err := db.GetFeedFavicon(feeds[i].URL); err == nil && favicon != "" {
			feedFavicon[feeds[i].URL] = favicon
		}
	}

	// Prepare template context
	timeWindow := fmt.Sprintf("From %s to %s", startTime.Format("2006-01-02 15:04"), endTime.Format("2006-01-02 15:04"))
	if config.MaxAge != "" {
		timeWindow = fmt.Sprintf("Last %s", config.MaxAge)
	}

	context := &TemplateContext{
		Feeds:       feeds,
		Items:       items,
		Metadata:    metadata,
		FeedFavicon: feedFavicon,
		GeneratedAt: endTime,
		TimeWindow:  timeWindow,
	}

	// Render HTML
	outputFile := filepath.Join(config.OutputDir, "index.html")
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	if err := r.Render(file, "index.html", context); err != nil {
		return fmt.Errorf("failed to render template: %w", err)
	}

	// Copy assets
	if err := r.CopyAssets(config.OutputDir); err != nil {
		return fmt.Errorf("failed to copy assets: %w", err)
	}

	fmt.Printf("Static site generated successfully in: %s\n", config.OutputDir) //nolint:forbidigo // User-facing output
	fmt.Printf("Open %s in your browser to view the site\n", outputFile)        //nolint:forbidigo // User-facing output

	return nil
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
