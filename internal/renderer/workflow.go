package renderer

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	MaxItemsPerFeed int // Maximum items to show per feed (0 = no limit)
	FeedsPerPage    int // Feeds per page for pagination (0 = no pagination)
	OutputDir       string
	TemplatesDir    string
	AssetsDir       string
	FeedsFile       string
	Format          string
	// SiteTitle overrides the title shown as each page's <title> and <h1>.
	// Empty means derive it from the feed list named by FeedsFile, falling
	// back to DefaultSiteTitle when there is no feed list at all. Directory
	// mode sets this from the title it already resolved for the index page,
	// so both modes share one fallback chain instead of computing their own.
	SiteTitle string
	Database  string
	Clean     bool
	// Quiet suppresses per-site progress output (the "Rendering feeds
	// from...", "Found N feeds...", "Open .../index.html..." lines).
	// Directory-mode callers that print their own summary set this so a
	// dozen sites don't each narrate their own render.
	Quiet bool
}

// Result summarizes what a single ExecuteWorkflow call produced. It feeds the
// multi-site index page.
type Result struct {
	FeedCount  int       // Feeds matching the time window and feed-list filter.
	ItemCount  int       // Items rendered, after min/max per-feed limits.
	NewestItem time.Time // Newest PublishedDate rendered; zero if no items.
}

// summarize computes a Result from the data about to be rendered.
func summarize(feeds []database.Feed, items map[string][]database.Item) *Result {
	result := &Result{FeedCount: len(feeds)}
	for i := range feeds {
		feedItems := items[feeds[i].URL]
		result.ItemCount += len(feedItems)
		for j := range feedItems {
			if feedItems[j].PublishedDate.After(result.NewestItem) {
				result.NewestItem = feedItems[j].PublishedDate
			}
		}
	}
	return result
}

// ExecuteWorkflow performs the complete render operation with the given configuration.
func ExecuteWorkflow(config *WorkflowConfig) (*Result, error) {
	originalOutputDir := config.OutputDir

	renderDir, cleanup, err := SetupStagingDir(config.Clean, originalOutputDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	config.OutputDir = renderDir
	defer func() { config.OutputDir = originalOutputDir }()

	// Setup database
	db, err := database.New(config.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	if err := db.IsInitialized(); err != nil {
		return nil, fmt.Errorf("database not initialized: %w", err)
	}

	// Parse time window
	startTime, endTime, err := database.ParseTimeWindow(config.MaxAge, config.Start, config.End)
	if err != nil {
		return nil, fmt.Errorf("invalid time parameters: %w", err)
	}

	// Load feed URLs if specified
	feedURLs, listTitle, err := loadFeedURLs(config.FeedsFile, config.Format)
	if err != nil {
		return nil, err
	}

	// Create output directory
	if err := os.MkdirAll(config.OutputDir, configpkg.DefaultDirPerm); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Query data with minimum items per feed guarantee
	feeds, items, err := queryData(db, startTime, endTime, feedURLs, config.MinItemsPerFeed, config.Quiet)
	if err != nil {
		return nil, err
	}

	if len(feeds) == 0 && !config.Quiet {
		fmt.Println("No feeds found matching criteria") //nolint:forbidigo // User-facing output
	}

	// Apply max items per feed limit if configured
	if config.MaxItemsPerFeed > 0 {
		items = limitItemsPerFeed(items, config.MaxItemsPerFeed)
		if !config.Quiet {
			//nolint:forbidigo // User-facing output
			fmt.Printf("Limited to maximum %d items per feed\n", config.MaxItemsPerFeed)
		}
	}

	// Generate site. FormatTimeWindow is called once here rather than three
	// times inside generateSite, which is what the chrome struct buys.
	chrome := SiteChrome{
		SiteTitle:   resolveSiteTitle(config.SiteTitle, listTitle),
		TimeWindow:  FormatTimeWindow(startTime, endTime, config.MaxAge),
		GeneratedAt: endTime,
	}
	if err := generateSite(config, feeds, items, chrome); err != nil {
		return nil, err
	}

	result := summarize(feeds, items)

	if config.Clean {
		if err := AtomicSwap(renderDir, originalOutputDir); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// loadFeedURLs reads the feed list at feedsFile and returns its URLs together
// with a display title for the site built from it: the list's own title, or
// the filename base when the list carries none. A text list never carries a
// title, and neither does an OPML with no <head><title>. Returns an empty
// title when there is no feed list.
func loadFeedURLs(feedsFile, format string) (urls []string, title string, err error) {
	if feedsFile == "" {
		return nil, "", nil
	}

	var feedFormat feedlist.Format
	switch format {
	case "opml":
		feedFormat = feedlist.FormatOPML
	case "text":
		feedFormat = feedlist.FormatText
	default:
		return nil, "", fmt.Errorf("unsupported feed format: %s (must be 'opml' or 'text')", format)
	}

	feedList, err := feedlist.LoadFeedList(feedFormat, feedsFile)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load feed list: %w", err)
	}

	// OPMLFeedList.Title already trims surrounding whitespace, so a
	// whitespace-only <title> arrives here as empty and takes the fallback.
	title = feedList.Title()
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(feedsFile), filepath.Ext(feedsFile))
	}

	return feedList.GetURLs(), title, nil
}

// resolveSiteTitle picks the title for the pages about to be rendered:
// an explicit override from the caller, then the feed list's own title, then
// the fixed default. Terminating the chain here rather than in the templates
// means SiteTitle is never empty by render time, so no template needs a
// conditional.
func resolveSiteTitle(override, listTitle string) string {
	if override != "" {
		return override
	}
	if listTitle != "" {
		return listTitle
	}
	return DefaultSiteTitle
}

func queryData(
	db *database.DB, startTime, endTime time.Time, feedURLs []string, minItemsPerFeed int, quiet bool,
) ([]database.Feed, map[string][]database.Item, error) {
	if !quiet {
		//nolint:forbidigo // User-facing output
		fmt.Printf("Rendering feeds from %s to %s...\n",
			startTime.Format("2006-01-02 15:04"), endTime.Format("2006-01-02 15:04"))
		if len(feedURLs) > 0 {
			fmt.Printf("Using %d feeds from feed list\n", len(feedURLs)) //nolint:forbidigo // User-facing output
		}
		if minItemsPerFeed > 0 {
			fmt.Printf("Ensuring at least %d items per feed\n", minItemsPerFeed) //nolint:forbidigo // User-facing output
		}
	}

	feeds, items, err := db.GetFeedsWithItemsMinimum(startTime, endTime, feedURLs, minItemsPerFeed)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query feeds and items: %w", err)
	}

	if !quiet {
		fmt.Printf("Found %d feeds with items\n", len(feeds)) //nolint:forbidigo // User-facing output
	}
	return feeds, items, nil
}

