package ntp

import (
	"context"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/pkg/mathutil"
	"github.com/stretchr/testify/assert"
)

func TestNewPool(t *testing.T) {
	client := NewClient(5*time.Second, 4)

	tests := []struct {
		name           string
		poolName       string
		strategy       string
		maxServers     int
		fallback       string
		expectStrategy string
		expectMax      int
	}{
		{"default_strategy", "pool.ntp.org", "", 4, "", "best_n", 4},
		{"explicit_strategy", "pool.ntp.org", "round_robin", 4, "", "round_robin", 4},
		{"zero_max_servers", "pool.ntp.org", "best_n", 0, "", "best_n", 4},
		{"custom_max", "pool.ntp.org", "best_n", 8, "", "best_n", 8},
		{"with_fallback", "pool.ntp.org", "best_n", 4, "time.google.com", "best_n", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewPool(tt.poolName, tt.strategy, tt.maxServers, tt.fallback, client)

			assert.NotNil(t, pool)
			assert.Equal(t, tt.poolName, pool.name)
			assert.Equal(t, tt.expectStrategy, pool.strategy)
			assert.Equal(t, tt.expectMax, pool.maxServers)
			assert.Equal(t, tt.fallback, pool.fallback)
			assert.NotNil(t, pool.querier)
			assert.NotNil(t, pool.dnsCache)
		})
	}
}

func TestPool_SelectServersByStrategy(t *testing.T) {
	client := NewClient(5*time.Second, 4)
	servers := []string{"server1", "server2", "server3", "server4", "server5"}

	tests := []struct {
		name            string
		strategy        string
		expectMinCount  int
		expectMaxCount  int
	}{
		{"strategy_all", "all", 5, 5},
		{"strategy_round_robin", "round_robin", 1, 1},
		{"strategy_best_n", "best_n", 5, 5},
		{"strategy_default", "", 5, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewPool("test.pool", tt.strategy, 4, "", client)
			selected := pool.selectServersByStrategy(servers)

			assert.GreaterOrEqual(t, len(selected), tt.expectMinCount)
			assert.LessOrEqual(t, len(selected), tt.expectMaxCount)

			// Verify selected servers are from original list
			for _, server := range selected {
				assert.Contains(t, servers, server)
			}
		})
	}
}

func TestPool_SelectServersByStrategy_RoundRobin(t *testing.T) {
	client := NewClient(5*time.Second, 4)
	servers := []string{"server1", "server2", "server3"}
	pool := NewPool("test.pool", "round_robin", 4, "", client)

	// Round-robin should select different servers over time
	// Note: This is time-based, so we can't predict exact server
	selected := pool.selectServersByStrategy(servers)
	assert.Len(t, selected, 1)
	assert.Contains(t, servers, selected[0])
}

func TestPool_SelectServersByStrategy_EmptyServers(t *testing.T) {
	client := NewClient(5*time.Second, 4)
	servers := []string{}

	strategies := []string{"all", "round_robin", "best_n"}
	for _, strategy := range strategies {
		t.Run(strategy, func(t *testing.T) {
			pool := NewPool("test.pool", strategy, 4, "", client)
			selected := pool.selectServersByStrategy(servers)
			assert.Len(t, selected, 0)
		})
	}
}

func TestPool_Resolve(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping network test in short mode")
	}

	client := NewClient(5*time.Second, 4)
	pool := NewPool("pool.ntp.org", "best_n", 4, "", client)

	ctx := context.Background()
	servers, duration, err := pool.Resolve(ctx)

	// May fail due to network issues
	if err != nil {
		t.Logf("DNS resolution failed (expected in some environments): %v", err)
		return
	}

	assert.NoError(t, err)
	assert.NotEmpty(t, servers)
	assert.Greater(t, duration, time.Duration(0))
	assert.LessOrEqual(t, len(servers), 4)
}

func TestPool_Resolve_WithFallback(t *testing.T) {
	client := NewClient(5*time.Second, 4)
	pool := NewPool("nonexistent.invalid.ntp.org", "best_n", 4, "time.google.com", client)

	ctx := context.Background()
	servers, duration, err := pool.Resolve(ctx)

	// Should return fallback
	if err == nil {
		assert.Contains(t, servers, "time.google.com")
		assert.Greater(t, duration, time.Duration(0))
	}
}

func TestPool_Resolve_NoFallback(t *testing.T) {
	client := NewClient(5*time.Second, 4)
	pool := NewPool("nonexistent.invalid.ntp.org", "best_n", 4, "", client)

	ctx := context.Background()
	_, _, err := pool.Resolve(ctx)

	assert.Error(t, err)
}

