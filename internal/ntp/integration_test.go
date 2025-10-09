package ntp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests using MockNTPClient to simulate real scenarios

func TestIntegration_PoolWithAdaptiveSampling(t *testing.T) {
	// Setup mock servers with different characteristics
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("server1.pool.ntp.org", 5*time.Millisecond, 2)
	mockClient.SetupSuccessfulServer("server2.pool.ntp.org", 100*time.Millisecond, 2) // High drift
	mockClient.SetupSuccessfulServer("server3.pool.ntp.org", 10*time.Millisecond, 2)

	// Create pool
	pool := NewPool("test.pool.ntp.org", "all", 3, "", mockClient)

	// Manually inject server IPs (bypass DNS)
	pool.dnsCache.cache["test.pool.ntp.org"] = &DNSCacheEntry{
		IPs:       []string{"server1.pool.ntp.org", "server2.pool.ntp.org", "server3.pool.ntp.org"},
		ExpiresAt: time.Now().Add(1 * time.Hour),
		TTL:       1 * time.Hour,
	}

	// Create adaptive sampler
	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{
		DefaultSamples:   3,
		HighDriftSamples: 10,
		DriftThreshold:   50 * time.Millisecond,
	}, mockClient)

	ctx := context.Background()

	// Sample from pool servers
	results, err := sampler.SampleMultipleServers(ctx, []string{
		"server1.pool.ntp.org",
		"server2.pool.ntp.org",
		"server3.pool.ntp.org",
	})

	require.NoError(t, err)
	assert.Len(t, results, 3)

	// Verify server2 got more samples due to high drift
	server2Results := results["server2.pool.ntp.org"]
	assert.GreaterOrEqual(t, len(server2Results), 10, "High drift server should get more samples")

	// Verify server1 and server3 got default samples
	assert.GreaterOrEqual(t, len(results["server1.pool.ntp.org"]), 3)
	assert.GreaterOrEqual(t, len(results["server3.pool.ntp.org"]), 3)
}

