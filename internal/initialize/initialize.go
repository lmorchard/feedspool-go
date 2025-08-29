package initialize

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/renderer"
)

// Config holds all configuration for initialization operations.
type Config struct {
	Database         string
	Upgrade          bool
	ExtractTemplates bool
	ExtractAssets    bool
	TemplatesDir     string
	AssetsDir        string
	JSONOutput       bool
}

// Execute performs the complete initialization operation.
func Execute(config *Config) error {
	// Determine what operations to perform
	databaseInit := !config.ExtractTemplates && !config.ExtractAssets

	// Database initialization
	if databaseInit {
		if err := initializeDatabase(config); err != nil {
			return err
		}
	}

	// Template extraction
	if config.ExtractTemplates {
		if err := extractTemplateFiles(config.TemplatesDir, config.JSONOutput); err != nil {
			return fmt.Errorf("failed to extract templates: %w", err)
		}
	}

	// Asset extraction
	if config.ExtractAssets {
		if err := extractAssetFiles(config.AssetsDir, config.JSONOutput); err != nil {
			return fmt.Errorf("failed to extract assets: %w", err)
		}
	}

	// JSON output for extraction results
	if config.JSONOutput {
		return outputJSON(config, databaseInit)
	}

	return nil
}

func initializeDatabase(config *Config) error {
	if _, err := os.Stat(config.Database); err == nil && !config.Upgrade {
		return fmt.Errorf("database already exists at %s. Use --upgrade to upgrade existing database", config.Database)
	}

	if err := database.Connect(config.Database); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer database.Close()

	if err := database.InitSchema(); err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	if !config.JSONOutput {
		printDatabaseResult(config.Upgrade)
	}

	return nil
}

func extractTemplateFiles(targetDir string, jsonOutput bool) error {
	// Check if target directory exists and prompt for confirmation
	if _, err := os.Stat(targetDir); err == nil {
		if !jsonOutput {
			//nolint:forbidigo // User-facing output
			fmt.Printf("Directory %s already exists. Files may be overwritten.\n", targetDir)
			if !confirmExtraction() {
				fmt.Println("Template extraction canceled") //nolint:forbidigo // User-facing output
				return nil
			}
		}
	}

	if err := renderer.ExtractTemplates(targetDir); err != nil {
		return err
	}

	if !jsonOutput {
		fmt.Printf("Templates extracted to: %s\n", targetDir)                   //nolint:forbidigo // User-facing output
		fmt.Println("You can now customize these templates and use them with:") //nolint:forbidigo // User-facing output
		fmt.Printf("  feedspool render --templates %s\n", targetDir)            //nolint:forbidigo // User-facing output
	}

	return nil
}

func extractAssetFiles(targetDir string, jsonOutput bool) error {
	// Check if target directory exists and prompt for confirmation
	if _, err := os.Stat(targetDir); err == nil {
		if !jsonOutput {
			//nolint:forbidigo // User-facing output
			fmt.Printf("Directory %s already exists. Files may be overwritten.\n", targetDir)
			if !confirmExtraction() {
				fmt.Println("Asset extraction canceled") //nolint:forbidigo // User-facing output
				return nil
			}
		}
	}

	if err := renderer.ExtractAssets(targetDir); err != nil {
		return err
	}

	if !jsonOutput {
		fmt.Printf("Assets extracted to: %s\n", targetDir)                   //nolint:forbidigo // User-facing output
		fmt.Println("You can now customize these assets and use them with:") //nolint:forbidigo // User-facing output
		fmt.Printf("  feedspool render --assets %s\n", targetDir)            //nolint:forbidigo // User-facing output
	}

	return nil
}

func confirmExtraction() bool {
	fmt.Print("Continue? [y/N]: ") //nolint:forbidigo // User-facing interactive prompt
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

func printDatabaseResult(upgrade bool) {
	if upgrade {
		version, err := database.GetMigrationVersion()
		if err != nil {
			fmt.Printf("Could not get migration version: %v\n", err) //nolint:forbidigo // User-facing output
		} else {
			fmt.Printf("Current database version: %d\n", version) //nolint:forbidigo // User-facing output
		}
		fmt.Println("Database schema upgraded successfully") //nolint:forbidigo // User-facing output
	} else {
		fmt.Println("Database initialized successfully") //nolint:forbidigo // User-facing output
	}
}

func outputJSON(config *Config, databaseInit bool) error {
	result := map[string]any{
		"success": true,
	}

	if databaseInit {
		result["database"] = config.Database
		if config.Upgrade {
			version, _ := database.GetMigrationVersion()
			result["action"] = "upgrade"
			result["version"] = version
		} else {
			result["action"] = "initialize"
		}
	}

	if config.ExtractTemplates {
		result["templates_extracted"] = config.TemplatesDir
	}
	if config.ExtractAssets {
		result["assets_extracted"] = config.AssetsDir
	}

	jsonData, _ := json.Marshal(result)
	fmt.Println(string(jsonData)) //nolint:forbidigo // JSON output to stdout
	return nil
}
