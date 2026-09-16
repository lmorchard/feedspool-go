package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lmorchard/feedspool-go/internal/httpclient"
)

// Config configures an Ollama-compatible embedding provider. A hosted
// OpenAI-compatible endpoint is the same thing with a different BaseURL and an
// APIKey, which is why there is only one implementation.
type Config struct {
	BaseURL string
	Model   string
	// APIKey is empty for a local Ollama. It is deliberately not settable from
	// a command-line flag anywhere upstream: a token passed on the command line
	// ends up in ps output.
	APIKey string
	// BatchSize and NumCtx fall back to the model's defaults when zero.
	BatchSize int
	NumCtx    int
	Timeout   time.Duration
}

// embedRequest is the Ollama POST /api/embed body. Input is an array because
// batching is worth 8-28x over one text per request.
type embedRequest struct {
	Model   string         `json:"model"`
	Input   []string       `json:"input"`
	Options map[string]any `json:"options,omitempty"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// OllamaProvider implements Provider against Ollama's /api/embed.
type OllamaProvider struct {
	client        *httpclient.Client
	baseURL       string
	model         string
	apiKey        string
	prefix        string
	numCtx        int
	batchSize     int
	maxInputChars int

	// The unit-norm assumption is checked once per provider rather than per
	// vector: it is a property of the model, and checking 1,898 vectors per
	// run to learn the same fact would be waste.
	normOnce sync.Once
	normErr  error
}

// NewOllamaProvider builds a provider, filling unset knobs from the model's
// measured defaults.
func NewOllamaProvider(cfg Config, client *httpclient.Client) *OllamaProvider {
	defaults := DefaultsFor(cfg.Model)

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = defaults.BatchSize
	}
	numCtx := cfg.NumCtx
	if numCtx <= 0 {
		numCtx = defaults.NumCtx
	}

	// The input cap tracks whatever num_ctx ends up being, including a
	// configured override, since it exists to keep inputs inside the context
	// the provider was actually told to use.
	//
	// Floored at the prefix plus minInputChars so the per-item budget can
	// never reach zero or below. truncateRunes treats a non-positive cap as
	// "no limit", which is the unsafe direction: a tiny configured num_ctx
	// would otherwise send whole untruncated items, which is the failure this
	// cap exists to prevent.
	maxInputChars := max(
		ModelDefaults{NumCtx: numCtx}.MaxInputChars(),
		len(defaults.Prefix)+minInputChars,
	)

	return &OllamaProvider{
		client:        client,
		baseURL:       strings.TrimRight(cfg.BaseURL, "/"),
		model:         cfg.Model,
		apiKey:        cfg.APIKey,
		prefix:        defaults.Prefix,
		numCtx:        numCtx,
		batchSize:     batchSize,
		maxInputChars: maxInputChars,
	}
}

// minInputChars is the smallest per-item budget the cap will ever leave after
// the prefix. A title alone is worth embedding; nothing is not.
const minInputChars = 64

func (p *OllamaProvider) ModelID() string { return p.model }

// Embed splits texts into batches, embeds each, and returns the vectors in
// input order. It verifies every vector in the call has the same width, so a
// provider that silently changes dimensionality mid-run fails loudly instead
// of writing rows that cannot be compared.
func (p *OllamaProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	vectors := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += p.batchSize {
		end := min(start+p.batchSize, len(texts))
		batch, err := p.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		vectors = append(vectors, batch...)
	}

	for i, vector := range vectors {
		if len(vector) != len(vectors[0]) {
			return nil, fmt.Errorf(
				"model %q returned %d dimensions for input %d but %d for input 0: "+
					"vectors of different widths cannot be compared",
				p.model, len(vector), i, len(vectors[0]),
			)
		}
	}
	return vectors, nil
}

func (p *OllamaProvider) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	inputs := make([]string, len(texts))
	for i, text := range texts {
		inputs[i] = p.prefix + truncateRunes(text, p.maxInputChars-len(p.prefix))
	}

	body, err := json.Marshal(embedRequest{
		Model:   p.model,
		Input:   inputs,
		Options: map[string]any{"num_ctx": p.numCtx},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode embed request: %w", err)
	}

	headers := map[string]string{"Content-Type": "application/json"}
	if p.apiKey != "" {
		headers["Authorization"] = "Bearer " + p.apiKey
	}

	// LimitResponseSize is deliberately not set: httpclient caps limited reads
	// at 100KB, and a 64-item batch of 1024-dim vectors is roughly 800KB of
	// JSON.
	resp, err := p.client.Do(&httpclient.Request{
		URL:     p.baseURL + "/api/embed",
		Method:  http.MethodPost,
		Headers: headers,
		Body:    bytes.NewReader(body),
		Context: ctx,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to reach the embedding provider at %s: %w", p.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding provider returned HTTP %d: %s",
			resp.StatusCode, firstLine(resp.BodyReader))
	}

	var decoded embedResponse
	if err := json.NewDecoder(resp.BodyReader).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("failed to decode the embedding response: %w", err)
	}
	if len(decoded.Embeddings) != len(texts) {
		return nil, fmt.Errorf("sent %d inputs to model %q but got %d embeddings back",
			len(texts), p.model, len(decoded.Embeddings))
	}

	p.normOnce.Do(func() { p.normErr = CheckUnitNorm(decoded.Embeddings[0]) })
	if p.normErr != nil {
		return nil, fmt.Errorf("model %q: %w", p.model, p.normErr)
	}

	return decoded.Embeddings, nil
}

// truncateRunes caps a string at maxChars without splitting a rune, so a
// truncated input stays valid UTF-8. Mirrors itemtext's truncate, which does
// the same for the derived text this reads.
func truncateRunes(s string, maxChars int) string {
	if maxChars <= 0 || len(s) <= maxChars {
		return s
	}
	truncated := s[:maxChars]
	for truncated != "" && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated
}

// firstLine reads a bounded snippet of an error body, for a message that says
// what the provider complained about without dumping a page of HTML.
func firstLine(r io.Reader) string {
	const maxSnippet = 200
	snippet, err := io.ReadAll(io.LimitReader(r, maxSnippet))
	if err != nil || len(snippet) == 0 {
		return "(no response body)"
	}
	return strings.TrimSpace(strings.SplitN(string(snippet), "\n", 2)[0])
}
