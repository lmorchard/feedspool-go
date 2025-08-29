package config

import (
	"testing"
	"time"
)

func TestGetDefault(t *testing.T) {
	cfg := GetDefault()

	tests := []struct {
		name     string
		actual   interface{}
		expected interface{}
	}{
		{"Database", cfg.Database, "./feeds.db"},
		{"Concurrency", cfg.Concurrency, 32},
		{"Timeout", cfg.Timeout, 30 * time.Second},
		{"MaxItems", cfg.MaxItems, 100},
		{"Verbose", cfg.Verbose, false},
		{"Debug", cfg.Debug, false},
		{"JSON", cfg.JSON, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.actual != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.actual, tt.expected)
			}
		})
	}
}
