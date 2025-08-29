package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/feedlist"
	"github.com/lmorchard/feedspool-go/internal/renderer"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	renderMaxAge     string
	renderStart      string
	renderEnd        string
	renderOutput     string
	renderTemplates  string
	renderAssets     string
	renderFeeds      string
	renderFormat     string
)

var renderCmd = &cobra.Command{
	Use:   "render",
	Short: "Generate static HTML site from feeds",
	Long: `Generate a static HTML site from feed data using customizable templates.

Time filtering options:
  --max-age 24h                     # Show feeds updated in last 24 hours
  --start 2023-01-01T00:00:00Z      # Explicit start time (RFC3339)
  --end 2023-12-31T23:59:59Z        # Explicit end time (RFC3339)

Feed filtering:
  --feeds feeds.txt --format text   # Use feeds from text file
  --feeds feeds.opml --format opml  # Use feeds from OPML file

Customization:
  --templates ./custom-templates    # Use custom templates directory
  --assets ./custom-assets          # Use custom static assets directory
  --output ./site                   # Output directory (default: ./build)

The command generates an index.html file with all matching feeds and their items
grouped underneath. Static assets (CSS, JS) are copied to the output directory.

Use 'feedspool init --extract-templates' to extract default templates for customization.`,
	RunE: runRender,
}

func init() {
	renderCmd.Flags().StringVar(&renderMaxAge, "max-age", "", "Show feeds updated within duration (e.g., 24h, 7d)")
	renderCmd.Flags().StringVar(&renderStart, "start", "", "Start time (RFC3339 format)")
	renderCmd.Flags().StringVar(&renderEnd, "end", "", "End time (RFC3339 format)")
	renderCmd.Flags().StringVar(&renderOutput, "output", "./build", "Output directory")
	renderCmd.Flags().StringVar(&renderTemplates, "templates", "", "Custom templates directory")
	renderCmd.Flags().StringVar(&renderAssets, "assets", "", "Custom assets directory")
	renderCmd.Flags().StringVar(&renderFeeds, "feeds", "", "Feed list file")
	renderCmd.Flags().StringVar(&renderFormat, "format", "text", "Feed list format (opml or text)")

	// Bind flags to viper for config file support
	_ = viper.BindPFlag("render.max_age", renderCmd.Flags().Lookup("max-age"))
	_ = viper.BindPFlag("render.start", renderCmd.Flags().Lookup("start"))
	_ = viper.BindPFlag("render.end", renderCmd.Flags().Lookup("end"))
	_ = viper.BindPFlag("render.output", renderCmd.Flags().Lookup("output"))
	_ = viper.BindPFlag("render.templates", renderCmd.Flags().Lookup("templates"))
	_ = viper.BindPFlag("render.assets", renderCmd.Flags().Lookup("assets"))
	_ = viper.BindPFlag("render.feeds", renderCmd.Flags().Lookup("feeds"))
	_ = viper.BindPFlag("render.format", renderCmd.Flags().Lookup("format"))

	rootCmd.AddCommand(renderCmd)
}

