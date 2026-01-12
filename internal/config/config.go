package config

import (
	"time"

	"github.com/spf13/viper"
)

// getIntWithDefault returns the viper int value or default if not set.
func getIntWithDefault(key string, defaultValue int) int {
	if viper.IsSet(key) {
		return viper.GetInt(key)
	}
	return defaultValue
}

const (
	defaultPort        = 8080
	defaultOutputDir   = "./build"
	DefaultTimeout     = 30 * time.Second
	DefaultConcurrency = 32
	DefaultMaxItems    = 100
	DefaultDirPerm     = 0o755
)

type Config struct {
	Database string
	Verbose  bool
	Debug    bool
	JSON     bool
	Timeout  time.Duration
	FeedList FeedListConfig
	Fetch    FetchConfig
	Render   RenderConfig
	Serve    ServeConfig
	Init     InitConfig
	Unfurl   UnfurlConfig
	Purge    PurgeConfig
}

type FeedListConfig struct {
	Format   string
	Filename string
}

type FetchConfig struct {
	WithUnfurl  bool `mapstructure:"with_unfurl"`
	Concurrency int  `mapstructure:"concurrency"`
	MaxItems    int  `mapstructure:"max_items"`
}

type RenderConfig struct {
	OutputDir           string
	TemplatesDir        string
	AssetsDir           string
	DefaultMaxAge       string
	DefaultClean        bool `mapstructure:"default_clean"`
	DefaultItemsPerFeed int  `mapstructure:"default_items_per_feed"`
}

type ServeConfig struct {
	Port int
	Dir  string
}

type InitConfig struct {
	TemplatesDir string
	AssetsDir    string
}

type UnfurlConfig struct {
	SkipRobots  bool          `mapstructure:"skip_robots"`
	RetryAfter  time.Duration `mapstructure:"retry_after"`
	Concurrency int           `mapstructure:"concurrency"`
}

type PurgeConfig struct {
	MaxAge       string `mapstructure:"max_age"`
	SkipVacuum   bool   `mapstructure:"skip_vacuum"`
	MinItemsKeep int    `mapstructure:"min_items_keep"`
}

func LoadConfig() *Config {
	timeoutStr := viper.GetString("timeout")
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		timeout = DefaultTimeout
	}

	return &Config{
		Database: viper.GetString("database"),
		Verbose:  viper.GetBool("verbose"),
		Debug:    viper.GetBool("debug"),
		JSON:     viper.GetBool("json"),
		Timeout:  timeout,
		FeedList: FeedListConfig{
			Format:   viper.GetString("feedlist.format"),
			Filename: viper.GetString("feedlist.filename"),
		},
		Fetch: FetchConfig{
			WithUnfurl:  viper.GetBool("fetch.with_unfurl"),
			Concurrency: getIntWithDefault("fetch.concurrency", DefaultConcurrency),
			MaxItems:    getIntWithDefault("fetch.max_items", DefaultMaxItems),
		},
		Render: RenderConfig{
			OutputDir:           viper.GetString("render.output_dir"),
			TemplatesDir:        viper.GetString("render.templates_dir"),
			AssetsDir:           viper.GetString("render.assets_dir"),
			DefaultMaxAge:       viper.GetString("render.default_max_age"),
			DefaultClean:        viper.GetBool("render.default_clean"),
			DefaultItemsPerFeed: getIntWithDefault("render.default_items_per_feed", 0),
		},
		Serve: ServeConfig{
			Port: viper.GetInt("serve.port"),
			Dir:  viper.GetString("serve.dir"),
		},
		Init: InitConfig{
			TemplatesDir: viper.GetString("init.templates_dir"),
			AssetsDir:    viper.GetString("init.assets_dir"),
		},
		Unfurl: UnfurlConfig{
			SkipRobots:  viper.GetBool("unfurl.skip_robots"),
			RetryAfter:  viper.GetDuration("unfurl.retry_after"),
			Concurrency: viper.GetInt("unfurl.concurrency"),
		},
		Purge: PurgeConfig{
			MaxAge:       viper.GetString("purge.max_age"),
			SkipVacuum:   viper.GetBool("purge.skip_vacuum"),
			MinItemsKeep: getIntWithDefault("purge.min_items_keep", 0),
		},
	}
}

func GetDefault() *Config {
	return &Config{
		Database: "./feeds.db",
		Timeout:  DefaultTimeout,
		FeedList: FeedListConfig{
			Format:   "", // Empty strings indicate not configured
			Filename: "",
		},
		Fetch: FetchConfig{
			WithUnfurl:  false, // Default to false
			Concurrency: DefaultConcurrency,
			MaxItems:    DefaultMaxItems,
		},
		Render: RenderConfig{
			OutputDir:           "./build",
			TemplatesDir:        "",
			AssetsDir:           "",
			DefaultMaxAge:       "24h",
			DefaultItemsPerFeed: 0, // 0 means no limit (show all items)
		},
		Serve: ServeConfig{
			Port: defaultPort,
			Dir:  defaultOutputDir,
		},
		Init: InitConfig{
			TemplatesDir: "./templates",
			AssetsDir:    "./assets",
		},
		Unfurl: UnfurlConfig{
			SkipRobots:  false,
			RetryAfter:  1 * time.Hour,
			Concurrency: DefaultConcurrency,
		},
		Purge: PurgeConfig{
			MaxAge:       "30d",
			MinItemsKeep: 0, // 0 means no minimum (delete all old items)
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
