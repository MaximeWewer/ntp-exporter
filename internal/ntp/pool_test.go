package ntp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// poolResolver answers from a fixed table and counts the lookups
type poolResolver struct {
	answers map[string][]string
	lookups int
}

func (f *poolResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	f.lookups++
	if ips, ok := f.answers[host]; ok {
		return ips, nil
	}
	return nil, errors.New("no such host")
}

func TestPool_MembersAreCappedToMaxServers(t *testing.T) {
	r := &poolResolver{answers: map[string][]string{
		"pool.example.org": {"192.0.2.1", "192.0.2.2", "192.0.2.3", "192.0.2.4", "192.0.2.5"},
	}}
	p := NewPool("pool.example.org", 3, DNSCacheConfig{}, r)

	members, total, err := p.Members(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"192.0.2.1", "192.0.2.2", "192.0.2.3"}, members, "the first addresses of the answer")
	assert.Equal(t, 5, total)
}

func TestPool_DefaultMaxServers(t *testing.T) {
	assert.Equal(t, 4, NewPool("pool.example.org", 0, DNSCacheConfig{}, nil).maxServers)
}

func TestPool_IPLiteral(t *testing.T) {
	members, total, err := NewPool("192.0.2.1", 4, DNSCacheConfig{}, &poolResolver{}).Members(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"192.0.2.1"}, members)
	assert.Equal(t, 1, total)
}

func TestPool_MembersResolutionFailure(t *testing.T) {
	_, _, err := NewPool("missing.example.org", 4, DNSCacheConfig{}, &poolResolver{}).Members(context.Background())
	assert.ErrorContains(t, err, "failed to resolve pool missing.example.org")
}

func TestPool_AnswersAreCached(t *testing.T) {
	r := &poolResolver{answers: map[string][]string{"pool.example.org": {"192.0.2.1"}}}
	p := NewPool("pool.example.org", 4, DNSCacheConfig{MinTTL: time.Hour, MaxTTL: time.Hour}, r)
	now := time.Unix(1_800_000_000, 0)
	p.dnsCache.now = func() time.Time { return now }

	for range 3 {
		_, _, err := p.Members(context.Background())
		require.NoError(t, err)
	}
	assert.Equal(t, 1, r.lookups, "answers are served from the cache")

	now = now.Add(2 * time.Hour)
	_, _, err := p.Members(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, r.lookups, "an expired answer is resolved again")
}
