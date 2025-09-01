package unfurl

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/httpclient"
	"github.com/sirupsen/logrus"
)

// UnfurlJob represents a URL to be unfurled.
type UnfurlJob struct {
	URL string
}

// UnfurlQueue manages parallel unfurl operations.
type UnfurlQueue struct {
	jobs          chan UnfurlJob
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	service       *Service
	concurrency   int
	queueDepth    int64
	totalEnqueued int64
	totalProcessed int64
	skipRobots    bool
	retryAfter    time.Duration
}

// NewUnfurlQueue creates a new unfurl queue with the specified concurrency.
func NewUnfurlQueue(ctx context.Context, db *database.DB, concurrency int, skipRobots bool, retryAfter time.Duration) *UnfurlQueue {
	queueCtx, cancel := context.WithCancel(ctx)
	
	// Create HTTP client for unfurl operations
	httpClient := httpclient.NewClient(&httpclient.Config{
		UserAgent:       httpclient.DefaultUserAgent,
		Timeout:         httpclient.DefaultTimeout,
		MaxResponseSize: httpclient.MaxResponseSize,
	})
	
	return &UnfurlQueue{
		jobs:        make(chan UnfurlJob, concurrency*2), // Buffer to prevent blocking
		ctx:         queueCtx,
		cancel:      cancel,
		service:     NewService(db, httpClient),
		concurrency: concurrency,
		skipRobots:  skipRobots,
		retryAfter:  retryAfter,
	}
}

// Start begins processing unfurl jobs with the configured number of workers.
func (q *UnfurlQueue) Start() {
	logrus.Infof("Starting unfurl queue with %d workers", q.concurrency)
	
	for i := 0; i < q.concurrency; i++ {
		q.wg.Add(1)
		go q.worker(i)
	}
}

// Enqueue adds a URL to the unfurl queue.
func (q *UnfurlQueue) Enqueue(job UnfurlJob) {
	select {
	case q.jobs <- job:
		atomic.AddInt64(&q.queueDepth, 1)
		atomic.AddInt64(&q.totalEnqueued, 1)
		logrus.Debugf("Enqueuing unfurl for: %s", job.URL)
	case <-q.ctx.Done():
		logrus.Debugf("Queue closed, cannot enqueue: %s", job.URL)
	}
}

// Close signals that no more jobs will be added to the queue.
func (q *UnfurlQueue) Close() {
	logrus.Debugf("Closing unfurl queue (no more jobs will be added)")
	close(q.jobs)
}

// Wait waits for all unfurl workers to complete.
func (q *UnfurlQueue) Wait() {
	logrus.Debugf("Waiting for unfurl workers to complete")
	q.wg.Wait()
	logrus.Infof("All unfurl operations completed: %d total processed", atomic.LoadInt64(&q.totalProcessed))
}

// Cancel cancels all unfurl operations and waits for cleanup.
func (q *UnfurlQueue) Cancel() {
	logrus.Debugf("Cancelling unfurl operations")
	q.cancel()
	q.wg.Wait()
}

// QueueDepth returns the current number of jobs in the queue.
func (q *UnfurlQueue) QueueDepth() int {
	return int(atomic.LoadInt64(&q.queueDepth))
}

// Stats returns statistics about the queue processing.
func (q *UnfurlQueue) Stats() (enqueued, processed int64) {
	return atomic.LoadInt64(&q.totalEnqueued), atomic.LoadInt64(&q.totalProcessed)
}

// worker processes unfurl jobs from the queue.
func (q *UnfurlQueue) worker(workerID int) {
	defer q.wg.Done()
	logrus.Debugf("Unfurl worker %d started", workerID)
	
	for {
		select {
		case job, ok := <-q.jobs:
			if !ok {
				// Channel closed, worker should exit
				logrus.Debugf("Unfurl worker %d exiting (channel closed)", workerID)
				return
			}
			
			q.processJob(job, workerID)
			atomic.AddInt64(&q.queueDepth, -1)
			atomic.AddInt64(&q.totalProcessed, 1)
			
		case <-q.ctx.Done():
			// Context cancelled, worker should exit
			logrus.Debugf("Unfurl worker %d exiting (context cancelled)", workerID)
			return
		}
	}
}

// processJob processes a single unfurl job.
func (q *UnfurlQueue) processJob(job UnfurlJob, workerID int) {
	logrus.Debugf("Worker %d starting unfurl for: %s", workerID, job.URL)
	
	// Process the URL using the existing service
	err := q.service.ProcessSingleURL(
		job.URL, 
		"", // No format output needed for batch processing
		q.retryAfter,
		false, // Don't retry immediately by default
		q.skipRobots,
	)
	
	if err != nil {
		logrus.Debugf("Worker %d unfurl failed for %s: %v", workerID, job.URL, err)
	} else {
		logrus.Debugf("Worker %d completed unfurl for: %s", workerID, job.URL)
	}
}