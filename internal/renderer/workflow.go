package renderer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	configpkg "github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/feedlist"
	"github.com/lmorchard/feedspool-go/internal/ids"
)

// WorkflowConfig holds all configuration for rendering operations.
type WorkflowConfig struct {
	MaxAge                 string
	Start                  string
	End                    string
	MinItemsPerFeed        int     // Minimum items to show per feed (0 = no minimum, use timespan only)
	MaxItemsPerFeed        int     // Maximum items to show per feed (0 = no limit)
	FeedsPerPage           int     // Feeds per page for pagination (0 = no pagination)
	TopicMaxFeedRatio      float32 // Maximum ratio of items allowed from a single feed before rejecting the topic
	TopicMinDiversityCount int     // Min items before applying diversity ratio limit
	OutputDir              string
	TemplatesDir           string
	AssetsDir              string
	FeedsFile              string
	Format                 string
	// SiteTitle overrides the title shown as each page's <title> and <h1>.
	// Empty means derive it from the feed list named by FeedsFile, falling
	// back to DefaultSiteTitle when there is no feed list at all. Directory
	// mode sets this from the title it already resolved for the index page,
	// so both modes share one fallback chain instead of computing their own.
	SiteTitle string
	Database  string
	Clean     bool
	// MigrationProgress reports any migration this run triggers. The renderer
	// opens its own connection, so without it "render" is the one user-facing
	// command that would still migrate in silence. nil is silent, which is
	// what tests and library callers want.
	MigrationProgress database.MigrationProgress
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
	NewestItem time.Time // Newest effective item date rendered; zero if no items.
}

