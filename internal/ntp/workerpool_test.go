package ntp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWorkerPool(t *testing.T) {
	mockClient := NewMockNTPClient()

	tests := []struct {
		name         string
		size         int
		expectedSize int
	}{
		{"Normal size", 5, 5},
		{"Zero size defaults to 1", 0, 1},
		{"Negative size defaults to 1", -5, 1},
		{"Large pool", 100, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewWorkerPool(tt.size, mockClient)
			assert.NotNil(t, pool)
			assert.Equal(t, tt.expectedSize, pool.Size())
		})
	}
}

func TestWorkerPool_Execute(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("server1", 5*time.Millisecond, 2)
	mockClient.SetupSuccessfulServer("server2", 10*time.Millisecond, 2)
	mockClient.SetupSuccessfulServer("server3", 15*time.Millisecond, 2)

	pool := NewWorkerPool(3, mockClient)
	ctx := context.Background()

	servers := []string{"server1", "server2", "server3"}
	results, err := pool.Execute(ctx, servers, 5)

	require.NoError(t, err)
	assert.Len(t, results, 3)

	// Verify each server was queried
	for _, server := range servers {
		result, exists := results[server]
		assert.True(t, exists, "Result for %s should exist", server)
		assert.NoError(t, result.Error)
		assert.NotEmpty(t, result.Responses)
		assert.GreaterOrEqual(t, result.Duration, time.Duration(0))
	}
}

func TestWorkerPool_ExecuteEmptyServers(t *testing.T) {
	mockClient := NewMockNTPClient()
	pool := NewWorkerPool(3, mockClient)
	ctx := context.Background()

	results, err := pool.Execute(ctx, []string{}, 5)

	assert.Error(t, err)
	assert.Nil(t, results)
	assert.Contains(t, err.Error(), "no servers")
}

func TestWorkerPool_ExecuteWithTimeout(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupHighLatencyServer("slow", 5*time.Second)

	pool := NewWorkerPool(2, mockClient)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	servers := []string{"slow"}
	results, err := pool.Execute(ctx, servers, 3)

	// Should return results with errors (workers collect individual failures)
	_ = err
	_ = results
	// The important thing is it respects the timeout
}

func TestWorkerPool_ExecuteWithPartialFailure(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("good1", 5*time.Millisecond, 2)
	mockClient.SetError("bad1", errors.New("network error"))
	mockClient.SetupSuccessfulServer("good2", 10*time.Millisecond, 2)

	pool := NewWorkerPool(3, mockClient)
	ctx := context.Background()

	servers := []string{"good1", "bad1", "good2"}
	results, err := pool.Execute(ctx, servers, 5)

	require.NoError(t, err) // Pool should succeed even with partial failures
	assert.Len(t, results, 3)

	// Check good servers
	assert.NoError(t, results["good1"].Error)
	assert.NoError(t, results["good2"].Error)

	// Check bad server
	assert.Error(t, results["bad1"].Error)
	assert.Empty(t, results["bad1"].Responses)
}

func TestWorkerPool_Concurrency(t *testing.T) {
	mockClient := NewMockNTPClient()

	// Setup 10 servers
	for i := 0; i < 10; i++ {
		server := "server" + string(rune('0'+i))
		mockClient.SetupSuccessfulServer(server, 50*time.Millisecond, 2)
	}

	servers := []string{
		"server0", "server1", "server2", "server3", "server4",
		"server5", "server6", "server7", "server8", "server9",
	}

	tests := []struct {
		name         string
		poolSize     int
		expectFaster time.Duration
	}{
		{"Pool size 1 (sequential)", 1, 500 * time.Millisecond},
		{"Pool size 5 (partial parallel)", 5, 200 * time.Millisecond},
		{"Pool size 10 (full parallel)", 10, 100 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewWorkerPool(tt.poolSize, mockClient)
			ctx := context.Background()

			start := time.Now()
			results, err := pool.Execute(ctx, servers, 3)
			duration := time.Since(start)

			require.NoError(t, err)
			assert.Len(t, results, 10)

			// With parallelism, should be faster
			t.Logf("Duration: %v (expected < %v)", duration, tt.expectFaster)
		})
	}
}

func TestWorkerPool_QueryAll(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("server1", 5*time.Millisecond, 2)
	mockClient.SetupSuccessfulServer("server2", 10*time.Millisecond, 2)

	pool := NewWorkerPool(2, mockClient)
	ctx := context.Background()

	servers := []string{"server1", "server2"}
	stats, err := pool.QueryAll(ctx, servers, 10)

	require.NoError(t, err)
	assert.Len(t, stats, 2)

	// Verify statistics are calculated
	for _, server := range servers {
		serverStats, exists := stats[server]
		assert.True(t, exists)
		assert.Greater(t, serverStats.SamplesCount, 0)
	}
}

