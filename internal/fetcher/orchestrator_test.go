package fetcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lmorchard/feedspool-go/internal/config"
)

func TestFetchFromURLs(t *testing.T) {
	db := setupTestDatabase(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(testFeedXML))
	}))
	defer server.Close()

	o := NewOrchestrator(db, config.GetDefault())
	results := o.FetchFromURLs(context.Background(), []string{server.URL}, FetchOptions{
		Timeout:     config.DefaultTimeout,
		MaxItems:    config.DefaultMaxItems,
		Concurrency: 1,
	})

	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Error != nil {
		t.Errorf("results[0].Error = %v, want nil", results[0].Error)
	}
}

func TestFetchFromURLsEmpty(t *testing.T) {
	db := setupTestDatabase(t)

	o := NewOrchestrator(db, config.GetDefault())
	results := o.FetchFromURLs(context.Background(), nil, FetchOptions{Concurrency: 1})

	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0", len(results))
	}
}
