package embed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/httpclient"
)

// probeDims is wide enough that every input in the batching test gets a
// distinct one-hot position, which is what lets the tests assert ordering.
const probeDims = 32

// fakeOllama stands in for `POST /api/embed`. It records what it received and
// answers with one-hot unit vectors whose hot index is parsed back out of the
// input text, so a test can assert both ordering and unit norm.
type fakeOllama struct {
	server *httptest.Server

	mu       sync.Mutex
	requests []embedRequest
	headers  []http.Header

	// Failure-mode switches, all off by default.
	status       int    // non-zero overrides 200
	rawBody      string // non-empty is returned verbatim
	countDelta   int    // return len(input)+countDelta embeddings
	ragged       bool   // make the second vector a different length
	unnormalized bool   // scale the first vector off unit length
}

func newFakeOllama(t *testing.T) *fakeOllama {
	t.Helper()
	f := &fakeOllama{}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeOllama) handle(w http.ResponseWriter, r *http.Request) {
	var req embedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.headers = append(f.headers, r.Header.Clone())
	f.mu.Unlock()

	if f.status != 0 {
		http.Error(w, "upstream said no", f.status)
		return
	}
	if f.rawBody != "" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(f.rawBody))
		return
	}

	count := len(req.Input) + f.countDelta
	vectors := make([][]float32, 0, count)
	for i := range count {
		dims := probeDims
		if f.ragged && i == 1 {
			dims = probeDims + 1
		}
		v := make([]float32, dims)
		hot := hotIndexFor(req.Input, i)
		v[hot%dims] = 1
		if f.unnormalized && i == 0 {
			v[hot%dims] = 2
		}
		vectors = append(vectors, v)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(embedResponse{Embeddings: vectors})
}

// hotIndexFor reads the trailing "#N" the tests put in each input, so the
// returned vector identifies which input produced it. Inputs without the
// marker fall back to their position.
func hotIndexFor(inputs []string, i int) int {
	if i >= len(inputs) {
		return i
	}
	if idx := strings.LastIndex(inputs[i], "#"); idx >= 0 {
		if n, err := strconv.Atoi(inputs[i][idx+1:]); err == nil {
			return n
		}
	}
	return i
}

func (f *fakeOllama) recorded() ([]embedRequest, []http.Header) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]embedRequest(nil), f.requests...), append([]http.Header(nil), f.headers...)
}

func newTestProvider(t *testing.T, f *fakeOllama, cfg Config) *OllamaProvider {
	t.Helper()
	if cfg.BaseURL == "" {
		cfg.BaseURL = f.server.URL
	}
	if cfg.Model == "" {
		cfg.Model = testModelQwen3Tagged
	}
	return NewOllamaProvider(cfg, httpclient.NewClient(&httpclient.Config{}))
}

func markedTexts(n int) []string {
	texts := make([]string, n)
	for i := range n {
		texts[i] = fmt.Sprintf("some item text #%d", i)
	}
	return texts
}

func TestModelIDReturnsConfiguredModel(t *testing.T) {
	f := newFakeOllama(t)
	p := newTestProvider(t, f, Config{Model: ModelNomicEmbedText})
	if got := p.ModelID(); got != ModelNomicEmbedText {
		t.Errorf("ModelID() = %q, want %q", got, ModelNomicEmbedText)
	}
}

