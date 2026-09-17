package topics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/httpclient"
)

func TestOllamaLabeler(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("expected path /api/generate, got %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}

		var req generateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Model != "qwen" { //nolint:goconst
			t.Errorf("expected model qwen, got %s", req.Model)
		}
		if req.Stream {
			t.Errorf("expected stream false")
		}

		// Send back a mocked response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(generateResponse{
			Response: "\"Tech News\"", // with quotes to test stripping
		})
	}))
	defer ts.Close()

	cfg := config.TopicsConfig{
		BaseURL: ts.URL,
		Model:   "qwen",
		APIKey:  "test-key",
	}

	client := httpclient.NewClient(&httpclient.Config{
		UserAgent: "test-client",
	})
	labeler := NewOllamaLabeler(cfg, client)

	titles := []string{"Apple releases new iPhone", "New iPhone announced today"}
	label, err := labeler.LabelCluster(context.Background(), titles)
	if err != nil {
		t.Fatalf("LabelCluster failed: %v", err)
	}

	if label != "Tech News" {
		t.Errorf("expected Tech News, got %q", label)
	}

	// Test empty titles
	labelEmpty, err := labeler.LabelCluster(context.Background(), nil)
	if err != nil {
		t.Fatalf("LabelCluster failed for empty: %v", err)
	}
	if labelEmpty != "Empty Topic" {
		t.Errorf("expected Empty Topic, got %q", labelEmpty)
	}
}
