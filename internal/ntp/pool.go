package ntp

import (
	"context"
	"fmt"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/logger"
)

// Pool is an NTP pool: a DNS name resolving to several servers
type Pool struct {
	name       string
	strategy   string
	maxServers int
	fallback   string
	dnsCache   *DNSCache
	// lastDNSDuration is the duration of the last real (non cached) resolution
	lastDNSDuration time.Duration
}

// NewPool creates a pool whose DNS answers are cached with the given TTLs
func NewPool(name, strategy string, maxServers int, fallback string, dns DNSCacheConfig) *Pool {
	if strategy == "" {
		strategy = "best_n"
	}
	if maxServers == 0 {
		maxServers = 4
	}

	return &Pool{
		name:       name,
		strategy:   strategy,
		maxServers: maxServers,
		fallback:   fallback,
		dnsCache:   NewDNSCache(dns),
	}
}

// Name returns the pool DNS name
func (p *Pool) Name() string {
	return p.name
}

// Members resolves the pool and returns the addresses to query according to
// the strategy, the total number of resolved addresses and the duration of
// the last real DNS resolution (cache hits do not reset it)
func (p *Pool) Members(ctx context.Context) ([]string, int, time.Duration, error) {
	start := time.Now()

	ips, cached, err := p.dnsCache.ResolveCached(ctx, p.name)
	if !cached {
		p.lastDNSDuration = time.Since(start)
	}
	duration := p.lastDNSDuration
	if err != nil {
		if p.fallback != "" {
			logger.SafeWarn("ntp", "Pool resolution failed, using fallback server", map[string]interface{}{
				"pool":     p.name,
				"fallback": p.fallback,
				"error":    err.Error(),
			})
			return []string{p.fallback}, 1, duration, nil
		}
		return nil, 0, duration, fmt.Errorf("failed to resolve pool %s: %w", p.name, err)
	}

	selected := p.selectServersByStrategy(ips)
	if len(selected) > p.maxServers {
		selected = selected[:p.maxServers]
	}

	return selected, len(ips), duration, nil
}

// selectServersByStrategy selects servers based on the configured strategy
func (p *Pool) selectServersByStrategy(servers []string) []string {
	switch p.strategy {
	case "round_robin":
		// One server per cycle, rotating over time across all servers
		if len(servers) == 0 {
			return []string{}
		}
		index := int(time.Now().Unix()) % len(servers)
		return []string{servers[index]}

	default:
		// "all" and "best_n": every resolved server, capped to max_servers
		return servers
	}
}
