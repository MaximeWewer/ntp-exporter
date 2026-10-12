package ntp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeResolver answers from a static table and counts lookups
type fakeResolver struct {
	mu      sync.Mutex
	answers map[string][]string
	err     error
	calls   int
}

func (f *fakeResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.answers[host], nil
}

func newTestCache(r *fakeResolver, ttl time.Duration) *DNSCache {
	c := NewDNSCache(DNSCacheConfig{MinTTL: ttl, MaxTTL: ttl})
	c.resolver = r
	return c
}

func TestNewDNSCache_Defaults(t *testing.T) {
	c := NewDNSCache(DNSCacheConfig{})
	assert.Equal(t, 5*time.Minute, c.minTTL)
	assert.Equal(t, 60*time.Minute, c.maxTTL)
}

func TestDNSCache_IPAddressIsNotResolved(t *testing.T) {
	r := &fakeResolver{}
	ips, cached, err := newTestCache(r, time.Hour).ResolveCached(context.Background(), "192.0.2.1")
	require.NoError(t, err)
	assert.Equal(t, []string{"192.0.2.1"}, ips)
	assert.True(t, cached)
	assert.Zero(t, r.calls)
}

func TestDNSCache_CachesAnswers(t *testing.T) {
	r := &fakeResolver{answers: map[string][]string{"pool.example.org": {"192.0.2.1", "192.0.2.2"}}}
	c := newTestCache(r, time.Hour)

	ips, cached, err := c.ResolveCached(context.Background(), "pool.example.org")
	require.NoError(t, err)
	assert.False(t, cached)
	assert.Len(t, ips, 2)

	_, cached, err = c.ResolveCached(context.Background(), "pool.example.org")
	require.NoError(t, err)
	assert.True(t, cached)
	assert.Equal(t, 1, r.calls)
}

func TestDNSCache_ExpiredEntryIsResolvedAgain(t *testing.T) {
	r := &fakeResolver{answers: map[string][]string{"pool.example.org": {"192.0.2.1"}}}
	c := newTestCache(r, time.Nanosecond)

	_, _, _ = c.ResolveCached(context.Background(), "pool.example.org")
	time.Sleep(time.Millisecond)
	_, cached, err := c.ResolveCached(context.Background(), "pool.example.org")
	require.NoError(t, err)
	assert.False(t, cached)
	assert.Equal(t, 2, r.calls)
}

func TestDNSCache_StaleEntryOnFailure(t *testing.T) {
	r := &fakeResolver{answers: map[string][]string{"pool.example.org": {"192.0.2.1"}}}
	c := newTestCache(r, time.Nanosecond)
	_, _, _ = c.ResolveCached(context.Background(), "pool.example.org")
	time.Sleep(time.Millisecond)

	r.err = errors.New("SERVFAIL")
	ips, cached, err := c.ResolveCached(context.Background(), "pool.example.org")
	require.NoError(t, err, "stale answer is served when resolution fails")
	assert.True(t, cached)
	assert.Equal(t, []string{"192.0.2.1"}, ips)
}

func TestDNSCache_FailureWithoutEntry(t *testing.T) {
	c := newTestCache(&fakeResolver{err: errors.New("NXDOMAIN")}, time.Hour)
	_, _, err := c.ResolveCached(context.Background(), "missing.example.org")
	assert.Error(t, err)
}

func TestDNSCache_AdaptiveTTL(t *testing.T) {
	c := NewDNSCache(DNSCacheConfig{MinTTL: time.Minute, MaxTTL: 9 * time.Minute})
	assert.Equal(t, 5*time.Minute, c.calculateAdaptiveTTL(false, nil), "first resolution uses the middle of the range")
	assert.Equal(t, 9*time.Minute, c.calculateAdaptiveTTL(true, &DNSCacheEntry{}))
	assert.Equal(t, time.Minute, c.calculateAdaptiveTTL(true, &DNSCacheEntry{ErrorCount: 1}))
}
