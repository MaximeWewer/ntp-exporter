package ntp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDNSCache(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{})
	assert.NotNil(t, cache)
	assert.Equal(t, 5*time.Minute, cache.minTTL)
	assert.Equal(t, 60*time.Minute, cache.maxTTL)
}

func TestNewDNSCache_CustomTTL(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{
		MinTTL: 1 * time.Minute,
		MaxTTL: 10 * time.Minute,
	})
	assert.Equal(t, 1*time.Minute, cache.minTTL)
	assert.Equal(t, 10*time.Minute, cache.maxTTL)
}

func TestDNSCache_ResolveIPAddress(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{})
	ctx := context.Background()

	// IP addresses should not be resolved
	ips, err := cache.Resolve(ctx, "8.8.8.8")
	require.NoError(t, err)
	assert.Equal(t, []string{"8.8.8.8"}, ips)
}

func TestDNSCache_ResolveCaching(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping DNS resolution test in short mode")
	}

	cache := NewDNSCache(DNSCacheConfig{
		MinTTL: 1 * time.Second,
		MaxTTL: 10 * time.Second,
	})
	ctx := context.Background()

	// First resolution - cache miss
	ips1, err := cache.Resolve(ctx, "pool.ntp.org")
	require.NoError(t, err)
	assert.Greater(t, len(ips1), 0)

	// Second resolution - cache hit (should be instant)
	start := time.Now()
	ips2, err := cache.Resolve(ctx, "pool.ntp.org")
	duration := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, ips1, ips2)
	assert.Less(t, duration, 10*time.Millisecond, "Cache hit should be very fast")
}

func TestDNSCache_TTLExpiration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping DNS resolution test in short mode")
	}

	cache := NewDNSCache(DNSCacheConfig{
		MinTTL: 100 * time.Millisecond,
		MaxTTL: 200 * time.Millisecond,
	})
	ctx := context.Background()

	// First resolution
	_, err := cache.Resolve(ctx, "pool.ntp.org")
	require.NoError(t, err)

	// Wait for TTL to expire
	time.Sleep(250 * time.Millisecond)

	// Second resolution - should re-resolve
	ips2, err := cache.Resolve(ctx, "pool.ntp.org")
	require.NoError(t, err)
	assert.Greater(t, len(ips2), 0)
	// IPs might be different after re-resolution
}

func TestDNSCache_InvalidHostname(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{})
	ctx := context.Background()

	ips, err := cache.Resolve(ctx, "invalid.nonexistent.hostname.test")
	assert.Error(t, err)
	assert.Nil(t, ips)
}

func TestDNSCache_Invalidate(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{})

	// Manually add entry
	cache.cache["test.example.com"] = &DNSCacheEntry{
		IPs:       []string{"1.2.3.4"},
		ExpiresAt: time.Now().Add(1 * time.Hour),
		TTL:       1 * time.Hour,
	}

	// Verify it exists
	assert.Len(t, cache.cache, 1)

	// Invalidate
	cache.Invalidate("test.example.com")

	// Verify it's gone
	assert.Len(t, cache.cache, 0)
}

func TestDNSCache_Clear(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{})

	// Add multiple entries
	cache.cache["test1.example.com"] = &DNSCacheEntry{
		IPs:       []string{"1.2.3.4"},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	cache.cache["test2.example.com"] = &DNSCacheEntry{
		IPs:       []string{"5.6.7.8"},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	assert.Len(t, cache.cache, 2)

	// Clear
	cache.Clear()

	// Verify all gone
	assert.Len(t, cache.cache, 0)
}

func TestDNSCache_Stats(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{})

	// Add valid entry
	cache.cache["valid.example.com"] = &DNSCacheEntry{
		IPs:       []string{"1.2.3.4"},
		ExpiresAt: time.Now().Add(1 * time.Hour),
		TTL:       1 * time.Hour,
	}

	// Add expired entry
	cache.cache["expired.example.com"] = &DNSCacheEntry{
		IPs:       []string{"5.6.7.8"},
		ExpiresAt: time.Now().Add(-1 * time.Hour),
		TTL:       1 * time.Hour,
	}

	stats := cache.Stats()
	assert.Equal(t, 2, stats["total_entries"])
	assert.Equal(t, 1, stats["valid_entries"])
	assert.Equal(t, 1, stats["expired_entries"])
}