// Batching is worth 8-28x against a real Ollama, so it is not an optimization
// to be verified loosely: assert the exact request sizes and that results come
// back in input order across batch boundaries.
func TestEmbedBatchesRequestsAndPreservesOrder(t *testing.T) {
	f := newFakeOllama(t)
	p := newTestProvider(t, f, Config{Model: ModelNomicEmbedText, BatchSize: 8})

	vectors, err := p.Embed(context.Background(), markedTexts(20))
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vectors) != 20 {
		t.Fatalf("got %d vectors, want 20", len(vectors))
	}

	requests, _ := f.recorded()
	gotSizes := make([]int, len(requests))
	for i, r := range requests {
		gotSizes[i] = len(r.Input)
	}
	wantSizes := []int{8, 8, 4}
	if fmt.Sprint(gotSizes) != fmt.Sprint(wantSizes) {
		t.Errorf("request sizes = %v, want %v", gotSizes, wantSizes)
	}

	// Each vector is one-hot at the index of the text that produced it, so
	// this catches a reordering or an off-by-one batch seam.
	for i, v := range vectors {
		if v[i] != 1 {
			t.Errorf("vector %d is not hot at index %d; results are out of input order", i, i)
		}
	}
}

func TestEmbedAppliesModelPrefix(t *testing.T) {
	tests := []struct {
		model      string
		wantPrefix string
	}{
		{ModelNomicEmbedText, nomicPrefix},
		{testModelQwen3Tagged, ""},
		{ModelEmbeddingGemma, gemmaPrefix},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			f := newFakeOllama(t)
			p := newTestProvider(t, f, Config{Model: tt.model})

			if _, err := p.Embed(context.Background(), []string{testTextHello}); err != nil {
				t.Fatalf("Embed: %v", err)
			}
			requests, _ := f.recorded()
			if len(requests) != 1 {
				t.Fatalf("got %d requests, want 1", len(requests))
			}
			want := tt.wantPrefix + testTextHello
			if requests[0].Input[0] != want {
				t.Errorf("input = %q, want %q", requests[0].Input[0], want)
			}
		})
	}
}

// Ollama's nomic card caps context at 2K unless num_ctx is sent, which would
// silently truncate the longest 2.3% of a real window.
func TestEmbedSendsNumCtx(t *testing.T) {
	f := newFakeOllama(t)
	p := newTestProvider(t, f, Config{Model: ModelNomicEmbedText})

	if _, err := p.Embed(context.Background(), []string{testTextHello}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	requests, _ := f.recorded()
	got, ok := requests[0].Options["num_ctx"]
	if !ok {
		t.Fatal("request carried no options.num_ctx; Ollama would silently cap nomic at 2K")
	}
	// JSON numbers decode as float64.
	if got != float64(8192) {
		t.Errorf("num_ctx = %v, want 8192", got)
	}
}

func TestEmbedConfigOverridesModelDefaults(t *testing.T) {
	f := newFakeOllama(t)
	p := newTestProvider(t, f, Config{Model: ModelNomicEmbedText, NumCtx: 4096, BatchSize: 3})

	if _, err := p.Embed(context.Background(), markedTexts(7)); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	requests, _ := f.recorded()
	if len(requests) != 3 {
		t.Fatalf("got %d requests, want 3 (batch size 3 over 7 texts)", len(requests))
	}
	if got := requests[0].Options["num_ctx"]; got != float64(4096) {
		t.Errorf("num_ctx = %v, want the configured 4096 rather than the model default", got)
	}
}

func TestEmbedEmptyInputMakesNoRequest(t *testing.T) {
	f := newFakeOllama(t)
	p := newTestProvider(t, f, Config{})

	vectors, err := p.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vectors) != 0 {
		t.Errorf("got %d vectors, want 0", len(vectors))
	}
	if requests, _ := f.recorded(); len(requests) != 0 {
		t.Errorf("made %d HTTP requests for empty input, want 0", len(requests))
	}
}