func runRender(_ *cobra.Command, _ []string) error {
	cfg := GetConfig()

	// Get values from viper (includes config file values)
	maxAge := viper.GetString("render.max_age")
	start := viper.GetString("render.start")
	end := viper.GetString("render.end")
	outputDir := viper.GetString("render.output")
	templatesDir := viper.GetString("render.templates")
	assetsDir := viper.GetString("render.assets")
	feedsFile := viper.GetString("render.feeds")
	format := viper.GetString("render.format")

	// Override with command line flags if provided
	if renderMaxAge != "" {
		maxAge = renderMaxAge
	}
	if renderStart != "" {
		start = renderStart
	}
	if renderEnd != "" {
		end = renderEnd
	}
	if renderOutput != "./build" {
		outputDir = renderOutput
	}
	if renderTemplates != "" {
		templatesDir = renderTemplates
	}
	if renderAssets != "" {
		assetsDir = renderAssets
	}
	if renderFeeds != "" {
		feedsFile = renderFeeds
	}
	if renderFormat != "text" {
		format = renderFormat
	}

	// Validate parameters
	if err := validateRenderParams(maxAge, start, end, outputDir, templatesDir, assetsDir, feedsFile, format); err != nil {
		return err
	}

	// Parse time window
	startTime, endTime, err := database.ParseTimeWindow(maxAge, start, end)
	if err != nil {
		return fmt.Errorf("invalid time parameters: %w", err)
	}

	// Load feed URLs from file if specified
	var feedURLs []string
	if feedsFile != "" {
		var feedFormat feedlist.Format
		switch format {
		case "opml":
			feedFormat = feedlist.FormatOPML
		case "text":
			feedFormat = feedlist.FormatText
		default:
			return fmt.Errorf("unsupported feed format: %s (must be 'opml' or 'text')", format)
		}

		feedList, err := feedlist.LoadFeedList(feedFormat, feedsFile)
		if err != nil {
			return fmt.Errorf("failed to load feed list: %w", err)
		}
		feedURLs = feedList.GetURLs()
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Connect to database
	if err := database.Connect(cfg.Database); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer database.Close()

	if err := database.IsInitialized(); err != nil {
		return fmt.Errorf("database not initialized: %w", err)
	}

	fmt.Printf("Rendering feeds from %s to %s...\n", startTime.Format("2006-01-02 15:04"), endTime.Format("2006-01-02 15:04"))
	if len(feedURLs) > 0 {
		fmt.Printf("Using %d feeds from %s\n", len(feedURLs), feedsFile)
	}

	// Query feeds and items
	feeds, items, err := database.GetFeedsWithItemsByTimeRange(startTime, endTime, feedURLs)
	if err != nil {
		return fmt.Errorf("failed to query feeds and items: %w", err)
	}

	if len(feeds) == 0 {
		fmt.Println("No feeds found matching criteria")
		return nil
	}

	fmt.Printf("Found %d feeds with items\n", len(feeds))

	// Initialize renderer
	r := renderer.NewRenderer(templatesDir, assetsDir)

	// Prepare template context
	timeWindow := fmt.Sprintf("From %s to %s", startTime.Format("2006-01-02 15:04"), endTime.Format("2006-01-02 15:04"))
	if maxAge != "" {
		timeWindow = fmt.Sprintf("Last %s", maxAge)
	}

	context := &renderer.TemplateContext{
		Feeds:       feeds,
		Items:       items,
		GeneratedAt: endTime,
		TimeWindow:  timeWindow,
	}

	// Render HTML
	outputFile := filepath.Join(outputDir, "index.html")
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	if err := r.Render(file, "index.html", context); err != nil {
		return fmt.Errorf("failed to render template: %w", err)
	}

	// Copy assets
	if err := r.CopyAssets(outputDir); err != nil {
		return fmt.Errorf("failed to copy assets: %w", err)
	}

	fmt.Printf("Static site generated successfully in: %s\n", outputDir)
	fmt.Printf("Open %s in your browser to view the site\n", outputFile)

	return nil
}

func validateRenderParams(maxAge, start, end, outputDir, templatesDir, assetsDir, feedsFile, format string) error {
	// Validate time parameters
	if maxAge != "" && (start != "" || end != "") {
		return fmt.Errorf("cannot specify both --max-age and explicit time range (--start/--end)")
	}

	// Validate template directory exists if specified
	if templatesDir != "" {
		if _, err := os.Stat(templatesDir); os.IsNotExist(err) {
			return fmt.Errorf("templates directory does not exist: %s", templatesDir)
		}
	}

	// Validate assets directory exists if specified
	if assetsDir != "" {
		if _, err := os.Stat(assetsDir); os.IsNotExist(err) {
			return fmt.Errorf("assets directory does not exist: %s", assetsDir)
		}
	}

	// Validate feed list file exists if specified
	if feedsFile != "" {
		if _, err := os.Stat(feedsFile); os.IsNotExist(err) {
			return fmt.Errorf("feed list file does not exist: %s", feedsFile)
		}

		// Validate format
		if format != "opml" && format != "text" {
			return fmt.Errorf("unsupported format: %s (must be 'opml' or 'text')", format)
		}
	}

	// Check if we can write to output directory
	if outputDir != "" {
		parent := filepath.Dir(outputDir)
		if _, err := os.Stat(parent); os.IsNotExist(err) {
			return fmt.Errorf("parent directory for output does not exist: %s", parent)
		}
	}

	return nil
}