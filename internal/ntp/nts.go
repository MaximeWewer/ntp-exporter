package ntp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/beevik/ntp"
	"github.com/beevik/nts"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
)

// NTSScheme prefixes the servers queried with Network Time Security (RFC 8915)
const NTSScheme = "nts://"

// IsNTS reports whether a configured server uses NTS
func IsNTS(server string) bool {
	return strings.HasPrefix(server, NTSScheme)
}

// NTSHost returns the key exchange address of an NTS server: host or host:port
// (default port 4460)
func NTSHost(server string) string {
	return strings.TrimPrefix(server, NTSScheme)
}

const (
	// ntsKeyExchangeInterval renews the key exchange regularly, so that the
	// key exchange server and its certificate are monitored too
	ntsKeyExchangeInterval = time.Hour
	// maxNTSSessions bounds the sessions kept for /probe targets
	maxNTSSessions = 1024
)

// NTSState describes the key exchange of an NTS server
type NTSState struct {
	// KESuccess is set when the last key exchange succeeded
	KESuccess bool
	// KEError is the error of the last key exchange
	KEError error
	// KETime is the time of the last key exchange
	KETime time.Time
	// CertExpiry is the expiry of the key exchange server certificate
	CertExpiry time.Time
	// Address is the NTP server "host:port" negotiated by the key exchange
	Address string
}

// ntsSession is the part of nts.Session used by the client
type ntsSession interface {
	QueryWithOptions(opt *ntp.QueryOptions) (*ntp.Response, error)
	Address() string
}

// ntsTarget holds the session of one NTS server. A session holds cookies
// consumed by each query: queries of a server are serialized by slot, a
// semaphore that a caller stops waiting for when its context ends.
type ntsTarget struct {
	slot    chan struct{}
	session ntsSession
	state   NTSState
	// used orders the uses of the targets, for the LRU eviction
	used uint64
}

// NTSClient queries NTS servers. A session (keys and cookies) is kept per
// server across cycles; the key exchange runs again every hour, after an
// authentication failure or when the cookies run out.
type NTSClient struct {
	timeout time.Duration
	spacing time.Duration
	now     func() time.Time
	// keyExchange performs a key exchange and returns the session and the
	// certificate expiry of the key exchange server
	keyExchange func(host string, timeout time.Duration) (ntsSession, time.Time, error)

	mu      sync.Mutex
	targets map[string]*ntsTarget
	tick    uint64
}

// NewNTSClient creates an NTS client
func NewNTSClient(timeout, spacing time.Duration) *NTSClient {
	return &NTSClient{
		timeout:     timeout,
		spacing:     spacing,
		now:         time.Now,
		keyExchange: keyExchange,
		targets:     make(map[string]*ntsTarget),
	}
}

// keyExchange performs an NTS-KE (TLS 1.3, port 4460 by default) and records
// the certificate of the key exchange server
func keyExchange(host string, timeout time.Duration) (ntsSession, time.Time, error) {
	var expiry time.Time
	session, err := nts.NewSessionWithOptions(host, &nts.SessionOptions{
		Timeout: timeout,
		Dialer: func(network, addr string, tlsConfig *tls.Config) (*tls.Conn, error) {
			conn, err := tls.DialWithDialer(&net.Dialer{Timeout: timeout}, network, addr, tlsConfig)
			if err != nil {
				return nil, err
			}
			// Bound the whole exchange, not only the handshake
			_ = conn.SetDeadline(time.Now().Add(timeout))
			if certs := conn.ConnectionState().PeerCertificates; len(certs) > 0 {
				expiry = certs[0].NotAfter
			}
			return conn, nil
		},
	})
	if err != nil {
		return nil, expiry, err
	}
	return session, expiry, nil
}

