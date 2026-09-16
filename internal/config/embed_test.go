package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestGetDefaultEmbed(t *testing.T) {
	cfg := GetDefault()

	tests := []struct {
		name     string
		actual   interface{}
		expected interface{}
	}{
		{"Embed.BaseURL", cfg.Embed.BaseURL, DefaultEmbedBaseURL},
		{"Embed.Model", cfg.Embed.Model, DefaultEmbedModel},
		// Zero means "use the model's own measured default", which differs per
		// model -- nomic plateaus at batch 8, qwen3 keeps scaling to 64, and
		// their context windows are 8192 and 32768. Baking one number in here
		// would override that for every model.
		{"Embed.BatchSize", cfg.Embed.BatchSize, 0},
		{"Embed.NumCtx", cfg.Embed.NumCtx, 0},
		{"Embed.APIKey", cfg.Embed.APIKey, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.actual != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.actual, tt.expected)
			}
		})
	}
}

func TestLoadConfigReadsEmbedSettings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("timeout", "30s")
	viper.Set("embed.base_url", "https://api.example.com/v1")
	viper.Set("embed.model", "qwen3-embedding:0.6b")
	viper.Set("embed.batch_size", 64)
	viper.Set("embed.num_ctx", 32768)
	viper.Set("embed.api_key", "not-a-real-key")

	cfg := LoadConfig()

	if cfg.Embed.BaseURL != "https://api.example.com/v1" {
		t.Errorf("BaseURL = %q", cfg.Embed.BaseURL)
	}
	if cfg.Embed.Model != "qwen3-embedding:0.6b" {
		t.Errorf("Model = %q", cfg.Embed.Model)
	}
	if cfg.Embed.BatchSize != 64 {
		t.Errorf("BatchSize = %d, want 64", cfg.Embed.BatchSize)
	}
	if cfg.Embed.NumCtx != 32768 {
		t.Errorf("NumCtx = %d, want 32768", cfg.Embed.NumCtx)
	}
	if cfg.Embed.APIKey != "not-a-real-key" {
		t.Errorf("APIKey = %q", cfg.Embed.APIKey)
	}
}

// An unset batch size or context window must stay zero rather than becoming
// some other number, because zero is the signal that means "defer to the
// model".
func TestLoadConfigLeavesEmbedOverridesUnsetAtZero(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("timeout", "30s")

	cfg := LoadConfig()

	if cfg.Embed.BatchSize != 0 {
		t.Errorf("BatchSize = %d with nothing configured, want 0", cfg.Embed.BatchSize)
	}
	if cfg.Embed.NumCtx != 0 {
		t.Errorf("NumCtx = %d with nothing configured, want 0", cfg.Embed.NumCtx)
	}
}