func TestEmbedRejectsBadResponses(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*fakeOllama)
		wantSub string
	}{
		{
			name:    "fewer embeddings than inputs",
			setup:   func(f *fakeOllama) { f.countDelta = -1 },
			wantSub: wantEmbeddingWord,
		},
		{
			name:    "more embeddings than inputs",
			setup:   func(f *fakeOllama) { f.countDelta = 1 },
			wantSub: wantEmbeddingWord,
		},
		{
			name:    "inconsistent dimensions",
			setup:   func(f *fakeOllama) { f.ragged = true },
			wantSub: "dimension",
		},
		{
			name:    "non-200 status",
			setup:   func(f *fakeOllama) { f.status = http.StatusInternalServerError },
			wantSub: "500",
		},
		{
			name:    "malformed JSON",
			setup:   func(f *fakeOllama) { f.rawBody = `{"embeddings": [[1, 2,` },
			wantSub: "decode",
		},
		{
			name:    "embeddings field absent",
			setup:   func(f *fakeOllama) { f.rawBody = `{"model":"x"}` },
			wantSub: wantEmbeddingWord,
		},
		{
			name:    "unnormalized vectors",
			setup:   func(f *fakeOllama) { f.unnormalized = true },
			wantSub: "unit",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeOllama(t)
			tt.setup(f)
			p := newTestProvider(t, f, Config{})

			_, err := p.Embed(context.Background(), markedTexts(3))
			if err == nil {
				t.Fatalf("Embed succeeded, want an error")
			}
			if !strings.Contains(strings.ToLower(err.Error()), tt.wantSub) {
				t.Errorf("error %q does not mention %q", err, tt.wantSub)
			}
		})
	}
}

func TestEmbedSendsAPIKeyOnlyWhenConfigured(t *testing.T) {
	t.Run("configured", func(t *testing.T) {
		f := newFakeOllama(t)
		p := newTestProvider(t, f, Config{APIKey: "sekrit"})
		if _, err := p.Embed(context.Background(), []string{testTextX}); err != nil {
			t.Fatalf("Embed: %v", err)
		}
		_, headers := f.recorded()
		if got := headers[0].Get("Authorization"); got != "Bearer sekrit" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer sekrit")
		}
	})

	t.Run("absent", func(t *testing.T) {
		f := newFakeOllama(t)
		p := newTestProvider(t, f, Config{})
		if _, err := p.Embed(context.Background(), []string{testTextX}); err != nil {
			t.Fatalf("Embed: %v", err)
		}
		_, headers := f.recorded()
		if got := headers[0].Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want it unset for a local provider", got)
		}
	})
}

func TestEmbedPropagatesContextCancellation(t *testing.T) {
	f := newFakeOllama(t)
	p := newTestProvider(t, f, Config{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := p.Embed(ctx, markedTexts(2)); err == nil {
		t.Fatal("Embed succeeded with a canceled context, want an error")
	}
}

// The response for a 64-item batch of 1024-dim vectors is roughly 800KB, well
// past httpclient's 100KB MaxResponseSize. That cap only applies when a
// request opts in via LimitResponseSize, so this asserts the provider does not.
func TestEmbedReadsResponsesLargerThanTheHTTPClientCap(t *testing.T) {
	f := newFakeOllama(t)
	p := newTestProvider(t, f, Config{BatchSize: 64})

	vectors, err := p.Embed(context.Background(), markedTexts(64))
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vectors) != 64 {
		t.Fatalf("got %d vectors, want 64", len(vectors))
	}
}

func TestEmbedOpenAIEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var req openAIEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		resp := openAIEmbedResponse{}
		for i := range req.Input {
			vec := make([]float32, probeDims)
			vec[i%probeDims] = 1.0
			resp.Data = append(resp.Data, struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{
				Index:     i,
				Embedding: vec,
			})
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := httpclient.NewClient(&httpclient.Config{Timeout: time.Second})
	provider := NewOllamaProvider(Config{
		BaseURL: server.URL + "/v1",
		Model:   "text-embedding-004",
	}, client)

	vectors, err := provider.Embed(context.Background(), []string{"test 1", "test 2"})
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("got %d vectors, want 2", len(vectors))
	}
	if len(vectors[0]) != probeDims {
		t.Errorf("got vector dimension %d, want %d", len(vectors[0]), probeDims)
	}
}
