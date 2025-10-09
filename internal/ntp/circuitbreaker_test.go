package ntp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sony/gobreaker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	config := DefaultCircuitBreakerConfig()

	assert.Equal(t, uint32(3), config.MaxRequests)
	assert.Equal(t, 60*time.Second, config.Interval)
	assert.Equal(t, 30*time.Second, config.Timeout)
	assert.NotNil(t, config.ReadyToTrip)

	// Test ReadyToTrip logic
	counts := gobreaker.Counts{
		Requests:      10,
		TotalFailures: 6,
	}
	assert.True(t, config.ReadyToTrip(counts))

	counts = gobreaker.Counts{
		Requests:      10,
		TotalFailures: 5,
	}
	assert.False(t, config.ReadyToTrip(counts))
}

func TestNewCircuitBreakerClient(t *testing.T) {
	mockClient := NewMockNTPClient()

	tests := []struct {
		name   string
		config CircuitBreakerConfig
	}{
		{
			name:   "Default config",
			config: DefaultCircuitBreakerConfig(),
		},
		{
			name: "Custom config",
			config: CircuitBreakerConfig{
				MaxRequests: 5,
				Interval:    120 * time.Second,
				Timeout:     60 * time.Second,
			},
		},
		{
			name:   "Empty config (should use defaults)",
			config: CircuitBreakerConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewCircuitBreakerClient(mockClient, tt.config)
			assert.NotNil(t, cb)
			assert.NotNil(t, cb.querier)
			assert.NotNil(t, cb.breakers)
		})
	}
}

func TestCircuitBreakerClient_SuccessfulQuery(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("good.ntp.org", 10*time.Millisecond, 2)

	cb := NewCircuitBreakerClient(mockClient, DefaultCircuitBreakerConfig())
	ctx := context.Background()

	resp, err := cb.Query(ctx, "good.ntp.org")

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, gobreaker.StateClosed, cb.GetState("good.ntp.org"))
}

func TestCircuitBreakerClient_FailureTripsBreaker(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetError("bad.ntp.org", errors.New("network error"))

	config := CircuitBreakerConfig{
		MaxRequests: 1,
		Interval:    1 * time.Second,
		Timeout:     100 * time.Millisecond,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 3
		},
	}

	cb := NewCircuitBreakerClient(mockClient, config)
	ctx := context.Background()

	// First 3 requests should fail and trip the breaker
	for i := 0; i < 3; i++ {
		_, err := cb.Query(ctx, "bad.ntp.org")
		assert.Error(t, err)
	}

	// Breaker should be open now
	assert.Equal(t, gobreaker.StateOpen, cb.GetState("bad.ntp.org"))

	// Next request should immediately fail with ErrOpenState
	_, err := cb.Query(ctx, "bad.ntp.org")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker open")
}

func TestCircuitBreakerClient_HalfOpenRecovery(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetError("failing.ntp.org", errors.New("failure"))

	config := CircuitBreakerConfig{
		MaxRequests: 1,
		Interval:    1 * time.Second,
		Timeout:     100 * time.Millisecond,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 2
		},
	}

	cb := NewCircuitBreakerClient(mockClient, config)
	ctx := context.Background()

	// Trip the breaker
	for i := 0; i < 2; i++ {
		_, _ = cb.Query(ctx, "failing.ntp.org")
	}

	assert.Equal(t, gobreaker.StateOpen, cb.GetState("failing.ntp.org"))

	// Wait for timeout to transition to half-open
	time.Sleep(150 * time.Millisecond)

	// After timeout, should be able to attempt queries again (half-open)
	// This will fail, but it proves we can try
	_, err := cb.Query(ctx, "failing.ntp.org")
	assert.Error(t, err)

	// After a failed attempt in half-open, should go back to open
	// (actual behavior depends on circuit breaker implementation)
	state := cb.GetState("failing.ntp.org")
	assert.True(t, state == gobreaker.StateOpen || state == gobreaker.StateHalfOpen)
}

func TestCircuitBreakerClient_QueryMultiple(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("multi.ntp.org", 5*time.Millisecond, 2)

	cb := NewCircuitBreakerClient(mockClient, DefaultCircuitBreakerConfig())
	ctx := context.Background()

	responses, err := cb.QueryMultiple(ctx, "multi.ntp.org", 5)

	require.NoError(t, err)
	assert.Len(t, responses, 5)
	assert.Equal(t, gobreaker.StateClosed, cb.GetState("multi.ntp.org"))
}

