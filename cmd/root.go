package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	cfg     *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "feedspool",
	Short: "feedspool - RSS/Atom feed management CLI",
	Long: `feedspool is a CLI tool for managing RSS and Atom feeds with subscription management.

Features:
• Unified feed fetching from single URLs, OPML files, or text lists
• Subscribe/unsubscribe commands with RSS/Atom autodiscovery
• Export database feeds to OPML or text formats
• Feed list cleanup and age-based purging
• Concurrent fetching with HTTP caching
• Configurable defaults for streamlined workflows

Use 'feedspool <command> --help' for detailed command information.`,
	PersistentPreRun: func(_ *cobra.Command, _ []string) {
		initConfig()
		setupLogging()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (default is ./feedspool.yaml)")
	rootCmd.PersistentFlags().StringP("database", "d", "./feeds.db", "database file path")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().Bool("debug", false, "debug output")
	rootCmd.PersistentFlags().Bool("json", false, "JSON output format")

	_ = viper.BindPFlag("database", rootCmd.PersistentFlags().Lookup("database"))
	_ = viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	_ = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))
	_ = viper.BindPFlag("json", rootCmd.PersistentFlags().Lookup("json"))
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName("feedspool")
	}

	viper.SetDefault("database", "./feeds.db")
	viper.SetDefault("concurrency", 32)
	viper.SetDefault("timeout", "30s")
	viper.SetDefault("max_items", 100)

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		var configNotFoundErr viper.ConfigFileNotFoundError
		if errors.As(err, &configNotFoundErr) {
			// Config file not found - only warn if explicitly specified
			if cfgFile != "" {
				logrus.WithError(err).Warn("Specified config file not found")
			}
		} else {
			// Other error (parsing, permissions, etc.) - only warn if config file exists
			if cfgFile != "" {
				logrus.WithError(err).Warn("Error reading specified config file")
			}
		}
	}

	cfg = config.LoadConfig()
}

func setupLogging() {
	if cfg.Debug {
		logrus.SetLevel(logrus.DebugLevel)
	} else if cfg.Verbose {
		logrus.SetLevel(logrus.InfoLevel)
	} else {
		logrus.SetLevel(logrus.WarnLevel)
	}

	if cfg.JSON {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	} else {
		logrus.SetFormatter(&logrus.TextFormatter{
			DisableTimestamp: true,
		})
	}
}

func GetConfig() *config.Config {
	if cfg == nil {
		cfg = config.LoadConfig()
	}
	return cfg
}
