package ntp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPool_Defaults(t *testing.T) {
	p := NewPool("pool.ntp.org", "", 0, "", DNSCacheConfig{})
	assert.Equal(t, "pool.ntp.org", p.Name())
	assert.Equal(t, "best_n", p.strategy)
	assert.Equal(t, 4, p.maxServers)
}

func TestPool_SelectServersByStrategy(t *testing.T) {
	servers := []string{"a", "b", "c"}

	assert.Equal(t, servers, NewPool("p", "all", 4, "", DNSCacheConfig{}).selectServersByStrategy(servers))
	assert.Equal(t, servers, NewPool("p", "best_n", 4, "", DNSCacheConfig{}).selectServersByStrategy(servers))

	rr := NewPool("p", "round_robin", 4, "", DNSCacheConfig{}).selectServersByStrategy(servers)
	require.Len(t, rr, 1)
	assert.Contains(t, servers, rr[0])
	assert.Empty(t, NewPool("p", "round_robin", 4, "", DNSCacheConfig{}).selectServersByStrategy(nil))
}

func TestPool_MembersFromIP(t *testing.T) {
	members, total, _, err := NewPool("192.0.2.1", "all", 4, "", DNSCacheConfig{}).Members(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"192.0.2.1"}, members)
	assert.Equal(t, 1, total)
}

func TestPool_MembersFallback(t *testing.T) {
	p := NewPool("does-not-exist.invalid", "all", 4, "192.0.2.9", DNSCacheConfig{})
	members, total, _, err := p.Members(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"192.0.2.9"}, members)
	assert.Equal(t, 1, total)
}

func TestPool_MembersResolutionFailure(t *testing.T) {
	_, _, _, err := NewPool("does-not-exist.invalid", "all", 4, "", DNSCacheConfig{}).Members(context.Background())
	assert.Error(t, err)
}

func TestPool_MembersDNSDurationIgnoresCacheHits(t *testing.T) {
	p := NewPool("localhost", "all", 4, "", DNSCacheConfig{MinTTL: time.Hour, MaxTTL: time.Hour})

	_, _, first, err := p.Members(context.Background())
	if err != nil {
		t.Skipf("localhost resolution unavailable: %v", err)
	}
	_, _, second, err := p.Members(context.Background())
	require.NoError(t, err)
	assert.Equal(t, first, second, "cache hit must keep the last real resolution duration")
}
