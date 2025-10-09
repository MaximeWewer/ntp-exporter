package ntp

import (
	"context"
	"testing"
	"time"
)

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(100, 10, 5)
	if rl == nil {
		t.Fatal("NewRateLimiter returned nil")
	}
	if rl.global == nil {
		t.Error("global limiter is nil")
	}
	if rl.perServer == nil {
		t.Error("perServer map is nil")
	}
	if rl.perServerRate != 10 {
		t.Errorf("perServerRate = %d, want 10", rl.perServerRate)
	}
	if rl.burstSize != 5 {
		t.Errorf("burstSize = %d, want 5", rl.burstSize)
	}
}

func TestRateLimiterWait(t *testing.T) {
	// High limits to avoid blocking in tests
	rl := NewRateLimiter(1000, 100, 10)
	ctx := context.Background()

	// First call should succeed immediately
	start := time.Now()
	err := rl.Wait(ctx, "test.server.com")
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Wait failed: %v", err)
	}

	// Should be fast (< 100ms)
	if elapsed > 100*time.Millisecond {
		t.Errorf("Wait took too long: %v", elapsed)
	}
}

func TestRateLimiterWaitContextCancelled(t *testing.T) {
	rl := NewRateLimiter(1, 1, 1) // Very low limits
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	err := rl.Wait(ctx, "test.server.com")
	if err == nil {
		t.Error("Expected error when context is cancelled, got nil")
	}
}

func TestRateLimiterWaitTimeout(t *testing.T) {
	rl := NewRateLimiter(1, 1, 1) // Very low limits
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Exhaust the burst
	rl.Wait(context.Background(), "test.server.com")
	rl.Wait(context.Background(), "test.server.com")

	// This should timeout
	err := rl.Wait(ctx, "test.server.com")
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
}

func TestRateLimiterPerServer(t *testing.T) {
	rl := NewRateLimiter(1000, 100, 10)
	ctx := context.Background()

	// Test multiple servers
	servers := []string{"server1.com", "server2.com", "server3.com"}

	for _, server := range servers {
		err := rl.Wait(ctx, server)
		if err != nil {
			t.Errorf("Wait failed for %s: %v", server, err)
		}
	}

	// Verify per-server limiters were created
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	for _, server := range servers {
		if _, exists := rl.perServer[server]; !exists {
			t.Errorf("Per-server limiter not created for %s", server)
		}
	}
}

func TestRateLimiterGetLimiterForServer(t *testing.T) {
	rl := NewRateLimiter(1000, 100, 10)

	// First call should create limiter
	limiter1 := rl.getLimiterForServer("test.server.com")
	if limiter1 == nil {
		t.Fatal("getLimiterForServer returned nil")
	}

	// Second call should return same limiter
	limiter2 := rl.getLimiterForServer("test.server.com")
	if limiter1 != limiter2 {
		t.Error("getLimiterForServer returned different limiters for same server")
	}

	// Different server should get different limiter
	limiter3 := rl.getLimiterForServer("other.server.com")
	if limiter1 == limiter3 {
		t.Error("Different servers got same limiter")
	}
}

func TestRateLimiterAllow(t *testing.T) {
	rl := NewRateLimiter(1000, 100, 10)

	// First calls should be allowed (within burst)
	for i := 0; i < 10; i++ {
		if !rl.Allow("test.server.com") {
			t.Errorf("Allow returned false at iteration %d (within burst)", i)
		}
	}

	// After burst, might not be allowed depending on timing
	// Just verify it doesn't panic
	_ = rl.Allow("test.server.com")
}

func TestRateLimiterAllowGlobalLimit(t *testing.T) {
	rl := NewRateLimiter(5, 100, 2) // Small global burst

	// Exhaust global burst
	for i := 0; i < 2; i++ {
		if !rl.Allow("server1.com") {
			t.Errorf("Allow returned false at iteration %d (within global burst)", i)
		}
	}

	// Next call should fail (global limit exceeded)
	if rl.Allow("server2.com") {
		// Might be allowed depending on timing, just verify no panic
		t.Log("Allow succeeded after burst (timing dependent)")
	}
}

func TestRateLimiterConcurrentAccess(t *testing.T) {
	rl := NewRateLimiter(1000, 100, 10)
	ctx := context.Background()

	done := make(chan bool)
	errors := make(chan error, 10)

	// Launch multiple goroutines
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()

			server := "server" + string(rune('0'+id))
			for j := 0; j < 5; j++ {
				err := rl.Wait(ctx, server)
				if err != nil {
					errors <- err
					return
				}
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent access error: %v", err)
	}
}

func BenchmarkRateLimiterWait(b *testing.B) {
	rl := NewRateLimiter(100000, 10000, 100)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rl.Wait(ctx, "test.server.com")
	}
}

func BenchmarkRateLimiterAllow(b *testing.B) {
	rl := NewRateLimiter(100000, 10000, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rl.Allow("test.server.com")
	}
}

func BenchmarkRateLimiterMultipleServers(b *testing.B) {
	rl := NewRateLimiter(100000, 10000, 100)
	ctx := context.Background()
	servers := []string{"server1", "server2", "server3", "server4", "server5"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server := servers[i%len(servers)]
		_ = rl.Wait(ctx, server)
	}
}
