package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/renderer"
	"github.com/spf13/cobra"
)

var (
	renderMaxAge    string
	renderStart     string
	renderEnd       string
	renderOutput    string
	renderTemplates string
	renderAssets    string
	renderFeeds     string
	renderFormat    string
	renderClean     bool
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
  --clean                          # Remove output directory before building

The command generates an index.html file with all matching feeds and their items
grouped underneath. Static assets (CSS, JS) are copied to the output directory.

Use 'feedspool init --extract-templates' to extract default templates for customization.`,
	RunE: runRender,
}

func init() {
	renderCmd.Flags().StringVar(&renderMaxAge, "max-age", "", "Show feeds updated within duration (e.g., 24h, 7d)")
	renderCmd.Flags().StringVar(&renderStart, "start", "", "Start time (RFC3339 format)")
	renderCmd.Flags().StringVar(&renderEnd, "end", "", "End time (RFC3339 format)")
	renderCmd.Flags().StringVar(&renderOutput, "output", defaultOutputDir, "Output directory")
	renderCmd.Flags().StringVar(&renderTemplates, "templates", "", "Custom templates directory")
	renderCmd.Flags().StringVar(&renderAssets, "assets", "", "Custom assets directory")
	renderCmd.Flags().StringVar(&renderFeeds, "feeds", "", "Feed list file")
	renderCmd.Flags().StringVar(&renderFormat, "format", defaultFormat, "Feed list format (opml or text)")
	renderCmd.Flags().BoolVar(&renderClean, "clean", false, "Remove output directory before building")

	// Note: Config file values are loaded through the Config struct, not viper bindings

	rootCmd.AddCommand(renderCmd)
}

func runRender(_ *cobra.Command, _ []string) error {
	cfg := GetConfig()

	// Build configuration from flags and config file
	config := buildRenderConfig(cfg)

	// Validate configuration
	if err := validateRenderConfig(config); err != nil {
		return err
	}

	// Execute the render operation
	return renderer.ExecuteWorkflow(config)
}

func buildRenderConfig(cfg *config.Config) *renderer.WorkflowConfig {
	// Start with config file values
	config := &renderer.WorkflowConfig{
		MaxAge:       cfg.Render.DefaultMaxAge,
		Start:        "",
		End:          "",
		OutputDir:    cfg.Render.OutputDir,
		TemplatesDir: cfg.Render.TemplatesDir,
		AssetsDir:    cfg.Render.AssetsDir,
		FeedsFile:    "",
		Format:       cfg.FeedList.Format,
		Database:     cfg.Database,
		Clean:        cfg.Render.DefaultClean,
	}

	// Override with command line flags if provided
	if renderMaxAge != "" {
		config.MaxAge = renderMaxAge
	}
	if renderStart != "" {
		config.Start = renderStart
	}
	if renderEnd != "" {
		config.End = renderEnd
	}
	if renderOutput != defaultOutputDir {
		config.OutputDir = renderOutput
	}
	if renderTemplates != "" {
		config.TemplatesDir = renderTemplates
	}
	if renderAssets != "" {
		config.AssetsDir = renderAssets
	}
	if renderFeeds != "" {
		config.FeedsFile = renderFeeds
	}
	if renderFormat != defaultFormat {
		config.Format = renderFormat
	}
	if renderClean {
		config.Clean = renderClean
	}

	return config
}

func validateRenderConfig(config *renderer.WorkflowConfig) error {
	return validateRenderParams(config.MaxAge, config.Start, config.End,
		config.OutputDir, config.TemplatesDir, config.AssetsDir,
		config.FeedsFile, config.Format)
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