// limitItemsPerFeed limits the number of items for each feed to the specified maximum.
func limitItemsPerFeed(items map[string][]database.Item, maxItems int) map[string][]database.Item {
	if maxItems <= 0 {
		return items
	}

	limited := make(map[string][]database.Item)
	for feedURL, feedItems := range items {
		if len(feedItems) <= maxItems {
			limited[feedURL] = feedItems
		} else {
			limited[feedURL] = feedItems[:maxItems]
		}
	}
	return limited
}

func generateSite(config *WorkflowConfig, feeds []database.Feed, items map[string][]database.Item,
	chrome SiteChrome,
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
	context := createTemplateContext(feeds, items, metadata, feedFavicon, chrome)

	// Calculate pagination info
	feedsPerPage := config.FeedsPerPage
	if feedsPerPage <= 0 {
		feedsPerPage = len(feeds) // Disable pagination
	}
	pages := splitFeedsIntoPages(context.Feeds, feedsPerPage)
	totalPages := len(pages)

	// Render main index file
	outputFile := filepath.Join(config.OutputDir, "index.html")
	if err := renderIndexFile(r, outputFile, context, totalPages, feedsPerPage); err != nil {
		return err
	}

	// Copy assets
	if err := r.CopyAssets(config.OutputDir); err != nil {
		return fmt.Errorf("failed to copy assets: %w", err)
	}

	feedsDir := filepath.Join(config.OutputDir, "feeds")

	// Render feed list page fragments (if pagination enabled)
	if totalPages > 1 {
		if err := renderFeedPages(r, feedsDir, context.Feeds, items, metadata,
			feedFavicon, chrome, feedsPerPage, config.Quiet); err != nil {
			return err
		}
	}

	// Render individual feed pages (only if feed.html template exists)
	feedTemplateExists := hasFeedTemplate(config.TemplatesDir)
	feedsGenerated := 0
	if feedTemplateExists {
		if err := renderIndividualFeeds(r, feedsDir, feeds, items, metadata,
			feedFavicon, chrome); err != nil {
			return err
		}
		feedsGenerated = len(feeds)
	}

	printSuccessMessage(feedsGenerated, feedTemplateExists, config.OutputDir, outputFile, config.Quiet)
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
	chrome SiteChrome,
) *TemplateContext {
	feedsWithIDs := make([]FeedWithID, len(feeds))
	for i := range feeds {
		feedsWithIDs[i] = FeedWithID{
			Feed: feeds[i],
			ID:   generateFeedID(feeds[i].URL),
		}
	}

	return &TemplateContext{
		SiteChrome:  chrome,
		Feeds:       feedsWithIDs,
		Items:       items,
		Metadata:    metadata,
		FeedFavicon: feedFavicon,
	}
}