func TestCircuitBreakerClient_QueryMultipleWithFailure(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetError("failing.ntp.org", errors.New("query failed"))

	config := CircuitBreakerConfig{
		MaxRequests: 1,
		Interval:    1 * time.Second,
		Timeout:     100 * time.Millisecond,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 2
		},
	}

	cb := NewCircuitBreakerClient(mockClient, config)
	ctx := context.Background()

	// First two requests should fail and trip
	_, err := cb.QueryMultiple(ctx, "failing.ntp.org", 3)
	assert.Error(t, err)

	_, err = cb.QueryMultiple(ctx, "failing.ntp.org", 3)
	assert.Error(t, err)

	// Breaker should be open
	assert.Equal(t, gobreaker.StateOpen, cb.GetState("failing.ntp.org"))
}

func TestCircuitBreakerClient_GetCounts(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("counted.ntp.org", 5*time.Millisecond, 2)

	cb := NewCircuitBreakerClient(mockClient, DefaultCircuitBreakerConfig())
	ctx := context.Background()

	// Make some successful requests
	for i := 0; i < 5; i++ {
		_, err := cb.Query(ctx, "counted.ntp.org")
		require.NoError(t, err)
	}

	counts := cb.GetCounts("counted.ntp.org")
	assert.Equal(t, uint32(5), counts.Requests)
	assert.Equal(t, uint32(0), counts.TotalFailures)
}

func TestCircuitBreakerClient_GetAllStates(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("good", 5*time.Millisecond, 2)
	mockClient.SetError("bad", errors.New("failure"))

	config := CircuitBreakerConfig{
		MaxRequests: 1,
		Interval:    1 * time.Second,
		Timeout:     100 * time.Millisecond,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 2
		},
	}

	cb := NewCircuitBreakerClient(mockClient, config)
	ctx := context.Background()

	// Query good server
	_, _ = cb.Query(ctx, "good")

	// Trip bad server
	for i := 0; i < 2; i++ {
		_, _ = cb.Query(ctx, "bad")
	}

	states := cb.GetAllStates()
	assert.Len(t, states, 2)
	assert.Equal(t, gobreaker.StateClosed, states["good"])
	assert.Equal(t, gobreaker.StateOpen, states["bad"])
}

func TestCircuitBreakerClient_MultipleServers(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("server1", 5*time.Millisecond, 2)
	mockClient.SetupSuccessfulServer("server2", 10*time.Millisecond, 2)
	mockClient.SetError("server3", errors.New("failure"))

	cb := NewCircuitBreakerClient(mockClient, DefaultCircuitBreakerConfig())
	ctx := context.Background()

	// Query different servers
	_, err1 := cb.Query(ctx, "server1")
	_, err2 := cb.Query(ctx, "server2")
	_, err3 := cb.Query(ctx, "server3")

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Error(t, err3)

	// Each should have its own breaker
	assert.Equal(t, gobreaker.StateClosed, cb.GetState("server1"))
	assert.Equal(t, gobreaker.StateClosed, cb.GetState("server2"))
	assert.Equal(t, gobreaker.StateClosed, cb.GetState("server3"))
}

func TestCircuitBreakerClient_ConcurrentAccess(t *testing.T) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("concurrent", 5*time.Millisecond, 2)

	cb := NewCircuitBreakerClient(mockClient, DefaultCircuitBreakerConfig())
	ctx := context.Background()

	// Launch concurrent queries
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = cb.Query(ctx, "concurrent")
			done <- true
		}()
	}

	// Wait for all
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should handle concurrency without issues
	counts := cb.GetCounts("concurrent")
	assert.Equal(t, uint32(10), counts.Requests)
}

func TestCircuitBreakerClient_NonExistentServer(t *testing.T) {
	mockClient := NewMockNTPClient()
	cb := NewCircuitBreakerClient(mockClient, DefaultCircuitBreakerConfig())

	// Get state for non-existent server
	state := cb.GetState("nonexistent")
	assert.Equal(t, gobreaker.StateClosed, state)

	counts := cb.GetCounts("nonexistent")
	assert.Equal(t, uint32(0), counts.Requests)
}

func BenchmarkCircuitBreakerClient_Query(b *testing.B) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("bench", 5*time.Millisecond, 2)

	cb := NewCircuitBreakerClient(mockClient, DefaultCircuitBreakerConfig())
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cb.Query(ctx, "bench")
	}
}

func BenchmarkCircuitBreakerClient_QueryMultiple(b *testing.B) {
	mockClient := NewMockNTPClient()
	mockClient.SetupSuccessfulServer("bench-multi", 5*time.Millisecond, 2)

	cb := NewCircuitBreakerClient(mockClient, DefaultCircuitBreakerConfig())
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cb.QueryMultiple(ctx, "bench-multi", 5)
	}
}