// acquire takes the slot of the server, waiting for it until the context
// ends. A free slot is always taken, even with an expired context.
func (t *ntsTarget) acquire(ctx context.Context) bool {
	select {
	case t.slot <- struct{}{}:
		return true
	default:
	}
	select {
	case t.slot <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

// target returns the state of an NTS server
func (c *NTSClient) target(host string) *ntsTarget {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.targets[host]
	if !ok {
		if len(c.targets) >= maxNTSSessions {
			// Evict the least recently used session: a burst still holding
			// it keeps its own reference
			var oldest string
			var oldestUse uint64
			for h, s := range c.targets {
				if oldest == "" || s.used < oldestUse {
					oldest, oldestUse = h, s.used
				}
			}
			delete(c.targets, oldest)
		}
		t = &ntsTarget{slot: make(chan struct{}, 1)}
		c.targets[host] = t
	}
	c.tick++
	t.used = c.tick
	return t
}

// ErrNTSBusy is returned when the context ends while another burst or key
// exchange of the same server is in progress
var ErrNTSBusy = errors.New("NTS server busy")

// NTSResult is the result of a burst to an NTS server
type NTSResult struct {
	Responses []*Response
	// Sent is the number of NTP queries sent: 0 when the key exchange failed
	Sent int
	// State describes the last key exchange
	State NTSState
	// Err is set when no response was received
	Err error
}

// Burst sends a burst of authenticated queries to an NTS server, after a key
// exchange when needed. Without a session, no query is sent.
func (c *NTSClient) Burst(ctx context.Context, host string, count int) NTSResult {
	t := c.target(host)
	if !t.acquire(ctx) {
		// Another burst or key exchange of this server is in progress
		return NTSResult{Err: fmt.Errorf("%w: %s: %w", ErrNTSBusy, host, ctx.Err())}
	}
	defer func() { <-t.slot }()

	if t.session == nil || c.now().Sub(t.state.KETime) >= ntsKeyExchangeInterval {
		c.renew(ctx, t, host)
	}
	if t.session == nil {
		return NTSResult{State: t.state, Err: fmt.Errorf("NTS key exchange with %s failed: %w", host, t.state.KEError)}
	}

	res := NTSResult{Responses: make([]*Response, 0, count)}
	var lastErr error
	for i := 0; i < count; i++ {
		if i > 0 {
			if err := wait(ctx, c.spacing); err != nil {
				lastErr = err
				break
			}
		}

		res.Sent++
		resp, err := c.query(ctx, t.session)
		if r, ok := protocolErrorResponse(NTSScheme+host, err); ok {
			res.Responses = append(res.Responses, r)
			continue
		}
		if err != nil {
			lastErr = err
			logger.SafeDebug("ntp", "NTS query failed", map[string]interface{}{
				"server": host,
				"error":  err.Error(),
			})
			if ctx.Err() != nil {
				// The abandoned query may still use the session: drop it
				t.session = nil
				break
			}
			if errors.Is(err, nts.ErrNoCookies) || errors.Is(err, nts.ErrAuthFailedOnClient) ||
				errors.Is(err, nts.ErrAuthFailedOnServer) {
				// The session is unusable: a new key exchange runs next time
				t.session = nil
				break
			}
			continue
		}

		// A NTSN Kiss-o'-Death (cookie not decrypted) is reported by
		// beevik/nts as ErrAuthFailedOnServer, handled above. Other
		// Kiss-o'-Death codes are not authenticated and are dropped by
		// beevik/nts (RFC 8915 §5.7): NTS targets get no RATE backoff.
		res.Responses = append(res.Responses, fromNTP(NTSScheme+host, resp))
	}

	res.State = t.state
	if len(res.Responses) == 0 {
		if lastErr == nil {
			lastErr = errors.New("no NTS response")
		}
		res.Err = fmt.Errorf("all NTS queries failed for server %s: %w", host, lastErr)
	}
	return res
}

// renew performs a key exchange and replaces the session
func (c *NTSClient) renew(ctx context.Context, t *ntsTarget, host string) {
	type result struct {
		session ntsSession
		expiry  time.Time
		err     error
	}
	done := make(chan result, 1)
	go func() {
		s, expiry, err := c.keyExchange(host, c.timeout)
		done <- result{s, expiry, err}
	}()

	var r result
	select {
	case <-ctx.Done():
		r.err = ctx.Err()
	case r = <-done:
	}

	t.state.KETime = c.now()
	t.state.KESuccess = r.err == nil
	t.state.KEError = r.err
	if !r.expiry.IsZero() {
		t.state.CertExpiry = r.expiry
	}
	if r.err != nil {
		t.session = nil
		t.state.Address = ""
		logger.SafeWarn("ntp", "NTS key exchange failed", map[string]interface{}{
			"server": host,
			"error":  r.err.Error(),
		})
		return
	}
	t.session = r.session
	t.state.Address = r.session.Address()
}

// query sends one authenticated query, bounded by the context
func (c *NTSClient) query(ctx context.Context, session ntsSession) (*ntp.Response, error) {
	type result struct {
		resp *ntp.Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		// NTS requires NTPv4
		resp, err := session.QueryWithOptions(&ntp.QueryOptions{Timeout: c.timeout, Version: 4})
		done <- result{resp, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-done:
		return r.resp, r.err
	}
}

// wait waits for d unless the context ends first
func wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
