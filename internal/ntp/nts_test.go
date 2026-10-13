package ntp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/beevik/ntp"
	"github.com/beevik/nts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSession answers queries with prepared responses or errors
type fakeSession struct {
	mu      sync.Mutex
	address string
	results []error // per query; nil means a valid response
	queries int
	delay   time.Duration
}

func (s *fakeSession) QueryWithOptions(*ntp.QueryOptions) (*ntp.Response, error) {
	time.Sleep(s.delay)
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.queries
	s.queries++
	if i < len(s.results) && s.results[i] != nil {
		return nil, s.results[i]
	}
	now := time.Now()
	return &ntp.Response{
		Time: now, ClockOffset: time.Millisecond, RTT: 5 * time.Millisecond, Stratum: 1,
		ReferenceTime: now.Add(-time.Second), RootDistance: time.Millisecond,
	}, nil
}

func (s *fakeSession) Address() string { return s.address }

func newTestNTSClient(exchange func(host string, timeout time.Duration) (ntsSession, time.Time, error)) *NTSClient {
	c := NewNTSClient(time.Second, 0)
	c.keyExchange = exchange
	return c
}

func TestIsNTS(t *testing.T) {
	assert.True(t, IsNTS("nts://time.cloudflare.com"))
	assert.False(t, IsNTS("time.cloudflare.com"))
	assert.Equal(t, "time.cloudflare.com:4460", NTSHost("nts://time.cloudflare.com:4460"))
}

func TestNTSClient_BurstAndSessionReuse(t *testing.T) {
	expiry := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	exchanges := 0
	session := &fakeSession{address: "192.0.2.1:123"}
	c := newTestNTSClient(func(string, time.Duration) (ntsSession, time.Time, error) {
		exchanges++
		return session, expiry, nil
	})

	res := c.Burst(context.Background(), "nts.example.org", 3)
	require.NoError(t, res.Err)
	assert.Len(t, res.Responses, 3)
	assert.Equal(t, 3, res.Sent)
	assert.True(t, res.State.KESuccess)
	assert.Equal(t, expiry, res.State.CertExpiry)
	assert.Equal(t, "192.0.2.1:123", res.State.Address)
	assert.True(t, res.Responses[0].IsValid())

	c.Burst(context.Background(), "nts.example.org", 2)
	assert.Equal(t, 1, exchanges, "the session is reused across bursts")

	now := time.Now()
	c.now = func() time.Time { return now.Add(2 * ntsKeyExchangeInterval) }
	c.Burst(context.Background(), "nts.example.org", 1)
	assert.Equal(t, 2, exchanges, "the key exchange is renewed every hour")
}

func TestNTSClient_KeyExchangeFailure(t *testing.T) {
	expiry := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	fail := true
	c := newTestNTSClient(func(string, time.Duration) (ntsSession, time.Time, error) {
		if fail {
			return nil, time.Time{}, errors.New("connection refused")
		}
		return &fakeSession{address: "192.0.2.1:123"}, expiry, nil
	})

	res := c.Burst(context.Background(), "nts.example.org", 3)
	assert.ErrorContains(t, res.Err, "key exchange")
	assert.Equal(t, 0, res.Sent, "no NTP query without session")
	assert.False(t, res.State.KESuccess)

	fail = false
	res = c.Burst(context.Background(), "nts.example.org", 1)
	require.NoError(t, res.Err)
	assert.True(t, res.State.KESuccess, "a new key exchange is attempted at the next burst")
}

func TestNTSClient_SessionDroppedOnAuthFailure(t *testing.T) {
	exchanges := 0
	c := newTestNTSClient(func(string, time.Duration) (ntsSession, time.Time, error) {
		exchanges++
		if exchanges == 1 {
			return &fakeSession{results: []error{nil, nts.ErrNoCookies}}, time.Time{}, nil
		}
		return &fakeSession{}, time.Time{}, nil
	})

	res := c.Burst(context.Background(), "nts.example.org", 3)
	assert.Len(t, res.Responses, 1)
	assert.Equal(t, 2, res.Sent, "the burst stops when the cookies run out")
	assert.NoError(t, res.Err)

	c.Burst(context.Background(), "nts.example.org", 1)
	assert.Equal(t, 2, exchanges)
}

func TestNTSClient_NakDropsSession(t *testing.T) {
	exchanges := 0
	c := newTestNTSClient(func(string, time.Duration) (ntsSession, time.Time, error) {
		exchanges++
		// beevik/nts reports a NTSN Kiss-o'-Death as ErrAuthFailedOnServer
		return &fakeSession{results: []error{nil, nts.ErrAuthFailedOnServer}}, time.Time{}, nil
	})

	res := c.Burst(context.Background(), "nts.example.org", 3)
	require.Len(t, res.Responses, 1)
	assert.Equal(t, 2, res.Sent, "the burst stops when the session is rejected")

	c.Burst(context.Background(), "nts.example.org", 1)
	assert.Equal(t, 2, exchanges)
}

func TestNTSClient_AllQueriesFail(t *testing.T) {
	timeout := errors.New("i/o timeout")
	c := newTestNTSClient(func(string, time.Duration) (ntsSession, time.Time, error) {
		return &fakeSession{results: []error{timeout, timeout}}, time.Time{}, nil
	})

	res := c.Burst(context.Background(), "nts.example.org", 2)
	assert.ErrorIs(t, res.Err, timeout)
	assert.Equal(t, 2, res.Sent)
	assert.Empty(t, res.Responses)
}

func TestNTSClient_ContextCancelled(t *testing.T) {
	c := newTestNTSClient(func(string, time.Duration) (ntsSession, time.Time, error) {
		return &fakeSession{delay: time.Second}, time.Time{}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	res := c.Burst(ctx, "nts.example.org", 2)
	assert.ErrorIs(t, res.Err, context.DeadlineExceeded)

	slowKE := newTestNTSClient(func(string, time.Duration) (ntsSession, time.Time, error) {
		time.Sleep(time.Second)
		return &fakeSession{}, time.Time{}, nil
	})
	res = slowKE.Burst(ctx, "nts.example.org", 1)
	assert.False(t, res.State.KESuccess)
	assert.ErrorIs(t, res.State.KEError, context.DeadlineExceeded)
}

func TestNTSClient_BoundedSessions(t *testing.T) {
	c := newTestNTSClient(func(string, time.Duration) (ntsSession, time.Time, error) {
		return &fakeSession{}, time.Time{}, nil
	})
	for i := range maxNTSSessions + 1 {
		c.target(string(rune('a'+i%26)) + time.Duration(i).String())
	}
	assert.LessOrEqual(t, len(c.targets), maxNTSSessions)
}

func TestKeyExchange_Unreachable(t *testing.T) {
	// Port 1 on localhost refuses connections: no network access needed
	_, _, err := keyExchange("127.0.0.1:1", 500*time.Millisecond)
	assert.Error(t, err)
}

func TestNTSClient_BusyServerHonorsContext(t *testing.T) {
	c := newTestNTSClient(func(string, time.Duration) (ntsSession, time.Time, error) {
		return &fakeSession{delay: time.Second}, time.Time{}, nil
	})

	// A 1s burst holds the server
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Burst(context.Background(), "nts.example.org", 1)
	}()
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	res := c.Burst(ctx, "nts.example.org", 1)

	assert.Less(t, time.Since(start), 200*time.Millisecond, "does not wait for the burst in progress")
	assert.ErrorIs(t, res.Err, context.DeadlineExceeded)
	assert.Equal(t, 0, res.Sent)
	<-done
}
