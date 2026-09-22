package config

import (
	"errors"
	"time"

	"github.com/spf13/viper"

	"github.com/lmorchard/feedspool-go/internal/lineage"
)

// getIntWithDefault returns the viper int value or default if not set.
func getIntWithDefault(key string, defaultValue int) int {
	if viper.IsSet(key) {
		return viper.GetInt(key)
	}
	return defaultValue
}

// getFloat64WithDefault returns the viper float64 value or default if not set.
func getFloat64WithDefault(key string, defaultValue float64) float64 {
	if viper.IsSet(key) {
		return viper.GetFloat64(key)
	}
	return defaultValue
}

// getStringWithDefault returns the viper string value or default if not set.
func getStringWithDefault(key, defaultValue string) string {
	if viper.IsSet(key) && viper.GetString(key) != "" {
		return viper.GetString(key)
	}
	return defaultValue
}

const (
	defaultPort                   = 8080
	defaultOutputDir              = "./build"
	DefaultTimeout                = 30 * time.Second
	DefaultConcurrency            = 32
	DefaultMaxItems               = 100
	DefaultDirPerm                = 0o755
	DefaultMinItemsPerFeed        = 5   // Render: minimum items to show per feed
	DefaultMaxItemsPerFeed        = 50  // Render: maximum items to show per feed
	DefaultMinItemsKeepPurge      = 10  // Purge: minimum items to keep per feed
	DefaultFeedsPerPage           = 25  // Render: feeds per page for pagination
	DefaultTopicMaxFeedRatio      = 0.8 // Render: Default max ratio of items from one feed before topic is rejected
	DefaultTopicMinDiversityCount = 2   // Render: Min topic size before applying diversity limits

	// DefaultEmbedBaseURL points at a local Ollama. A hosted provider is the
	// same code path with a different URL and an API key.
	DefaultEmbedBaseURL = "http://localhost:11434"
	// DefaultEmbedModel is nomic-embed-text: the smallest of the credible
	// options, Apache-2.0, and the only one shipping a dedicated "clustering:"
	// task prefix, which is what this feature is ultimately for.
	DefaultEmbedModel = "nomic-embed-text"
	DefaultEmbedLast  = "1w"

	// DefaultTopicsConcurrency restricts how many concurrent requests are sent
	// to the LLM when labeling clusters.
	DefaultTopicsConcurrency = 5
	DefaultTopicsMinItems    = 7
	DefaultTopicsMaxItems    = 100
	DefaultTopicsThreshold   = 0.70
	DefaultTopicsLast        = "1d"

	// Topic lineage: how a run finds the same topic in earlier runs and when it
	// keeps the earlier label. The numbers live in internal/lineage so the
	// migration backfill and live runs share one source; see there for why.
	DefaultTopicsLineageLookback  = lineage.DefaultLookback
	DefaultTopicsLineageThreshold = lineage.DefaultAttachThreshold
	DefaultTopicsInheritThreshold = lineage.DefaultInheritThreshold
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
	Embed    EmbedConfig
	Topics   TopicsConfig
	Build    BuildConfig
}

type FeedListConfig struct {
	Format   string
	Filename string
	Dir      string
}

type FetchConfig struct {
	WithUnfurl  bool `mapstructure:"with_unfurl"`
	Concurrency int  `mapstructure:"concurrency"`
	MaxItems    int  `mapstructure:"max_items"`
}

type RenderConfig struct {
	OutputDir              string
	TemplatesDir           string
	AssetsDir              string
	DefaultMaxAge          string
	DefaultClean           bool    `mapstructure:"default_clean"`
	DefaultMinItemsPerFeed int     `mapstructure:"default_min_items_per_feed"`
	DefaultMaxItemsPerFeed int     `mapstructure:"default_max_items_per_feed"`
	FeedsPerPage           int     `mapstructure:"feeds_per_page"`
	TopicMaxFeedRatio      float32 `mapstructure:"topic_max_feed_ratio"`
	TopicMinDiversityCount int     `mapstructure:"topic_min_diversity_count"`
}

type ServeConfig struct {
	Port int
	// Bind is the listen address. Empty means all interfaces, preserving the
	// behavior from before the option existed.
	Bind string
	Dir  string
	API  APIConfig
}

