package config

import (
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Database    string
	Verbose     bool
	Debug       bool
	JSON        bool
	Concurrency int
	Timeout     time.Duration
	MaxItems    int
	FeedList    FeedListConfig
}

type FeedListConfig struct {
	Format   string
	Filename string
}

func LoadConfig() *Config {
	timeoutStr := viper.GetString("timeout")
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		timeout = 30 * time.Second
	}

	return &Config{
		Database:    viper.GetString("database"),
		Verbose:     viper.GetBool("verbose"),
		Debug:       viper.GetBool("debug"),
		JSON:        viper.GetBool("json"),
		Concurrency: viper.GetInt("concurrency"),
		Timeout:     timeout,
		MaxItems:    viper.GetInt("max_items"),
		FeedList: FeedListConfig{
			Format:   viper.GetString("feedlist.format"),
			Filename: viper.GetString("feedlist.filename"),
		},
	}
}

func GetDefault() *Config {
	return &Config{
		Database:    "./feeds.db",
		Concurrency: 32,
		Timeout:     30 * time.Second,
		MaxItems:    100,
		FeedList: FeedListConfig{
			Format:   "", // Empty strings indicate not configured
			Filename: "",
		},
	}
}

// HasDefaultFeedList returns true if both format and filename are configured.
func (c *Config) HasDefaultFeedList() bool {
	return c.FeedList.Format != "" && c.FeedList.Filename != ""
}

// GetDefaultFeedList returns the configured default format and filename.
func (c *Config) GetDefaultFeedList() (format, filename string) {
	return c.FeedList.Format, c.FeedList.Filename
}
