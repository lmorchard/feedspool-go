package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/renderer"
	"github.com/sirupsen/logrus"
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

	// Get values from viper (includes config file values)
	targetTemplatesDir := viper.GetString("init.templates_dir")
	targetAssetsDir := viper.GetString("init.assets_dir")

	// Override with command line flags if provided
	if templatesDir != "./templates" {
		targetTemplatesDir = templatesDir
	}
	if assetsDir != "./assets" {
		targetAssetsDir = assetsDir
	}

	// Check if we're doing database initialization (default when no extraction flags are used)
	databaseInit := !extractTemplates && !extractAssets

	// Database initialization logic
	if databaseInit {
		if _, err := os.Stat(cfg.Database); err == nil && !upgradeFlag {
			return fmt.Errorf("database already exists at %s. Use --upgrade to upgrade existing database", cfg.Database)
		}

		if err := database.Connect(cfg.Database); err != nil {
			return fmt.Errorf("failed to connect to database: %w", err)
		}
		defer database.Close()

		if err := database.InitSchema(); err != nil {
			return fmt.Errorf("failed to initialize schema: %w", err)
		}

		if !cfg.JSON {
			if upgradeFlag {
				version, err := database.GetMigrationVersion()
				if err != nil {
					logrus.Warnf("Could not get migration version: %v", err)
				} else {
					logrus.Infof("Current database version: %d", version)
				}
				fmt.Println("Database schema upgraded successfully")
			} else {
				fmt.Println("Database initialized successfully")
			}
		}
	}

	// Template extraction
	if extractTemplates {
		if err := extractTemplateFiles(targetTemplatesDir, cfg.JSON); err != nil {
			return fmt.Errorf("failed to extract templates: %w", err)
		}
	}

	// Asset extraction
	if extractAssets {
		if err := extractAssetFiles(targetAssetsDir, cfg.JSON); err != nil {
			return fmt.Errorf("failed to extract assets: %w", err)
		}
	}

	// JSON output for extraction results
	if cfg.JSON && (extractTemplates || extractAssets) {
		result := map[string]interface{}{
			"success": true,
		}
		if databaseInit {
			result["database"] = cfg.Database
			if upgradeFlag {
				version, _ := database.GetMigrationVersion()
				result["action"] = actionUpgrade
				result["version"] = version
			} else {
				result["action"] = actionInitialize
			}
		}
		if extractTemplates {
			result["templates_extracted"] = targetTemplatesDir
		}
		if extractAssets {
			result["assets_extracted"] = targetAssetsDir
		}
		jsonData, _ := json.Marshal(result)
		fmt.Println(string(jsonData))
	} else if cfg.JSON && databaseInit {
		result := map[string]interface{}{
			"success":  true,
			"database": cfg.Database,
		}
		if upgradeFlag {
			version, _ := database.GetMigrationVersion()
			result["action"] = actionUpgrade
			result["version"] = version
		} else {
			result["action"] = actionInitialize
		}
		jsonData, _ := json.Marshal(result)
		fmt.Println(string(jsonData))
	}

	return nil
}

func extractTemplateFiles(targetDir string, jsonOutput bool) error {
	// Check if target directory exists and prompt for confirmation
	if _, err := os.Stat(targetDir); err == nil {
		if !jsonOutput {
			fmt.Printf("Directory %s already exists. Files may be overwritten.\n", targetDir)
			if !confirmExtraction() {
				fmt.Println("Template extraction canceled")
				return nil
			}
		}
	}

	if err := renderer.ExtractTemplates(targetDir); err != nil {
		return err
	}

	if !jsonOutput {
		fmt.Printf("Templates extracted to: %s\n", targetDir)
		fmt.Println("You can now customize these templates and use them with:")
		fmt.Printf("  feedspool render --templates %s\n", targetDir)
	}

	return nil
}

func extractAssetFiles(targetDir string, jsonOutput bool) error {
	// Check if target directory exists and prompt for confirmation
	if _, err := os.Stat(targetDir); err == nil {
		if !jsonOutput {
			fmt.Printf("Directory %s already exists. Files may be overwritten.\n", targetDir)
			if !confirmExtraction() {
				fmt.Println("Asset extraction canceled")
				return nil
			}
		}
	}

	if err := renderer.ExtractAssets(targetDir); err != nil {
		return err
	}

	if !jsonOutput {
		fmt.Printf("Assets extracted to: %s\n", targetDir)
		fmt.Println("You can now customize these assets and use them with:")
		fmt.Printf("  feedspool render --assets %s\n", targetDir)
	}

	return nil
}

func confirmExtraction() bool {
	fmt.Print("Continue? [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}
