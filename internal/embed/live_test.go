package embed

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/httpclient"
)

// TestLiveOllama exercises a real local Ollama. It is skipped unless
// FEEDSPOOL_EMBED_LIVE is set, so CI stays hermetic and `make test` needs no
// model downloads.
//
//	FEEDSPOOL_EMBED_LIVE=1 go test ./internal/embed -run TestLiveOllama -v
//
// What it actually protects: the dimensions and the unit-norm assumption are
// facts about these models measured once by hand. This is what notices if a
// model update changes either, rather than having a stale number in a comment
// be the only record.
func TestLiveOllama(t *testing.T) {
	if os.Getenv("FEEDSPOOL_EMBED_LIVE") == "" {
		t.Skip("set FEEDSPOOL_EMBED_LIVE=1 with a local Ollama running to exercise this")
	}

	baseURL := os.Getenv("FEEDSPOOL_EMBED_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	tests := []struct {
		model    string
		wantDims int
	}{
		{ModelNomicEmbedText, 768},
		{testModelQwen3Tagged, 1024},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			provider := NewOllamaProvider(
				Config{BaseURL: baseURL, Model: tt.model},
				httpclient.NewClient(&httpclient.Config{Timeout: 2 * time.Minute}),
			)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			// More than one text so batching runs against the real server too.
			vectors, err := provider.Embed(ctx, []string{
				"Rust 1.87 released with stabilized async closures",
				"A measles outbreak in west Texas reaches 300 cases",
			})
			if err != nil {
				t.Fatalf("Embed against live Ollama: %v", err)
			}
			if len(vectors) != 2 {
				t.Fatalf("got %d vectors, want 2", len(vectors))
			}
			if len(vectors[0]) != tt.wantDims {
				t.Errorf("model %s returned %d dimensions, want %d",
					tt.model, len(vectors[0]), tt.wantDims)
			}
			// Embed only norm-checks the first vector of the first batch, so
			// check the second here: this is the assumption the storage design
			// rests on.
			if err := CheckUnitNorm(vectors[1]); err != nil {
				t.Errorf("second vector from %s: %v", tt.model, err)
			}
			t.Logf("%s returned %d dimensions, both vectors unit length", tt.model, len(vectors[0]))
		})
	}
}
