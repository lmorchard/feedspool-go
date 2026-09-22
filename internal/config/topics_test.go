package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestGetDefaultTopicsLineage(t *testing.T) {
	cfg := GetDefault()

	if cfg.Topics.LineageLookback != 6 {
		t.Errorf("LineageLookback = %d, want 6", cfg.Topics.LineageLookback)
	}
	if cfg.Topics.LineageThreshold != 0.5 {
		t.Errorf("LineageThreshold = %v, want 0.5", cfg.Topics.LineageThreshold)
	}
	if cfg.Topics.InheritThreshold != 0.9 {
		t.Errorf("InheritThreshold = %v, want 0.9", cfg.Topics.InheritThreshold)
	}
	if cfg.Topics.GrowthMargin != 2 {
		t.Errorf("GrowthMargin = %d, want 2", cfg.Topics.GrowthMargin)
	}
}

func TestLoadConfigReadsTopicsLineage(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("timeout", "30s")
	viper.Set("topics.lineage_lookback", 3)
	viper.Set("topics.lineage_threshold", 0.6)
	viper.Set("topics.inherit_threshold", 0.95)
	viper.Set("topics.growth_margin", 3)

	cfg := LoadConfig()

	if cfg.Topics.LineageLookback != 3 {
		t.Errorf("LineageLookback = %d, want 3", cfg.Topics.LineageLookback)
	}
	if cfg.Topics.LineageThreshold != 0.6 {
		t.Errorf("LineageThreshold = %v, want 0.6", cfg.Topics.LineageThreshold)
	}
	if cfg.Topics.InheritThreshold != 0.95 {
		t.Errorf("InheritThreshold = %v, want 0.95", cfg.Topics.InheritThreshold)
	}
	if cfg.Topics.GrowthMargin != 3 {
		t.Errorf("GrowthMargin = %d, want 3", cfg.Topics.GrowthMargin)
	}
}

func TestLoadConfigDefaultsTopicsLineageWhenUnset(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("timeout", "30s")

	cfg := LoadConfig()

	if cfg.Topics.LineageLookback != DefaultTopicsLineageLookback {
		t.Errorf("LineageLookback = %d, want %d", cfg.Topics.LineageLookback, DefaultTopicsLineageLookback)
	}
	if cfg.Topics.LineageThreshold != DefaultTopicsLineageThreshold {
		t.Errorf("LineageThreshold = %v, want %v", cfg.Topics.LineageThreshold, DefaultTopicsLineageThreshold)
	}
	if cfg.Topics.InheritThreshold != DefaultTopicsInheritThreshold {
		t.Errorf("InheritThreshold = %v, want %v", cfg.Topics.InheritThreshold, DefaultTopicsInheritThreshold)
	}
	if cfg.Topics.GrowthMargin != DefaultTopicsGrowthMargin {
		t.Errorf("GrowthMargin = %d, want %d", cfg.Topics.GrowthMargin, DefaultTopicsGrowthMargin)
	}
}