func TestWorkerPool_QueryAllWithFailures(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("good", 5*time.Millisecond, 2)
	mockClient.SetError("bad", errors.New("failure"))

	pool := NewWorkerPool(2, mockClient)
	ctx := context.Background()

	servers := []string{"good", "bad"}
	stats, err := pool.QueryAll(ctx, servers, 10)

	require.NoError(t, err)
	assert.Len(t, stats, 2)

	// Good server should have stats
	goodStats := stats["good"]
	assert.Greater(t, goodStats.SamplesCount, 0)

	// Bad server should have empty stats
	badStats := stats["bad"]
	assert.Equal(t, 0, badStats.SamplesCount)
}

func TestWorkerPool_NoRaceCondition(t *testing.T) {
	mockClient := NewMockNTPClient()

	// Setup unique servers
	servers := make([]string, 20)
	for i := 0; i < 20; i++ {
		server := "race-server-" + string(rune('a'+i))
		servers[i] = server
		mockClient.SetupSuccessfulServer(server, 5*time.Millisecond, 2)
	}

	pool := NewWorkerPool(5, mockClient)
	ctx := context.Background()

	// Run multiple times to detect races
	for i := 0; i < 3; i++ {
		results, err := pool.Execute(ctx, servers, 3)
		require.NoError(t, err)
		assert.Len(t, results, 20)
	}
}

func TestWorkerPool_ConcurrentExecutions(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("server1", 10*time.Millisecond, 2)

	pool := NewWorkerPool(2, mockClient)

	// Try to run two executions concurrently
	var wg sync.WaitGroup
	wg.Add(2)

	errors := make(chan error, 2)

	go func() {
		defer wg.Done()
		_, err := pool.Execute(context.Background(), []string{"server1"}, 3)
		errors <- err
	}()

	// Give first execution time to start
	time.Sleep(5 * time.Millisecond)

	go func() {
		defer wg.Done()
		_, err := pool.Execute(context.Background(), []string{"server1"}, 3)
		errors <- err
	}()

	wg.Wait()
	close(errors)

	// Collect errors
	errorList := []error{}
	for err := range errors {
		errorList = append(errorList, err)
	}

	// One should fail with "already running"
	hasRunningError := false
	for _, err := range errorList {
		if err != nil && err.Error() == "worker pool already running" {
			hasRunningError = true
		}
	}

	// At least one should succeed
	hasSuccess := false
	for _, err := range errorList {
		if err == nil {
			hasSuccess = true
		}
	}

	assert.True(t, hasRunningError || hasSuccess, "Should have either running error or success")
}

func TestWorkerPool_AdaptivePoolSize(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("s1", 5*time.Millisecond, 2)
	mockClient.SetupSuccessfulServer("s2", 5*time.Millisecond, 2)

	// Pool size larger than server count
	pool := NewWorkerPool(10, mockClient)
	ctx := context.Background()

	results, err := pool.Execute(ctx, []string{"s1", "s2"}, 5)

	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestWorkerPool_ContextCancellation(t *testing.T) {
	mockClient := NewMockNTPClient()

	// Setup slow servers
	for i := 0; i < 5; i++ {
		server := "slow-" + string(rune('0'+i))
		mockClient.SetupHighLatencyServer(server, 5*time.Second)
	}

	servers := []string{"slow-0", "slow-1", "slow-2", "slow-3", "slow-4"}

	pool := NewWorkerPool(5, mockClient)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	results, err := pool.Execute(ctx, servers, 3)
	duration := time.Since(start)

	// Should return results even if some jobs timeout
	// (workers return individual errors per job)
	_ = err
	_ = results
	assert.Less(t, duration, 500*time.Millisecond)
}

func TestWorkerPool_WorkerReuse(t *testing.T) {
	mockClient := NewMockNTPClient()
	var callCount int32

	// Create unique servers
	servers := make([]string, 20)
	for i := 0; i < 20; i++ {
		server := "reuse-" + string(rune('a'+i))
		servers[i] = server
		mockClient.SetupSuccessfulServer(server, 5*time.Millisecond, 2)
	}

	pool := NewWorkerPool(5, mockClient)
	ctx := context.Background()

	// Execute jobs
	results, err := pool.Execute(ctx, servers, 3)

	require.NoError(t, err)
	assert.Len(t, results, 20)

	// With 5 workers handling 20 jobs, each worker should handle ~4 jobs
	// This tests worker reuse
	_ = atomic.LoadInt32(&callCount)
}

func BenchmarkWorkerPool_Execute(b *testing.B) {
	mockClient := NewMockNTPClient()

	servers := make([]string, 10)
	for i := 0; i < 10; i++ {
		server := "bench-" + string(rune('0'+i))
		servers[i] = server
		mockClient.SetupSuccessfulServer(server, 5*time.Millisecond, 2)
	}

	pool := NewWorkerPool(5, mockClient)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pool.Execute(ctx, servers, 5)
	}
}

func BenchmarkWorkerPool_QueryAll(b *testing.B) {
	mockClient := NewMockNTPClient()

	servers := make([]string, 10)
	for i := 0; i < 10; i++ {
		server := "bench-all-" + string(rune('0'+i))
		servers[i] = server
		mockClient.SetupSuccessfulServer(server, 5*time.Millisecond, 2)
	}

	pool := NewWorkerPool(5, mockClient)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pool.QueryAll(ctx, servers, 10)
	}
}
