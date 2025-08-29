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
	}
}

func GetDefault() *Config {
	return &Config{
		Database:    "./feeds.db",
		Concurrency: 32,
		Timeout:     30 * time.Second,
		MaxItems:    100,
	}
}
