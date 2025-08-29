package cmd

import (
	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/initialize"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	upgradeFlag      bool
	extractTemplates bool
	extractAssets    bool
	templatesDir     string
	assetsDir        string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the database and optionally extract templates/assets",
	Long: `Initialize the database and optionally extract embedded templates and assets.

Database initialization:
  feedspool init                    # Create new database
  feedspool init --upgrade          # Upgrade existing database schema

Template and asset extraction:
  feedspool init --extract-templates           # Extract to ./templates/
  feedspool init --extract-assets              # Extract to ./assets/
  feedspool init --extract-templates --extract-assets  # Extract both

Custom directories:
  feedspool init --extract-templates --templates-dir ./custom-templates
  feedspool init --extract-assets --assets-dir ./custom-assets

You can combine database initialization with template extraction in a single command.
Extracted files can be customized and used with the render command's --templates
and --assets flags.`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().BoolVar(&upgradeFlag, "upgrade", false, "Upgrade existing database schema")
	initCmd.Flags().BoolVar(&extractTemplates, "extract-templates", false, "Extract embedded templates to filesystem")
	initCmd.Flags().BoolVar(&extractAssets, "extract-assets", false, "Extract embedded static assets to filesystem")
	initCmd.Flags().StringVar(&templatesDir, "templates-dir", "./templates", "Directory for template extraction")
	initCmd.Flags().StringVar(&assetsDir, "assets-dir", "./assets", "Directory for asset extraction")

	// Bind flags to viper for config file support
	_ = viper.BindPFlag("init.templates_dir", initCmd.Flags().Lookup("templates-dir"))
	_ = viper.BindPFlag("init.assets_dir", initCmd.Flags().Lookup("assets-dir"))

	rootCmd.AddCommand(initCmd)
}

func runInit(_ *cobra.Command, _ []string) error {
	cfg := GetConfig()

	// Build configuration from flags and config file
	config := buildInitConfig(cfg)

	// Execute the initialization operation
	return initialize.Execute(config)
}

func buildInitConfig(cfg *config.Config) *initialize.Config {
	// Get values from viper (includes config file values)
	config := &initialize.Config{
		Database:         cfg.Database,
		Upgrade:          upgradeFlag,
		ExtractTemplates: extractTemplates,
		ExtractAssets:    extractAssets,
		TemplatesDir:     viper.GetString("init.templates_dir"),
		AssetsDir:        viper.GetString("init.assets_dir"),
		JSONOutput:       cfg.JSON,
	}

	// Override with command line flags if provided
	if templatesDir != "./templates" {
		config.TemplatesDir = templatesDir
	}
	if assetsDir != "./assets" {
		config.AssetsDir = assetsDir
	}

	return config
}
