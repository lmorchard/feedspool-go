package embed

import (
	"math"
	"testing"
)

// Shared across the package's test files. The tagged model name is the form a
// user actually configures; the plain family names come from embed.go.
const (
	testModelQwen3Tagged = "qwen3-embedding:0.6b"
	testTextHello        = "hello #0"
	testTextX            = "x #0"
	wantEmbeddingWord    = "embedding"
)

// This is the one place the literal prefix, context-window and batch-size
// values are pinned. It deliberately uses string and number literals rather
// than the package's own constants: asserting nomicPrefix == nomicPrefix would
// pass no matter what the value became. Other tests use the constants, because
// they check that a prefix is applied rather than what it is.
func TestDefaultsForKnownModels(t *testing.T) {
	tests := []struct {
		model     string
		prefix    string
		numCtx    int
		batchSize int
	}{
		// nomic ships a dedicated "clustering:" task prefix, which is why it is
		// the default model for this feature. Its NumCtx is deliberately 8192
		// (the model's true native window) and not Ollama's 2K card default:
		// 2.3% of a real 2-day window exceeds 2048 tokens.
		{"nomic-embed-text", "clustering: ", 8192, 8},
		{"qwen3-embedding", "", 2048, 64},
		{"embeddinggemma", "title: none | text: ", 2048, 32},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := DefaultsFor(tt.model)
			if got.Prefix != tt.prefix {
				t.Errorf("Prefix = %q, want %q", got.Prefix, tt.prefix)
			}
			if got.NumCtx != tt.numCtx {
				t.Errorf("NumCtx = %d, want %d", got.NumCtx, tt.numCtx)
			}
			if got.BatchSize != tt.batchSize {
				t.Errorf("BatchSize = %d, want %d", got.BatchSize, tt.batchSize)
			}
		})
	}
}

// Ollama model names carry a ":tag", and the defaults are per model family,
// not per tag: "qwen3-embedding:0.6b" and "qwen3-embedding:8b" want the same
// prefix and context window.
func TestDefaultsForStripsOllamaTag(t *testing.T) {
	for _, model := range []string{"qwen3-embedding:0.6b", "qwen3-embedding:8b", "qwen3-embedding:latest"} {
		t.Run(model, func(t *testing.T) {
			if got := DefaultsFor(model); got != DefaultsFor("qwen3-embedding") {
				t.Errorf("DefaultsFor(%q) = %+v, want the untagged defaults %+v",
					model, got, DefaultsFor("qwen3-embedding"))
			}
		})
	}
}

// An uncharacterized model must still work rather than erroring: a
// conservative profile beats refusing to run.
func TestDefaultsForUnknownModelFallsBack(t *testing.T) {
	got := DefaultsFor("some-model-nobody-has-measured")
	if got.Prefix != "" {
		t.Errorf("Prefix = %q, want empty for an unknown model", got.Prefix)
	}
	if got.NumCtx <= 0 {
		t.Errorf("NumCtx = %d, want a positive fallback", got.NumCtx)
	}
	if got.BatchSize <= 0 {
		t.Errorf("BatchSize = %d, want a positive fallback", got.BatchSize)
	}
}

func TestCheckUnitNorm(t *testing.T) {
	// 1/sqrt(4) across four components is a unit vector.
	half := float32(0.5)
	unit := []float32{half, half, half, half}

	tests := []struct {
		name    string
		vector  []float32
		wantErr bool
	}{
		{"one-hot is unit length", []float32{1, 0, 0}, false},
		{"evenly spread unit vector", unit, false},
		{"just inside tolerance", []float32{1.0005}, false},
		{"double length is rejected", []float32{2, 0, 0}, true},
		{"half length is rejected", []float32{0.5, 0, 0}, true},
		{"zero vector is rejected", []float32{0, 0, 0}, true},
		{"empty vector is rejected", []float32{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckUnitNorm(tt.vector)
			if tt.wantErr && err == nil {
				t.Fatalf("CheckUnitNorm(%v) = nil, want an error", tt.vector)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("CheckUnitNorm(%v) = %v, want nil", tt.vector, err)
			}
		})
	}
}

// Guard the tolerance itself: a vector far enough off unit length to matter for
// similarity must be rejected, and the test above would still pass if the
// tolerance were widened to uselessness.
func TestCheckUnitNormToleranceIsTight(t *testing.T) {
	if err := CheckUnitNorm([]float32{float32(math.Sqrt(1.1))}); err == nil {
		t.Fatal("a vector with norm ~1.049 was accepted; the tolerance is too loose to catch an unnormalized provider")
	}
}

// Non-finite values need an explicit check, not the tolerance test: every
// comparison against NaN is false, so math.Abs(NaN-1) > tolerance does not
// fire and a NaN vector would pass. It would then be stored, produce NaN
// similarities, and finally fail at JSON encode time in `related --json`,
// a long way from where it went wrong.
func TestCheckUnitNormRejectsNonFinite(t *testing.T) {
	tests := []struct {
		name   string
		vector []float32
	}{
		{"NaN component", []float32{float32(math.NaN()), 0, 0}},
		{"NaN among real components", []float32{0.5, 0.5, float32(math.NaN())}},
		{"positive infinity", []float32{float32(math.Inf(1)), 0, 0}},
		{"negative infinity", []float32{float32(math.Inf(-1)), 0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CheckUnitNorm(tt.vector); err == nil {
				t.Fatalf("CheckUnitNorm(%v) = nil, want an error", tt.vector)
			}
		})
	}
}