// summarize computes a Result from the data about to be rendered.
func summarize(feeds []database.Feed, items map[string][]database.Item) *Result {
	result := &Result{FeedCount: len(feeds)}
	for i := range feeds {
		feedItems := items[feeds[i].URL]
		result.ItemCount += len(feedItems)
		for j := range feedItems {
			itemDate := feedItems[j].EffectiveDate()
			if itemDate.After(result.NewestItem) {
				result.NewestItem = itemDate
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

	// Set before IsInitialized, which is the call that migrates.
	db.SetMigrationProgress(config.MigrationProgress)
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
		logrus.Info("No feeds found matching criteria")
	}

	// Apply max items per feed limit if configured
	if config.MaxItemsPerFeed > 0 {
		items = limitItemsPerFeed(items, config.MaxItemsPerFeed)
		if !config.Quiet {
			logrus.Infof("Limited to maximum %d items per feed", config.MaxItemsPerFeed)
		}
	}

	// Generate site. FormatTimeWindow is called once here rather than three
	// times inside generateSite, which is what the chrome struct buys.
	chrome := SiteChrome{
		SiteTitle:   resolveSiteTitle(config.SiteTitle, listTitle),
		TimeWindow:  FormatTimeWindow(startTime, endTime, config.MaxAge),
		GeneratedAt: endTime,
	}
	if err := generateSite(config, feeds, items, chrome, feedURLs); err != nil {
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
		logrus.Infof("Rendering feeds from %s to %s...",
			startTime.Format("2006-01-02 15:04"), endTime.Format("2006-01-02 15:04"))
		if len(feedURLs) > 0 {
			logrus.Infof("Using %d feeds from feed list", len(feedURLs))
		}
		if minItemsPerFeed > 0 {
			logrus.Infof("Ensuring at least %d items per feed", minItemsPerFeed)
		}
	}

	feeds, items, err := db.GetFeedsWithItemsMinimum(startTime, endTime, feedURLs, minItemsPerFeed)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query feeds and items: %w", err)
	}

	if !quiet {
		logrus.Infof("Found %d feeds with items", len(feeds))
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
	chrome SiteChrome, feedURLs []string,
) error {
	db, err := database.New(config.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	r := NewRenderer(config.TemplatesDir, config.AssetsDir)

	// Topics
	topicCtx, hasTopics := resolveSiteTopics(db, config, chrome, feedURLs)
	chrome.HasTopics = hasTopics

	// Fetch metadata and favicons
	metadata, feedFavicon := fetchMetadataAndFavicons(db, feeds, items)

	// Generate template context
	templateCtx := createTemplateContext(feeds, items, metadata, feedFavicon, chrome)

	// Calculate pagination info
	feedsPerPage := config.FeedsPerPage
	if feedsPerPage <= 0 {
		feedsPerPage = len(feeds) // Disable pagination
	}
	pages := splitFeedsIntoPages(templateCtx.Feeds, feedsPerPage)
	totalPages := len(pages)

	// Render main index file
	outputFile := filepath.Join(config.OutputDir, "index.html")
	if err := renderIndexFile(r, outputFile, templateCtx, totalPages, feedsPerPage); err != nil {
		return err
	}

	if err := writeOrRemoveTopicsFile(r, config, topicCtx); err != nil {
		return err
	}

	// Copy assets
	if err := r.CopyAssets(config.OutputDir); err != nil {
		return fmt.Errorf("failed to copy assets: %w", err)
	}

	feedsDir := filepath.Join(config.OutputDir, "feeds")

	// Render feed list page fragments (if pagination enabled)
	if totalPages > 1 {
		if err := renderFeedPages(r, feedsDir, templateCtx.Feeds, items, metadata,
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

func resolveSiteTopics(
	db *database.DB, config *WorkflowConfig, chrome SiteChrome, feedURLs []string,
) (*TopicsTemplateContext, bool) {
	run, _ := db.GetLatestTopicRun(context.Background())
	if run == nil {
		return nil, false
	}
	topicCtx, rawItemsMap := BuildTopicsContext(db, run, chrome, config, feedURLs)
	if topicCtx == nil || len(topicCtx.Topics) == 0 {
		return nil, false
	}
	topicCtx.HasTopics = true
	FetchTopicMetadataAndFavicons(db, topicCtx, rawItemsMap)
	return topicCtx, true
}

func writeOrRemoveTopicsFile(r *Renderer, config *WorkflowConfig, topicCtx *TopicsTemplateContext) error {
	topicOutputFile := filepath.Join(config.OutputDir, "topics.html")
	if topicCtx != nil {
		return renderTopicsFile(r, topicOutputFile, topicCtx)
	}
	if err := os.Remove(topicOutputFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove stale topics file: %w", err)
	}
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
			ID:   ids.FeedID(feeds[i].URL),
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

// BuildTopicsContext builds the template context for trending topics.
// If allowedFeedURLs is non-nil, only items from feeds in allowedFeedURLs are included.
func BuildTopicsContext(
	db *database.DB, run *database.TopicRun, chrome SiteChrome, config *WorkflowConfig, allowedFeedURLs []string,
) (ctx *TopicsTemplateContext, rawItemsMap map[int64][]*database.Item) {
	topicList, err := db.GetTopicsForRun(context.Background(), run.ID)
	if err != nil {
		return nil, nil
	}
	topicItemsMap, err := db.GetTopicItems(context.Background(), run.ID)
	if err != nil {
		return nil, nil
	}

	var allItemIDs []int64
	for _, ids := range topicItemsMap {
		allItemIDs = append(allItemIDs, ids...)
	}
	topicItemsData, err := db.GetItemsByIDs(allItemIDs)
	if err != nil {
		return nil, nil
	}

	var allowedSet map[string]bool
	if allowedFeedURLs != nil {
		allowedSet = make(map[string]bool, len(allowedFeedURLs))
		for _, url := range allowedFeedURLs {
			allowedSet[url] = true
		}
	}

	topicItemSlices := make(map[int64][]*database.Item)
	rejectedCount := 0
	for topicID, itemIDs := range topicItemsMap {
		slice, rejected := filterSingleTopicItems(itemIDs, topicItemsData, allowedSet, config)
		if rejected {
			rejectedCount++
			continue
		}
		topicItemSlices[topicID] = slice
	}

	if rejectedCount > 0 && !config.Quiet {
		logrus.Infof("Render filtered out %d topic(s) exceeding max single-feed ratio or with no items for site",
			rejectedCount)
	}

	// Filter out topics that were rejected and copy topics with updated item counts
	var cleanTopicList []*database.Topic
	for _, topic := range topicList {
		if slice, ok := topicItemSlices[topic.ID]; ok {
			topicCopy := *topic
			topicCopy.Score = float64(len(slice))
			cleanTopicList = append(cleanTopicList, &topicCopy)
		}
	}

	if len(cleanTopicList) == 0 {
		return nil, nil
	}

	return &TopicsTemplateContext{
		SiteChrome: chrome,
		Run:        run,
		Topics:     cleanTopicList,
		GroupsMap:  make(map[int64][]TopicFeedGroup),
	}, topicItemSlices
}

func filterSingleTopicItems(
	itemIDs []int64, topicItemsData map[int64]*database.Item, allowedSet map[string]bool, config *WorkflowConfig,
) ([]*database.Item, bool) {
	var slice []*database.Item
	feedCounts := make(map[string]int)

	for _, id := range itemIDs {
		if item, ok := topicItemsData[id]; ok {
			if allowedSet != nil && !allowedSet[item.FeedURL] {
				continue
			}
			slice = append(slice, item)
			feedCounts[item.FeedURL]++
		}
	}

	if len(slice) == 0 {
		return nil, true
	}

	if config.TopicMaxFeedRatio > 0 && len(slice) >= config.TopicMinDiversityCount {
		for _, count := range feedCounts {
			if float32(count)/float32(len(slice)) >= config.TopicMaxFeedRatio {
				return nil, true
			}
		}
	}

	return slice, false
}

// FetchTopicMetadataAndFavicons populates metadata, favicons, and feed groups for topics.
//
//nolint:cyclop
func FetchTopicMetadataAndFavicons(
	db *database.DB, topicCtx *TopicsTemplateContext, rawItemsMap map[int64][]*database.Item,
) {
	topicCtx.Metadata = make(map[string]*database.URLMetadata)

	feedURLs := make(map[string]bool)
	for _, itemsList := range rawItemsMap {
		for _, item := range itemsList {
			if item.Link != "" {
				if meta, err := db.GetMetadata(item.Link); err == nil && meta != nil {
					topicCtx.Metadata[item.Link] = meta
				}
			}
			if item.FeedURL != "" {
				feedURLs[item.FeedURL] = true
			}
		}
	}

	feedFavicon := make(map[string]string)
	feedTitles := make(map[string]string)
	for feedURL := range feedURLs {
		if favicon, err := db.GetFeedFavicon(feedURL); err == nil && favicon != "" {
			feedFavicon[feedURL] = favicon
		}
		if feed, err := db.GetFeed(feedURL); err == nil && feed != nil {
			feedTitles[feedURL] = feed.Title
		}
	}

	// Now that we have titles and favicons, populate the GroupsMap
	for topicID, itemsList := range rawItemsMap {
		// Group items by feed URL
		groups := make(map[string]*TopicFeedGroup)
		for _, item := range itemsList {
			group, ok := groups[item.FeedURL]
			if !ok {
				group = &TopicFeedGroup{
					FeedURL: item.FeedURL,
					Title:   feedTitles[item.FeedURL],
					Favicon: feedFavicon[item.FeedURL],
				}
				groups[item.FeedURL] = group
			}
			group.Items = append(group.Items, item)
		}

		// Convert map to slice and sort deterministically
		slice := make([]TopicFeedGroup, 0, len(groups))
		for _, group := range groups {
			slice = append(slice, *group)
		}
		sort.Slice(slice, func(i, j int) bool {
			if len(slice[i].Items) != len(slice[j].Items) {
				return len(slice[i].Items) > len(slice[j].Items)
			}
			if slice[i].Title != slice[j].Title {
				return slice[i].Title < slice[j].Title
			}
			return slice[i].FeedURL < slice[j].FeedURL
		})
		topicCtx.GroupsMap[topicID] = slice
	}
}

// RenderGlobalTopics renders the top-level topics.html page for multi-site directory builds.
func RenderGlobalTopics(config *WorkflowConfig, chrome SiteChrome) (bool, error) {
	db, err := database.New(config.Database)
	if err != nil {
		removeStaleTopicsFile(config.OutputDir)
		return false, fmt.Errorf("failed to connect to database for global topics: %w", err)
	}
	defer db.Close()

	run, err := db.GetLatestTopicRun(context.Background())
	if err != nil {
		removeStaleTopicsFile(config.OutputDir)
		return false, fmt.Errorf("failed to query latest topic run: %w", err)
	}
	if run == nil {
		removeStaleTopicsFile(config.OutputDir)
		return false, nil
	}

	topicCtx, rawItemsMap := BuildTopicsContext(db, run, chrome, config, nil)
	if topicCtx == nil || len(topicCtx.Topics) == 0 {
		removeStaleTopicsFile(config.OutputDir)
		return false, nil
	}

	chrome.HasTopics = true
	topicCtx.HasTopics = true

	FetchTopicMetadataAndFavicons(db, topicCtx, rawItemsMap)

	r := NewRenderer(config.TemplatesDir, config.AssetsDir)
	topicOutputFile := filepath.Join(config.OutputDir, "topics.html")
	if err := renderTopicsFile(r, topicOutputFile, topicCtx); err != nil {
		removeStaleTopicsFile(config.OutputDir)
		return false, err
	}

	if err := r.CopyAssets(config.OutputDir); err != nil {
		removeStaleTopicsFile(config.OutputDir)
		return false, fmt.Errorf("failed to copy assets for global topics: %w", err)
	}

	return true, nil
}

func removeStaleTopicsFile(dir string) {
	topicOutputFile := filepath.Join(dir, "topics.html")
	_ = os.Remove(topicOutputFile)
}

func renderIndexFile(r *Renderer, outputFile string, templateCtx *TemplateContext, totalPages, feedsPerPage int) error {
	// Wrap context with pagination info for template
	type IndexContext struct {
		*TemplateContext
		TotalPages   int
		FeedsPerPage int
	}

	indexContext := &IndexContext{
		TemplateContext: templateCtx,
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

func renderTopicsFile(r *Renderer, outputFile string, topicCtx *TopicsTemplateContext) error {
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	if err := r.Render(file, "topics.html", topicCtx); err != nil {
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
		logrus.Infof("Generated %d feed list pages", totalPages)
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
	feedID := ids.FeedID(feed.URL)
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
		logrus.Infof("Generated %d individual feed pages", feedCount)
		logrus.Infof("Multi-page site generated successfully in: %s", outputDir)
	} else {
		logrus.Infof("Single-page site generated successfully in: %s", outputDir)
		if feedTemplateExists {
			logrus.Info("(no feeds matched - no individual feed pages to generate)")
		} else {
			logrus.Info("(feed.html template not found - skipped individual feed pages)")
		}
	}
	logrus.Infof("Open %s in your browser to view the site", outputFile)
}
