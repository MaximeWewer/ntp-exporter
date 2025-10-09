package ntp

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/time/rate"
)

// RateLimiter manages rate limiting for NTP queries
type RateLimiter struct {
	global        *rate.Limiter
	perServer     map[string]*rate.Limiter
	mu            sync.RWMutex
	perServerRate int
	burstSize     int
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(globalRate, perServerRate, burstSize int) *RateLimiter {
	return &RateLimiter{
		global:        rate.NewLimiter(rate.Limit(globalRate), burstSize),
		perServer:     make(map[string]*rate.Limiter),
		perServerRate: perServerRate,
		burstSize:     burstSize,
	}
}

// Wait waits for permission to make a query to the specified server
func (rl *RateLimiter) Wait(ctx context.Context, server string) error {
	// Global rate limit
	if err := rl.global.Wait(ctx); err != nil {
		return fmt.Errorf("global rate limit: %w", err)
	}

	// Per-server rate limit
	limiter := rl.getLimiterForServer(server)
	if err := limiter.Wait(ctx); err != nil {
		return fmt.Errorf("per-server rate limit for %s: %w", server, err)
	}

	return nil
}

// getLimiterForServer gets or creates a rate limiter for a server
func (rl *RateLimiter) getLimiterForServer(server string) *rate.Limiter {
	rl.mu.RLock()
	limiter, exists := rl.perServer[server]
	rl.mu.RUnlock()

	if exists {
		return limiter
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	if limiter, exists := rl.perServer[server]; exists {
		return limiter
	}

	limiter = rate.NewLimiter(rate.Limit(rl.perServerRate), rl.burstSize)
	rl.perServer[server] = limiter
	return limiter
}

// Allow checks if a query is allowed without waiting
func (rl *RateLimiter) Allow(server string) bool {
	if !rl.global.Allow() {
		return false
	}
	limiter := rl.getLimiterForServer(server)
	return limiter.Allow()
}
