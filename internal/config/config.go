package config

import (
	"time"

	"github.com/spf13/viper"
)

const (
	defaultPort      = 8080
	defaultOutputDir = "./build"
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
	Render      RenderConfig
	Serve       ServeConfig
	Init        InitConfig
}

type FeedListConfig struct {
	Format   string
	Filename string
}

type RenderConfig struct {
	OutputDir     string
	TemplatesDir  string
	AssetsDir     string
	DefaultMaxAge string
}

type ServeConfig struct {
	Port int
	Dir  string
}

type InitConfig struct {
	TemplatesDir string
	AssetsDir    string
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
		Render: RenderConfig{
			OutputDir:     viper.GetString("render.output_dir"),
			TemplatesDir:  viper.GetString("render.templates_dir"),
			AssetsDir:     viper.GetString("render.assets_dir"),
			DefaultMaxAge: viper.GetString("render.default_max_age"),
		},
		Serve: ServeConfig{
			Port: viper.GetInt("serve.port"),
			Dir:  viper.GetString("serve.dir"),
		},
		Init: InitConfig{
			TemplatesDir: viper.GetString("init.templates_dir"),
			AssetsDir:    viper.GetString("init.assets_dir"),
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
		Render: RenderConfig{
			OutputDir:     "./build",
			TemplatesDir:  "",
			AssetsDir:     "",
			DefaultMaxAge: "24h",
		},
		Serve: ServeConfig{
			Port: defaultPort,
			Dir:  defaultOutputDir,
		},
		Init: InitConfig{
			TemplatesDir: "./templates",
			AssetsDir:    "./assets",
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
