// Package embed turns item text into vector embeddings.
//
// Local and hosted providers are the same code path: a local model is just a
// provider whose base URL is localhost. In-process inference is deliberately
// not implemented here -- it is feasible cgo-free via hugot + GoMLX, but it
// forces a much weaker model, and Provider is transport-agnostic so it can be
// added later as a second implementation. See the issue #30 session notes.
package embed

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Version identifies this derivation in item_embeddings bookkeeping. Bump it
// whenever the text handed to the model changes for the same item text --
// which includes changing any Prefix below, because the prefix is part of what
// produced the vector.
const Version = 1

// Provider turns text into vectors.
//
// There is deliberately no Dims method: dimensionality is len(vectors[0]) from
// the first response. A separate probe would be a second source of truth, free
// to disagree with the vectors actually returned.
type Provider interface {
	ModelID() string
	// Embed accepts any number of texts and handles batching internally, so
	// callers never have to know the provider's batch size.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// ModelDefaults are the per-model knobs. Prefix is prepended to every input.
type ModelDefaults struct {
	Prefix    string
	NumCtx    int
	BatchSize int
}

// Context windows are each model's true native window, NOT what Ollama
// defaults to. Ollama's nomic card caps context at 2K unless num_ctx is sent,
// and 2.3% of a real 2-day window of feed items exceeds 2048 tokens -- the
// longest and most topically distinctive ones.
const (
	nomicNumCtx = 8192
	qwen3NumCtx = 32768
	gemmaNumCtx = 2048
)

// Batch sizes are where throughput stopped improving against an
// Apple-silicon Ollama: nomic plateaus around 8, qwen3 keeps scaling to 64.
const (
	nomicBatchSize = 8
	qwen3BatchSize = 64
	gemmaBatchSize = 32
)

const (
	// Conservative profile for a model nobody has characterized. Small enough
	// a batch to be safe against a short context, large enough to beat the
	// ~8x penalty of embedding one text per request.
	fallbackNumCtx    = 2048
	fallbackBatchSize = 16
)

// Prefixes are per-model input templates. nomic ships task prefixes and
// "clustering:" is one of them, which is why it is the default model for topic
// work. embeddinggemma wants a title/text framing. qwen3 wants an instruction
// preamble only for queries, so documents go in bare.
const (
	nomicPrefix = "clustering: "
	gemmaPrefix = "title: none | text: "
)

// Model family names, with no Ollama ":tag" -- the defaults are per family.
const (
	ModelNomicEmbedText = "nomic-embed-text"
	ModelQwen3Embedding = "qwen3-embedding"
	ModelEmbeddingGemma = "embeddinggemma"
)

// modelDefaults is a function rather than a package variable so the map cannot
// be mutated at a distance, following the getMigrations() precedent in
// internal/database. Keyed by model family, with any Ollama ":tag" stripped.
func modelDefaults() map[string]ModelDefaults {
	return map[string]ModelDefaults{
		ModelNomicEmbedText: {Prefix: nomicPrefix, NumCtx: nomicNumCtx, BatchSize: nomicBatchSize},
		ModelQwen3Embedding: {Prefix: "", NumCtx: qwen3NumCtx, BatchSize: qwen3BatchSize},
		ModelEmbeddingGemma: {Prefix: gemmaPrefix, NumCtx: gemmaNumCtx, BatchSize: gemmaBatchSize},
	}
}

// DefaultsFor returns the knobs for a model, falling back to a conservative
// profile rather than erroring: an uncharacterized model should still work.
func DefaultsFor(model string) ModelDefaults {
	if defaults, ok := modelDefaults()[baseModelName(model)]; ok {
		return defaults
	}
	return ModelDefaults{NumCtx: fallbackNumCtx, BatchSize: fallbackBatchSize}
}

// baseModelName strips an Ollama ":tag" suffix, so "qwen3-embedding:0.6b" and
// "qwen3-embedding:8b" resolve to the same family defaults.
func baseModelName(model string) string {
	if i := strings.Index(model, ":"); i >= 0 {
		return model[:i]
	}
	return model
}

// normTolerance is tight on purpose. Both models measured for issue #30 return
// vectors of norm 1.0000, so anything meaningfully off unit length means the
// provider changed behavior rather than that floating point drifted.
const normTolerance = 1e-3

// CheckUnitNorm verifies a provider returns L2-normalized vectors, which is
// what lets similarity be a plain dot product with no magnitude stored.
//
// Checked at the provider rather than in the decoder so a misbehaving model is
// caught when it misbehaves, instead of at some later read.
func CheckUnitNorm(vector []float32) error {
	if len(vector) == 0 {
		return errors.New("embedding is empty")
	}
	var sum float64
	for _, component := range vector {
		sum += float64(component) * float64(component)
	}
	norm := math.Sqrt(sum)
	if math.Abs(norm-1) > normTolerance {
		return fmt.Errorf(
			"embedding is not unit length (norm %.6f): similarity assumes normalized vectors, "+
				"so this model needs explicit normalization before its vectors can be stored", norm,
		)
	}
	return nil
}