// APIConfig controls the JSON API mounted on the serve command.
type APIConfig struct {
	Enabled bool
	// Token is deliberately not exposed as a command-line flag: a token passed
	// on the command line ends up in ps output. Config file or
	// FEEDSPOOL_API_TOKEN only.
	Token string
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

type BuildConfig struct {
	SkipEmbed  bool `mapstructure:"skip_embed"`
	SkipTopics bool `mapstructure:"skip_topics"`
}

// EmbedConfig controls the embedding provider used by the embed and related
// commands.
//
// Local and hosted are the same code path: a local Ollama is just a BaseURL of
// localhost, and a hosted provider is a different BaseURL plus an APIKey.
type EmbedConfig struct {
	BaseURL string `mapstructure:"base_url"`
	Model   string `mapstructure:"model"`
	Last    string `mapstructure:"last"`
	// BatchSize and NumCtx are 0 by default, meaning "use the model's own
	// measured defaults" from internal/embed. Set them only to override.
	BatchSize int `mapstructure:"batch_size"`
	NumCtx    int `mapstructure:"num_ctx"`
	// APIKey is deliberately not exposed as a command-line flag: a token
	// passed on the command line ends up in ps output. Config file or
	// FEEDSPOOL_EMBED_API_KEY only. Same reasoning as APIConfig.Token above.
	APIKey string `mapstructure:"api_key"`
}

type TopicsConfig struct {
	BaseURL           string  `mapstructure:"base_url"`
	Model             string  `mapstructure:"model"`
	EmbedModel        string  `mapstructure:"embed_model"`
	APIKey            string  `mapstructure:"api_key"`
	Concurrency       int     `mapstructure:"concurrency"`
	MinItems          int     `mapstructure:"min_items"`
	MaxItems          int     `mapstructure:"max_items"`
	Threshold         float32 `mapstructure:"threshold"`
	Last              string  `mapstructure:"last"`
	MaxFeedRatio      float32 `mapstructure:"max_feed_ratio"`
	MinDiversityCount int     `mapstructure:"min_diversity_count"`
	LineageLookback   int     `mapstructure:"lineage_lookback"`
	LineageThreshold  float64 `mapstructure:"lineage_threshold"`
	InheritThreshold  float64 `mapstructure:"inherit_threshold"`
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
			Dir:      viper.GetString("feedlist.dir"),
		},
		Fetch: FetchConfig{
			WithUnfurl:  viper.GetBool("fetch.with_unfurl"),
			Concurrency: getIntWithDefault("fetch.concurrency", DefaultConcurrency),
			MaxItems:    getIntWithDefault("fetch.max_items", DefaultMaxItems),
		},
		Render: RenderConfig{
			OutputDir:              viper.GetString("render.output_dir"),
			TemplatesDir:           viper.GetString("render.templates_dir"),
			AssetsDir:              viper.GetString("render.assets_dir"),
			DefaultMaxAge:          viper.GetString("render.default_max_age"),
			DefaultClean:           viper.GetBool("render.default_clean"),
			DefaultMinItemsPerFeed: getIntWithDefault("render.default_min_items_per_feed", DefaultMinItemsPerFeed),
			DefaultMaxItemsPerFeed: getIntWithDefault("render.default_max_items_per_feed", DefaultMaxItemsPerFeed),
			FeedsPerPage:           getIntWithDefault("render.feeds_per_page", DefaultFeedsPerPage),
			TopicMaxFeedRatio:      float32(getFloat64WithDefault("render.topic_max_feed_ratio", DefaultTopicMaxFeedRatio)),
			TopicMinDiversityCount: getIntWithDefault("render.topic_min_diversity_count", DefaultTopicMinDiversityCount),
		},
		Serve: ServeConfig{
			Port: viper.GetInt("serve.port"),
			Bind: viper.GetString("serve.bind"),
			Dir:  viper.GetString("serve.dir"),
			API: APIConfig{
				Enabled: viper.GetBool("serve.api.enabled"),
				Token:   viper.GetString("serve.api.token"),
			},
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
		Embed: EmbedConfig{
			BaseURL:   viper.GetString("embed.base_url"),
			Model:     viper.GetString("embed.model"),
			Last:      getStringWithDefault("embed.last", DefaultEmbedLast),
			BatchSize: getIntWithDefault("embed.batch_size", 0),
			NumCtx:    getIntWithDefault("embed.num_ctx", 0),
			APIKey:    viper.GetString("embed.api_key"),
		},
		Topics: TopicsConfig{
			BaseURL:           viper.GetString("topics.base_url"),
			Model:             viper.GetString("topics.model"),
			EmbedModel:        viper.GetString("topics.embed_model"),
			APIKey:            viper.GetString("topics.api_key"),
			Concurrency:       getIntWithDefault("topics.concurrency", DefaultTopicsConcurrency),
			MinItems:          getIntWithDefault("topics.min_items", DefaultTopicsMinItems),
			MaxItems:          getIntWithDefault("topics.max_items", DefaultTopicsMaxItems),
			Threshold:         float32(getFloat64WithDefault("topics.threshold", DefaultTopicsThreshold)),
			Last:              getStringWithDefault("topics.last", DefaultTopicsLast),
			MaxFeedRatio:      float32(getFloat64WithDefault("topics.max_feed_ratio", float64(DefaultTopicMaxFeedRatio))),
			MinDiversityCount: getIntWithDefault("topics.min_diversity_count", DefaultTopicMinDiversityCount),
			LineageLookback:   getIntWithDefault("topics.lineage_lookback", DefaultTopicsLineageLookback),
			LineageThreshold:  getFloat64WithDefault("topics.lineage_threshold", DefaultTopicsLineageThreshold),
			InheritThreshold:  getFloat64WithDefault("topics.inherit_threshold", DefaultTopicsInheritThreshold),
		},
		Build: BuildConfig{
			SkipEmbed:  viper.GetBool("build.skip_embed"),
			SkipTopics: viper.GetBool("build.skip_topics"),
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
			Dir:      "",
		},
		Fetch: FetchConfig{
			WithUnfurl:  false, // Default to false
			Concurrency: DefaultConcurrency,
			MaxItems:    DefaultMaxItems,
		},
		Render: RenderConfig{
			OutputDir:              "./build",
			TemplatesDir:           "",
			AssetsDir:              "",
			DefaultMaxAge:          "24h",
			DefaultMinItemsPerFeed: DefaultMinItemsPerFeed,
			DefaultMaxItemsPerFeed: DefaultMaxItemsPerFeed,
			FeedsPerPage:           DefaultFeedsPerPage,
			TopicMaxFeedRatio:      DefaultTopicMaxFeedRatio,
			TopicMinDiversityCount: DefaultTopicMinDiversityCount,
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
			MinItemsKeep: DefaultMinItemsKeepPurge,
		},
		Embed: EmbedConfig{
			BaseURL: DefaultEmbedBaseURL,
			Model:   DefaultEmbedModel,
			Last:    DefaultEmbedLast,
			// Zero means "use the model's own measured default".
			BatchSize: 0,
			NumCtx:    0,
		},
		Topics: TopicsConfig{
			BaseURL:           "",
			Model:             "",
			Concurrency:       DefaultTopicsConcurrency,
			MinItems:          DefaultTopicsMinItems,
			MaxItems:          DefaultTopicsMaxItems,
			Threshold:         DefaultTopicsThreshold,
			Last:              DefaultTopicsLast,
			MaxFeedRatio:      DefaultTopicMaxFeedRatio,
			MinDiversityCount: DefaultTopicMinDiversityCount,
			LineageLookback:   DefaultTopicsLineageLookback,
			LineageThreshold:  DefaultTopicsLineageThreshold,
			InheritThreshold:  DefaultTopicsInheritThreshold,
		},
	}
}

// HasDefaultFeedList returns true if both format and filename are configured.
func (c *Config) HasDefaultFeedList() bool {
	return c.FeedList.Format != "" && c.FeedList.Filename != ""
}

// HasFeedListDir returns true if a feed list directory is configured.
func (c *Config) HasFeedListDir() bool {
	return c.FeedList.Dir != ""
}

// Validate reports configuration that cannot be acted on unambiguously.
func (c *Config) Validate() error {
	if c.FeedList.Dir != "" && c.FeedList.Filename != "" {
		return errors.New(
			"ambiguous config: set either feedlist.dir or feedlist.filename, not both",
		)
	}
	return nil
}

// GetDefaultFeedList returns the configured default format and filename.
func (c *Config) GetDefaultFeedList() (format, filename string) {
	return c.FeedList.Format, c.FeedList.Filename
}
