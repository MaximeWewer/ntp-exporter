package ntp

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// WorkerPool manages concurrent NTP queries with bounded parallelism.
type WorkerPool struct {
	size    int
	querier NTPQuerier
	mu      sync.Mutex
	running bool
}

// Job represents a single NTP query task.
type Job struct {
	Server   string
	Ctx      context.Context
	Samples  int
	ResultCh chan<- JobResult
}

// JobResult contains the result of a job execution.
type JobResult struct {
	Server    string
	Responses []*Response
	Error     error
	Duration  time.Duration
}

// NewWorkerPool creates a new worker pool with the specified size.
// size determines the maximum number of concurrent workers.
func NewWorkerPool(size int, querier NTPQuerier) *WorkerPool {
	if size <= 0 {
		size = 1
	}
	return &WorkerPool{
		size:    size,
		querier: querier,
	}
}

// Execute runs a batch of jobs using the worker pool.
// It launches workers to process jobs concurrently and collects results.
func (wp *WorkerPool) Execute(ctx context.Context, servers []string, samples int) (map[string]JobResult, error) {
	if len(servers) == 0 {
		return nil, fmt.Errorf("no servers to query")
	}

	wp.mu.Lock()
	if wp.running {
		wp.mu.Unlock()
		return nil, fmt.Errorf("worker pool already running")
	}
	wp.running = true
	wp.mu.Unlock()

	defer func() {
		wp.mu.Lock()
		wp.running = false
		wp.mu.Unlock()
	}()

	results := make(map[string]JobResult)
	resultsCh := make(chan JobResult, len(servers))
	jobsCh := make(chan Job, len(servers))

	// Start workers
	workerCount := wp.size
	if workerCount > len(servers) {
		workerCount = len(servers)
	}

	var wg sync.WaitGroup
	wg.Add(workerCount)

	for i := 0; i < workerCount; i++ {
		go wp.worker(ctx, jobsCh, &wg)
	}

	// Submit jobs
	for _, server := range servers {
		job := Job{
			Server:   server,
			Ctx:      ctx,
			Samples:  samples,
			ResultCh: resultsCh,
		}
		select {
		case jobsCh <- job:
		case <-ctx.Done():
			close(jobsCh)
			wg.Wait()
			return nil, ctx.Err()
		}
	}
	close(jobsCh)

	// Wait for all workers to finish
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// Collect results
	for result := range resultsCh {
		results[result.Server] = result
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no results collected")
	}

	return results, nil
}

// worker processes jobs from the jobs channel.
func (wp *WorkerPool) worker(ctx context.Context, jobs <-chan Job, wg *sync.WaitGroup) {
	defer wg.Done()

	for job := range jobs {
		select {
		case <-ctx.Done():
			job.ResultCh <- JobResult{
				Server: job.Server,
				Error:  ctx.Err(),
			}
			return
		default:
			result := wp.processJob(job)
			select {
			case job.ResultCh <- result:
			case <-ctx.Done():
				return
			}
		}
	}
}

// processJob executes a single job.
func (wp *WorkerPool) processJob(job Job) JobResult {
	start := time.Now()

	responses, err := wp.querier.QueryMultiple(job.Ctx, job.Server, job.Samples)

	return JobResult{
		Server:    job.Server,
		Responses: responses,
		Error:     err,
		Duration:  time.Since(start),
	}
}

// QueryAll queries all servers in parallel with samples per server.
// Returns aggregated statistics for each server.
func (wp *WorkerPool) QueryAll(ctx context.Context, servers []string, samples int) (map[string]*Statistics, error) {
	results, err := wp.Execute(ctx, servers, samples)
	if err != nil {
		return nil, err
	}

	stats := make(map[string]*Statistics)
	for server, result := range results {
		if result.Error != nil {
			// Create empty stats with error
			stats[server] = &Statistics{
				SamplesCount: 0,
			}
			continue
		}

		serverStats := CalculateStatistics(result.Responses, samples)
		stats[server] = serverStats
	}

	return stats, nil
}

// Size returns the configured worker pool size.
func (wp *WorkerPool) Size() int {
	return wp.size
}
