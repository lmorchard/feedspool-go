package unfurl

import (
	"context"
	"testing"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
)

func TestUnfurlQueue_Basic(t *testing.T) {
	// Create a test database
	db, err := database.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	if err := db.InitSchema(); err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	ctx := context.Background()
	queue := NewUnfurlQueue(ctx, db, 2, true, 1*time.Hour)

	if queue == nil {
		t.Fatal("Failed to create unfurl queue")
	}

	queue.Start()
	defer queue.Cancel()

	// Test initial state
	if queue.QueueDepth() != 0 {
		t.Errorf("Expected queue depth 0, got %d", queue.QueueDepth())
	}

	enqueued, processed := queue.Stats()
	if enqueued != 0 || processed != 0 {
		t.Errorf("Expected 0 enqueued and 0 processed, got %d enqueued, %d processed", enqueued, processed)
	}
}

func TestUnfurlQueue_EnqueueAndProcess(t *testing.T) {
	// Create a test database
	db, err := database.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	if err := db.InitSchema(); err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	ctx := context.Background()
	queue := NewUnfurlQueue(ctx, db, 1, true, 1*time.Hour)
	queue.Start()
	defer queue.Cancel()

	// Enqueue a test job
	testURL := "https://example.com"
	queue.Enqueue(UnfurlJob{URL: testURL})

	// Check stats
	enqueued, _ := queue.Stats()
	if enqueued != 1 {
		t.Errorf("Expected 1 enqueued, got %d", enqueued)
	}

	// Give it time to process (this is a basic test, real unfurl will likely fail for example.com)
	time.Sleep(100 * time.Millisecond)
}

func TestUnfurlQueue_ConcurrencyValidation(t *testing.T) {
	// Test that concurrency is validated and adjusted
	db, err := database.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	if err := db.InitSchema(); err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	tests := []struct {
		input    int
		expected int
	}{
		{0, 1},     // Zero should become 1
		{-5, 1},    // Negative should become 1
		{50, 50},   // Normal value should stay the same
		{150, 100}, // Too high should be limited to 100
	}

	for _, test := range tests {
		ctx := context.Background()
		queue := NewUnfurlQueue(ctx, db, test.input, true, 1*time.Hour)
		if queue.concurrency != test.expected {
			t.Errorf("For input %d, expected concurrency %d, got %d", test.input, test.expected, queue.concurrency)
		}
		queue.Cancel()
	}
}
