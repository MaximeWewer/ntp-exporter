package ntp

import (
	"context"
	"fmt"
)

// Pool is an NTP pool: a DNS name resolving to several servers
type Pool struct {
	name       string
	maxServers int
	dnsCache   *DNSCache
}

// NewPool creates a pool whose DNS answers are cached with the given TTLs.
// The first maxServers addresses of each answer are queried: DNS servers of
// public pools rotate their answers.
func NewPool(name string, maxServers int, dns DNSCacheConfig, resolver HostResolver) *Pool {
	if maxServers == 0 {
		maxServers = 4
	}
	cache := NewDNSCache(dns)
	if resolver != nil {
		cache.resolver = resolver
	}
	return &Pool{name: name, maxServers: maxServers, dnsCache: cache}
}

// SetResolver replaces the resolver of the pool
func (p *Pool) SetResolver(r HostResolver) {
	p.dnsCache.resolver = r
}

// Name returns the pool DNS name
func (p *Pool) Name() string {
	return p.name
}

// Members resolves the pool and returns the addresses to query and the total
// number of resolved addresses
func (p *Pool) Members(ctx context.Context) ([]string, int, error) {
	ips, _, err := p.dnsCache.ResolveCached(ctx, p.name)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to resolve pool %s: %w", p.name, err)
	}
	return ips[:min(len(ips), p.maxServers)], len(ips), nil
}