func renderIndexFile(r *Renderer, outputFile string, context *TemplateContext, totalPages, feedsPerPage int) error {
	// Wrap context with pagination info for template
	type IndexContext struct {
		*TemplateContext
		TotalPages   int
		FeedsPerPage int
	}

	indexContext := &IndexContext{
		TemplateContext: context,
		TotalPages:      totalPages,
		FeedsPerPage:    feedsPerPage,
	}

	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	if err := r.Render(file, "index.html", indexContext); err != nil {
		return fmt.Errorf("failed to render template: %w", err)
	}

	return nil
}

// splitFeedsIntoPages divides feeds into pages of the specified size.
// Returns a slice of feed slices, one per page.
func splitFeedsIntoPages(feeds []FeedWithID, pageSize int) [][]FeedWithID {
	if pageSize <= 0 {
		return [][]FeedWithID{feeds} // No pagination
	}

	var pages [][]FeedWithID
	for i := 0; i < len(feeds); i += pageSize {
		end := i + pageSize
		if end > len(feeds) {
			end = len(feeds)
		}
		pages = append(pages, feeds[i:end])
	}
	return pages
}

// renderFeedPages renders paginated feed list pages in feeds/page-N.html.
func renderFeedPages(r *Renderer, feedsDir string, feeds []FeedWithID,
	items map[string][]database.Item, metadata map[string]*database.URLMetadata,
	feedFavicon map[string]string, chrome SiteChrome,
	feedsPerPage int, quiet bool,
) error {
	if err := os.MkdirAll(feedsDir, configpkg.DefaultDirPerm); err != nil {
		return fmt.Errorf("failed to create feeds directory: %w", err)
	}

	pages := splitFeedsIntoPages(feeds, feedsPerPage)
	totalPages := len(pages)

	for pageNum, pageFeeds := range pages {
		pageContext := &PageTemplateContext{
			SiteChrome:  chrome,
			Feeds:       pageFeeds,
			Items:       items, // Full items map (feeds reference what they need)
			Metadata:    metadata,
			FeedFavicon: feedFavicon,
			PageNumber:  pageNum + 1, // 1-indexed
			TotalPages:  totalPages,
		}

		pageFile := filepath.Join(feedsDir, fmt.Sprintf("page-%d.html", pageNum+1))
		file, err := os.Create(pageFile)
		if err != nil {
			return fmt.Errorf("failed to create page file %s: %w", pageFile, err)
		}

		err = r.Render(file, "feed-list-page.html", pageContext)
		file.Close() // Close immediately to avoid defer accumulation

		if err != nil {
			return fmt.Errorf("failed to render page %d: %w", pageNum+1, err)
		}
	}

	if !quiet {
		fmt.Printf("Generated %d feed list pages\n", totalPages) //nolint:forbidigo
	}

	return nil
}

func renderIndividualFeeds(r *Renderer, feedsDir string, feeds []database.Feed,
	items map[string][]database.Item, metadata map[string]*database.URLMetadata,
	feedFavicon map[string]string, chrome SiteChrome,
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
			feedFavicon[feed.URL], chrome); err != nil {
			return err
		}
	}

	return nil
}

func renderSingleFeed(r *Renderer, feedsDir string, feed *database.Feed,
	feedItems []database.Item, metadata map[string]*database.URLMetadata,
	favicon string, chrome SiteChrome,
) error {
	feedID := generateFeedID(feed.URL)
	feedContext := &FeedTemplateContext{
		SiteChrome:  chrome,
		Feed:        *feed,
		Items:       feedItems,
		Metadata:    metadata,
		FeedFavicon: favicon,
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

// FormatTimeWindow describes the render window for display: the configured
// max-age string when set, or the resolved start/end times otherwise.
// Exported so other packages that render related pages (such as the
// multi-site index) can describe the same window without duplicating this
// formatting and risking disagreement with the per-site pages.
func FormatTimeWindow(startTime, endTime time.Time, maxAge string) string {
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

func printSuccessMessage(feedCount int, feedTemplateExists bool, outputDir, outputFile string, quiet bool) {
	if quiet {
		return
	}
	if feedCount > 0 {
		//nolint:forbidigo // User-facing output
		fmt.Printf("Generated %d individual feed pages\n", feedCount)
		//nolint:forbidigo // User-facing output
		fmt.Printf("Multi-page site generated successfully in: %s\n", outputDir)
	} else {
		//nolint:forbidigo // User-facing output
		fmt.Printf("Single-page site generated successfully in: %s\n", outputDir)
		if feedTemplateExists {
			//nolint:forbidigo // User-facing output
			fmt.Printf("(no feeds matched - no individual feed pages to generate)\n")
		} else {
			//nolint:forbidigo // User-facing output
			fmt.Printf("(feed.html template not found - skipped individual feed pages)\n")
		}
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
