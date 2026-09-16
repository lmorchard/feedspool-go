// Package embed turns item text into vector embeddings.
//
// Local and hosted providers are the same code path, as long as the endpoint
// speaks Ollama's /api/embed shape: a local model is just a provider whose
// base URL is localhost. OpenAI's embeddings API has a different request and
// response shape and is not supported. In-process inference is deliberately
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

// charsPerToken is a deliberately rough characters-to-tokens estimate, used
// only to size the input cap below. Real tokenizers vary; over-estimating the
// token count (by assuming few characters per token) truncates more than
// strictly necessary, which is the safe direction.
const charsPerToken = 3

// MaxInputChars is the cap applied to each item's text before it is sent.
//
// Truncating here rather than letting the provider handle over-length input is
// not defensive tidiness -- it is required. Measured against Ollama 0.32.0:
//
//   - qwen3-embedding:0.6b with num_ctx 8192 FAILS on long inputs, returning
//     HTTP 400 "do embedding request: EOF" (its runner dies). The threshold is
//     not even stable: a 12,440-char item failed alone in one run while a
//     binary search put the limit near 24,891 chars in another.
//   - the same model with num_ctx 2048 accepts 200,000 characters happily,
//     truncating internally.
//   - nomic-embed-text is reliable at both settings.
//
// So a provider's behavior above its configured context is undocumented and
// inconsistent, and a real corpus contains items long enough to hit it -- the
// reference spool's largest item is ~57,000 characters. Capping the input to
// what the context can hold makes the behavior ours, explicit and testable,
// and costs nothing: the model cannot attend to more than num_ctx tokens
// regardless.
func (d ModelDefaults) MaxInputChars() int {
	return d.NumCtx * charsPerToken
}

// Context windows are set explicitly because Ollama's own defaults are lower
// than the models support: its nomic card caps context at 2K unless num_ctx is
// sent, and 2.3% of a real 2-day window of feed items exceeds 2048 tokens --
// the longest and most topically distinctive ones.
//
// These are not each model's maximum, they are the largest value each was
// measured to handle reliably, and they also size MaxInputChars above.
//
// nomic is reliable at 8192, its true native window, which covers 99.7% of
// items in the reference corpus.
//
// qwen3 advertises 32768 but is NOT reliable above 2048 on Ollama 0.32.0: at
// num_ctx 8192 its runner dies on long inputs with HTTP 400 "do embedding
// request: EOF", at an unstable threshold. At 2048 it is solid. So 2048 it is,
// which truncates the ~2.3% of items longer than that rather than failing the
// run. A reliable smaller window beats a flaky larger one.
const (
	nomicNumCtx = 8192
	qwen3NumCtx = 2048
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

// ItemInput assembles the text handed to the model from an item's derived
// title, summary and body.
//
// This is the single definition of "what text represents this item for
// embedding", and it deliberately reads already-derived text rather than
// re-deriving it: internal/itemtext owns the HTML stripping and truncation, so
// the embedder and the full-text index cannot disagree about an item's content.
//
// Changing this assembly changes the vector for unchanged item text, so it is
// covered by Version above -- bump Version when this changes.
func ItemInput(title, summary, body string) string {
	parts := make([]string, 0, 3)
	for _, part := range []string{title, summary, body} {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, "\n\n")
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

	// NaN and Inf must be rejected explicitly, before the tolerance test.
	// Every comparison against NaN is false, so `math.Abs(NaN-1) > tolerance`
	// does not fire and a non-finite vector would pass -- then get stored, then
	// produce NaN similarities, and finally fail at JSON encode time in
	// `related --json`, a long way from the cause.
	if math.IsNaN(norm) || math.IsInf(norm, 0) {
		return fmt.Errorf(
			"embedding contains a non-finite value (norm %v): the model returned "+
				"NaN or Inf, which would silently poison every similarity it takes part in", norm,
		)
	}

	if math.Abs(norm-1) > normTolerance {
		return fmt.Errorf(
			"embedding is not unit length (norm %.6f): similarity assumes normalized vectors, "+
				"so this model needs explicit normalization before its vectors can be stored", norm,
		)
	}
	return nil
}