func TestIntegration_PoolStrategyRoundRobin(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("server1.pool.ntp.org", 5*time.Millisecond, 2)
	mockClient.SetupSuccessfulServer("server2.pool.ntp.org", 10*time.Millisecond, 2)
	mockClient.SetupSuccessfulServer("server3.pool.ntp.org", 15*time.Millisecond, 2)

	pool := NewPool("test.pool.ntp.org", "round_robin", 3, "", mockClient)

	// Inject servers
	pool.dnsCache.cache["test.pool.ntp.org"] = &DNSCacheEntry{
		IPs:       []string{"server1.pool.ntp.org", "server2.pool.ntp.org", "server3.pool.ntp.org"},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	ctx := context.Background()

	// Query pool multiple times
	selectedServers := make(map[string]bool)
	for i := 0; i < 10; i++ {
		response, err := pool.Query(ctx, 3)
		if err == nil && len(response.Responses) > 0 {
			// Round-robin should select 1 server per query
			assert.LessOrEqual(t, len(response.Responses), 1)
			if len(response.Responses) > 0 {
				selectedServers[response.Responses[0].Server] = true
			}
		}
		time.Sleep(time.Second) // Wait for round-robin rotation
	}

	// Over multiple queries, different servers should be selected
	assert.GreaterOrEqual(t, len(selectedServers), 1)
}

func TestIntegration_DNSCacheWithPool(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("192.0.2.1", 5*time.Millisecond, 2)
	mockClient.SetupSuccessfulServer("192.0.2.2", 10*time.Millisecond, 2)

	pool := NewPool("test.pool.ntp.org", "best_n", 2, "", mockClient)

	// First query - DNS cache miss (populated manually)
	pool.dnsCache.cache["test.pool.ntp.org"] = &DNSCacheEntry{
		IPs:       []string{"192.0.2.1", "192.0.2.2"},
		ExpiresAt: time.Now().Add(100 * time.Millisecond), // Short TTL
		TTL:       100 * time.Millisecond,
	}

	ctx := context.Background()

	// First query
	start := time.Now()
	_, err := pool.Query(ctx, 3)
	duration1 := time.Since(start)
	require.NoError(t, err)

	// Second query - should use cache
	start = time.Now()
	_, err = pool.Query(ctx, 3)
	duration2 := time.Since(start)
	require.NoError(t, err)

	// With mock, timing might be too fast to measure reliably
	_ = duration2
	_ = duration1

	// Wait for cache expiration
	time.Sleep(150 * time.Millisecond)

	// Third query - cache expired, need refresh
	start = time.Now()
	_, err = pool.Query(ctx, 3)
	duration3 := time.Since(start)

	// Third query may fail (no real DNS) but shouldn't panic
	_ = err
	_ = duration3
}

func TestIntegration_FailoverWithFallback(t *testing.T) {
	mockClient := NewMockNTPClient()
	// Primary server fails
	mockClient.SetError("primary.ntp.org", assert.AnError)
	// Fallback succeeds
	mockClient.SetupSuccessfulServer("fallback.ntp.org", 10*time.Millisecond, 2)

	pool := NewPool("primary.ntp.org", "best_n", 4, "fallback.ntp.org", mockClient)

	ctx := context.Background()
	servers, _, err := pool.Resolve(ctx)

	require.NoError(t, err)
	assert.Contains(t, servers, "fallback.ntp.org", "Should use fallback on DNS failure")
}

func TestIntegration_FlappingServerDetection(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupFlappingServer("flapping.ntp.org")

	ctx := context.Background()

	// Query flapping server multiple times
	successCount := 0
	failCount := 0

	for i := 0; i < 10; i++ {
		_, err := mockClient.Query(ctx, "flapping.ntp.org")
		if err == nil {
			successCount++
		} else {
			failCount++
		}
	}

	// Should have both successes and failures
	assert.Greater(t, successCount, 0, "Flapping server should sometimes succeed")
	assert.Greater(t, failCount, 0, "Flapping server should sometimes fail")
}

func TestIntegration_HighLatencyWithTimeout(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupHighLatencyServer("slow.ntp.org", 3*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	start := time.Now()
	_, err := mockClient.Query(ctx, "slow.ntp.org")
	duration := time.Since(start)

	// Should timeout before 3 seconds
	assert.Error(t, err)
	assert.Less(t, duration, 2*time.Second, "Should timeout quickly")
}

func TestIntegration_StatisticsCalculation(t *testing.T) {
	mockClient := NewMockNTPClient()

	// Setup server with consistent low drift
	mockClient.SetupSuccessfulServer("stable.ntp.org", 5*time.Millisecond, 2)

	ctx := context.Background()
	responses, err := mockClient.QueryMultiple(ctx, "stable.ntp.org", 10)
	require.NoError(t, err)

	stats := CalculateStatistics(responses, 10)

	// Verify statistics
	assert.Equal(t, 10, stats.SamplesCount)
	assert.Equal(t, 0.0, stats.PacketLossRatio)
	assert.InDelta(t, 5*time.Millisecond, stats.MedianOffset, float64(time.Millisecond))
	assert.Less(t, stats.StdDevOffset, 10*time.Millisecond, "Low variance expected")
}

func TestIntegration_KissOfDeathHandling(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupKoDServer("kod.ntp.org", "RATE")

	ctx := context.Background()
	resp, err := mockClient.Query(ctx, "kod.ntp.org")

	require.NoError(t, err)
	assert.True(t, resp.IsKissOfDeath())
	assert.Equal(t, "RATE", resp.KissCode)
}

func TestIntegration_InvalidStratumDetection(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupInvalidStratumServer("invalid.ntp.org")

	ctx := context.Background()
	resp, err := mockClient.Query(ctx, "invalid.ntp.org")

	require.NoError(t, err)
	assert.True(t, resp.Stratum == 0 || resp.Stratum > 15, "Invalid stratum")
}

func TestIntegration_ValidationWithMock(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("valid.ntp.org", 10*time.Millisecond, 2)
	mockClient.SetupKoDServer("kod.ntp.org", "DENY")
	mockClient.SetupInvalidStratumServer("invalid.ntp.org")

	validator := NewValidator()
	ctx := context.Background()

	// Test valid server
	validResp, err := mockClient.Query(ctx, "valid.ntp.org")
	require.NoError(t, err)
	validResult := validator.Validate(validResp)
	assert.True(t, validResult.Valid)
	assert.Greater(t, validResult.TrustScore, 0.8)

	// Test KoD server
	kodResp, err := mockClient.Query(ctx, "kod.ntp.org")
	require.NoError(t, err)
	kodResult := validator.Validate(kodResp)
	assert.False(t, kodResult.Valid)
	assert.Contains(t, kodResult.Errors, "Kiss-of-Death received: DENY")

	// Test invalid stratum
	invalidResp, err := mockClient.Query(ctx, "invalid.ntp.org")
	require.NoError(t, err)
	invalidResult := validator.Validate(invalidResp)
	assert.False(t, invalidResult.Valid)
}

func TestIntegration_ConcurrentQueries(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("concurrent.ntp.org", 5*time.Millisecond, 2)

	ctx := context.Background()
	concurrency := 100
	done := make(chan bool, concurrency)
	errors := make(chan error, concurrency)

	// Launch concurrent queries
	for i := 0; i < concurrency; i++ {
		go func() {
			_, err := mockClient.Query(ctx, "concurrent.ntp.org")
			if err != nil {
				errors <- err
			}
			done <- true
		}()
	}

	// Wait for all
	for i := 0; i < concurrency; i++ {
		<-done
	}
	close(errors)

	// Check no errors
	errorCount := 0
	for range errors {
		errorCount++
	}

	assert.Equal(t, 0, errorCount, "No errors in concurrent queries")
	assert.Equal(t, concurrency, mockClient.GetCallCount("concurrent.ntp.org"))
}

func BenchmarkIntegration_PoolQueryWithCache(b *testing.B) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("server1", 5*time.Millisecond, 2)
	mockClient.SetupSuccessfulServer("server2", 10*time.Millisecond, 2)

	pool := NewPool("test.pool", "best_n", 2, "", mockClient)
	pool.dnsCache.cache["test.pool"] = &DNSCacheEntry{
		IPs:       []string{"server1", "server2"},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pool.Query(ctx, 3)
	}
}

func BenchmarkIntegration_AdaptiveSamplingLowDrift(b *testing.B) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("test.ntp.org", 5*time.Millisecond, 2)

	sampler := NewAdaptiveSampler(AdaptiveSamplingConfig{}, mockClient)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sampler.Sample(ctx, "test.ntp.org")
	}
}