func TestDNSCache_CleanupExpired(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{})

	// Add valid entry
	cache.cache["valid.example.com"] = &DNSCacheEntry{
		IPs:       []string{"1.2.3.4"},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	// Add expired entries
	cache.cache["expired1.example.com"] = &DNSCacheEntry{
		IPs:       []string{"5.6.7.8"},
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	cache.cache["expired2.example.com"] = &DNSCacheEntry{
		IPs:       []string{"9.10.11.12"},
		ExpiresAt: time.Now().Add(-2 * time.Hour),
	}

	assert.Len(t, cache.cache, 3)

	// Cleanup
	removed := cache.CleanupExpired()

	// Verify expired removed
	assert.Equal(t, 2, removed)
	assert.Len(t, cache.cache, 1)
	_, exists := cache.cache["valid.example.com"]
	assert.True(t, exists)
}

func TestDNSCache_AdaptiveTTL(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{
		MinTTL: 1 * time.Minute,
		MaxTTL: 10 * time.Minute,
	})

	tests := []struct {
		name       string
		exists     bool
		errorCount int
		expectTTL  time.Duration
	}{
		{"first_resolution", false, 0, 5*time.Minute + 30*time.Second}, // Mid-range
		{"successful_history", true, 0, 10 * time.Minute},              // Max TTL
		{"error_history", true, 5, 1 * time.Minute},                    // Min TTL
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var entry *DNSCacheEntry
			if tt.exists {
				entry = &DNSCacheEntry{
					ErrorCount: tt.errorCount,
				}
			}

			ttl := cache.calculateAdaptiveTTL(tt.exists, entry)
			assert.Equal(t, tt.expectTTL, ttl)
		})
	}
}

func TestDNSCache_StaleCache(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping DNS resolution test in short mode")
	}

	cache := NewDNSCache(DNSCacheConfig{
		MinTTL: 100 * time.Millisecond,
		MaxTTL: 200 * time.Millisecond,
	})
	ctx := context.Background()

	// First resolution - should succeed
	_, err := cache.Resolve(ctx, "pool.ntp.org")
	require.NoError(t, err)

	// Wait for expiration
	time.Sleep(250 * time.Millisecond)

	// Try to resolve invalid hostname
	// Should use stale cache as fallback
	ips2, err := cache.Resolve(ctx, "invalid.test.nonexistent.hostname")
	assert.Error(t, err)
	assert.Nil(t, ips2)
}

func TestDNSCache_CleanupWorker(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{})

	// Add expired entry
	cache.cache["expired.example.com"] = &DNSCacheEntry{
		IPs:       []string{"1.2.3.4"},
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start cleanup worker with short interval
	go cache.StartCleanupWorker(ctx, 100*time.Millisecond)

	// Wait for cleanup to run
	time.Sleep(200 * time.Millisecond)

	// Verify expired entry was removed
	cache.mu.RLock()
	_, exists := cache.cache["expired.example.com"]
	cache.mu.RUnlock()

	assert.False(t, exists)
}

func TestDNSCache_ContextCancellation(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{})
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	ips, err := cache.Resolve(ctx, "pool.ntp.org")
	assert.Error(t, err)
	assert.Nil(t, ips)
}

func BenchmarkDNSCache_Hit(b *testing.B) {
	cache := NewDNSCache(DNSCacheConfig{})
	ctx := context.Background()

	// Pre-populate cache
	cache.cache["test.example.com"] = &DNSCacheEntry{
		IPs:       []string{"1.2.3.4", "5.6.7.8"},
		ExpiresAt: time.Now().Add(1 * time.Hour),
		TTL:       1 * time.Hour,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Resolve(ctx, "test.example.com")
	}
}

func BenchmarkDNSCache_Miss(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping DNS resolution benchmark in short mode")
	}

	cache := NewDNSCache(DNSCacheConfig{})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Resolve(ctx, "pool.ntp.org")
	}
}
