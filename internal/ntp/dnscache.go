package ntp

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/maximewewer/ntp-exporter/pkg/logger"
)

// DNSCacheEntry represents a cached DNS resolution
type DNSCacheEntry struct {
	IPs        []string
	ExpiresAt  time.Time
	TTL        time.Duration
	ErrorCount int
}

// DNSCache provides intelligent DNS caching with adaptive TTL
type DNSCache struct {
	mu      sync.RWMutex
	cache   map[string]*DNSCacheEntry
	minTTL  time.Duration
	maxTTL  time.Duration
	resolver *net.Resolver
}

// DNSCacheConfig configures the DNS cache behavior
type DNSCacheConfig struct {
	MinTTL time.Duration // Minimum TTL (default: 5min)
	MaxTTL time.Duration // Maximum TTL (default: 60min)
}

// NewDNSCache creates a new DNS cache with adaptive TTL
func NewDNSCache(config DNSCacheConfig) *DNSCache {
	if config.MinTTL == 0 {
		config.MinTTL = 5 * time.Minute
	}
	if config.MaxTTL == 0 {
		config.MaxTTL = 60 * time.Minute
	}

	return &DNSCache{
		cache:  make(map[string]*DNSCacheEntry),
		minTTL: config.MinTTL,
		maxTTL: config.MaxTTL,
		resolver: &net.Resolver{
			PreferGo: true,
		},
	}
}

// Resolve resolves a hostname with intelligent caching
func (c *DNSCache) Resolve(ctx context.Context, hostname string) ([]string, error) {
	// Check if IP address (no resolution needed)
	if net.ParseIP(hostname) != nil {
		return []string{hostname}, nil
	}

	// Check cache
	c.mu.RLock()
	entry, exists := c.cache[hostname]
	c.mu.RUnlock()

	if exists && time.Now().Before(entry.ExpiresAt) {
		logger.SafeDebug("dns", "DNS cache hit", map[string]interface{}{
			"hostname": hostname,
			"ips":      len(entry.IPs),
			"ttl":      entry.TTL.String(),
		})
		return entry.IPs, nil
	}

	// Cache miss or expired - perform resolution
	logger.SafeDebug("dns", "DNS cache miss, resolving", map[string]interface{}{
		"hostname": hostname,
	})

	ips, err := c.resolveWithTimeout(ctx, hostname)
	if err != nil {
		// On error, try to use stale cache if available
		if exists {
			c.mu.Lock()
			entry.ErrorCount++
			c.mu.Unlock()

			logger.SafeWarn("dns", "DNS resolution failed, using stale cache", map[string]interface{}{
				"hostname":    hostname,
				"error":       err.Error(),
				"error_count": entry.ErrorCount,
			})
			return entry.IPs, nil
		}
		return nil, err
	}

	// Calculate adaptive TTL based on success/failure history
	ttl := c.calculateAdaptiveTTL(exists, entry)

	// Update cache
	c.mu.Lock()
	c.cache[hostname] = &DNSCacheEntry{
		IPs:        ips,
		ExpiresAt:  time.Now().Add(ttl),
		TTL:        ttl,
		ErrorCount: 0,
	}
	c.mu.Unlock()

	logger.SafeDebug("dns", "DNS cache updated", map[string]interface{}{
		"hostname": hostname,
		"ips":      len(ips),
		"ttl":      ttl.String(),
	})

	return ips, nil
}

// resolveWithTimeout performs DNS resolution with timeout
func (c *DNSCache) resolveWithTimeout(ctx context.Context, hostname string) ([]string, error) {
	// Create context with timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}

	addrs, err := c.resolver.LookupHost(ctx, hostname)
	if err != nil {
		return nil, err
	}

	return addrs, nil
}

// calculateAdaptiveTTL calculates TTL based on resolution history
func (c *DNSCache) calculateAdaptiveTTL(exists bool, entry *DNSCacheEntry) time.Duration {
	// Start with default TTL (middle of range)
	ttl := (c.minTTL + c.maxTTL) / 2

	if exists {
		// If previous resolution had errors, use shorter TTL
		if entry.ErrorCount > 0 {
			ttl = c.minTTL
			logger.SafeDebug("dns", "Using minimum TTL due to previous errors", map[string]interface{}{
				"error_count": entry.ErrorCount,
				"ttl":         ttl.String(),
			})
		} else {
			// Successful resolution history - increase TTL
			ttl = c.maxTTL
		}
	}

	return ttl
}

// Invalidate removes a hostname from the cache
func (c *DNSCache) Invalidate(hostname string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.cache, hostname)
	logger.SafeDebug("dns", "DNS cache invalidated", map[string]interface{}{
		"hostname": hostname,
	})
}

// Clear removes all entries from the cache
func (c *DNSCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string]*DNSCacheEntry)
	logger.Info("dns", "DNS cache cleared")
}

// Stats returns cache statistics
func (c *DNSCache) Stats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	totalEntries := len(c.cache)
	validEntries := 0
	expiredEntries := 0

	now := time.Now()
	for _, entry := range c.cache {
		if now.Before(entry.ExpiresAt) {
			validEntries++
		} else {
			expiredEntries++
		}
	}

	return map[string]interface{}{
		"total_entries":   totalEntries,
		"valid_entries":   validEntries,
		"expired_entries": expiredEntries,
		"min_ttl":         c.minTTL.String(),
		"max_ttl":         c.maxTTL.String(),
	}
}

// CleanupExpired removes expired entries (should be called periodically)
func (c *DNSCache) CleanupExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	removed := 0

	for hostname, entry := range c.cache {
		if now.After(entry.ExpiresAt) {
			delete(c.cache, hostname)
			removed++
		}
	}

	if removed > 0 {
		logger.SafeDebug("dns", "Cleaned up expired DNS entries", map[string]interface{}{
			"removed": removed,
		})
	}

	return removed
}

// StartCleanupWorker starts a background worker to clean expired entries
func (c *DNSCache) StartCleanupWorker(ctx context.Context, interval time.Duration) {
	if interval == 0 {
		interval = 10 * time.Minute
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info("dns", "DNS cache cleanup worker started")

	for {
		select {
		case <-ctx.Done():
			logger.Info("dns", "DNS cache cleanup worker stopped")
			return
		case <-ticker.C:
			c.CleanupExpired()
		}
	}
}