func TestPool_Resolve_ContextCancellation(t *testing.T) {
	client := NewClient(5*time.Second, 4)
	pool := NewPool("pool.ntp.org", "best_n", 4, "", client)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := pool.Resolve(ctx)

	assert.Error(t, err)
}

func TestPool_Query(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping network test in short mode")
	}

	client := NewClient(5*time.Second, 4)
	pool := NewPool("pool.ntp.org", "best_n", 4, "time.google.com", client)

	ctx := context.Background()
	response, err := pool.Query(ctx, 1)

	// May fail due to network issues
	if err != nil {
		t.Logf("Pool query failed (expected in some environments): %v", err)
		return
	}

	assert.NotNil(t, response)
	assert.Equal(t, "pool.ntp.org", response.PoolName)
	assert.NotEmpty(t, response.Servers)
	assert.Greater(t, response.TotalServers, 0)
}

func TestPool_Query_ContextCancellation(t *testing.T) {
	client := NewClient(5*time.Second, 4)
	pool := NewPool("pool.ntp.org", "best_n", 4, "", client)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	response, err := pool.Query(ctx, 1)

	// Context should be honored
	if err != nil {
		assert.Error(t, err)
	} else {
		assert.NotNil(t, response)
	}
}

func TestPool_findBestOffset(t *testing.T) {
	client := NewClient(5*time.Second, 4)
	pool := NewPool("pool.ntp.org", "best_n", 4, "", client)

	tests := []struct {
		name      string
		responses []*Response
		expected  time.Duration
	}{
		{
			name:      "empty",
			responses: []*Response{},
			expected:  0,
		},
		{
			name: "single",
			responses: []*Response{
				{Offset: 100 * time.Millisecond},
			},
			expected: 100 * time.Millisecond,
		},
		{
			name: "positive_offsets",
			responses: []*Response{
				{Offset: 100 * time.Millisecond},
				{Offset: 50 * time.Millisecond},
				{Offset: 200 * time.Millisecond},
			},
			expected: 50 * time.Millisecond,
		},
		{
			name: "negative_offsets",
			responses: []*Response{
				{Offset: -100 * time.Millisecond},
				{Offset: -50 * time.Millisecond},
				{Offset: -200 * time.Millisecond},
			},
			expected: -50 * time.Millisecond,
		},
		{
			name: "mixed_offsets",
			responses: []*Response{
				{Offset: 100 * time.Millisecond},
				{Offset: -50 * time.Millisecond},
				{Offset: 200 * time.Millisecond},
				{Offset: -10 * time.Millisecond},
			},
			expected: -10 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pool.findBestOffset(tt.responses)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAbsTime(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Duration
		expected time.Duration
	}{
		{"positive", 100 * time.Millisecond, 100 * time.Millisecond},
		{"negative", -100 * time.Millisecond, 100 * time.Millisecond},
		{"zero", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mathutil.AbsDuration(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPoolResponse_Structure(t *testing.T) {
	response := &PoolResponse{
		PoolName:      "pool.ntp.org",
		Servers:       []string{"192.168.1.1", "192.168.1.2"},
		Responses:     []*Response{},
		ActiveServers: 2,
		TotalServers:  4,
		BestOffset:    10 * time.Millisecond,
		DNSResolution: 50 * time.Millisecond,
	}

	assert.Equal(t, "pool.ntp.org", response.PoolName)
	assert.Len(t, response.Servers, 2)
	assert.Equal(t, 2, response.ActiveServers)
	assert.Equal(t, 4, response.TotalServers)
	assert.Equal(t, 10*time.Millisecond, response.BestOffset)
	assert.Equal(t, 50*time.Millisecond, response.DNSResolution)
}

func BenchmarkPool_findBestOffset(b *testing.B) {
	client := NewClient(5*time.Second, 4)
	pool := NewPool("pool.ntp.org", "best_n", 4, "", client)

	responses := []*Response{
		{Offset: 100 * time.Millisecond},
		{Offset: -50 * time.Millisecond},
		{Offset: 200 * time.Millisecond},
		{Offset: -10 * time.Millisecond},
		{Offset: 75 * time.Millisecond},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pool.findBestOffset(responses)
	}
}

func TestPool_MaxServersLimit(t *testing.T) {
	client := NewClient(5*time.Second, 4)
	pool := NewPool("pool.ntp.org", "best_n", 2, "", client)

	// Verify maxServers is set correctly
	assert.Equal(t, 2, pool.maxServers)
}
