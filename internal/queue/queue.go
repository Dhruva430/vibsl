// Package queue provides a bounded in-memory work queue and a worker pool that
// drains it through the pipeline. Jobs are processed concurrently up to the
// configured worker count; back-pressure is applied when the buffer is full.
package queue

import (
	"context"
	"sync"

	"github.com/Dhruva430/vibsl/internal/pipeline"
)

// Queue is a buffered channel of job ids plus a pool of workers.
type Queue struct {
	ch     chan string
	runner *pipeline.Runner
	wg     sync.WaitGroup
}

// New creates a queue with the given buffer capacity and pipeline runner.
func New(capacity int, runner *pipeline.Runner) *Queue {
	if capacity < 1 {
		capacity = 1
	}
	return &Queue{ch: make(chan string, capacity), runner: runner}
}

// Enqueue submits a job id for processing. It returns false if the queue is
// full, letting the API reply 503 rather than blocking the request.
func (q *Queue) Enqueue(id string) bool {
	select {
	case q.ch <- id:
		return true
	default:
		return false
	}
}

// Start launches n workers that process ids until the context is cancelled.
func (q *Queue) Start(ctx context.Context, workers int) {
	if workers < 1 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.worker(ctx)
	}
}

func (q *Queue) worker(ctx context.Context) {
	defer q.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-q.ch:
			// Run is self-contained: it records success/failure on the job
			// record, so a single bad job never takes down the worker.
			q.runner.Run(ctx, id)
		}
	}
}

// Drain waits for all workers to exit after the context is cancelled.
func (q *Queue) Drain() { q.wg.Wait() }

// Depth reports the number of ids currently buffered (best-effort, for /healthz).
func (q *Queue) Depth() int { return len(q.ch) }
