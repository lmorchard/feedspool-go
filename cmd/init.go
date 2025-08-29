package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var upgradeFlag bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the database",
	Long:  `Creates a new database with the required schema. Use --upgrade to upgrade an existing database.`,
	RunE:  runInit,
}

func init() {
	initCmd.Flags().BoolVar(&upgradeFlag, "upgrade", false, "Upgrade existing database schema")
	rootCmd.AddCommand(initCmd)
}

func runInit(_ *cobra.Command, _ []string) error {
	cfg := GetConfig()

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

	if cfg.JSON {
		result := map[string]interface{}{
			"success":  true,
			"database": cfg.Database,
		}
		if upgradeFlag {
			version, _ := database.GetMigrationVersion()
			result["action"] = "upgrade"
			result["version"] = version
		} else {
			result["action"] = "initialize"
		}
		jsonData, _ := json.Marshal(result)
		fmt.Println(string(jsonData))
	} else {
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

	return nil
}
