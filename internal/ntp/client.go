package ntp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/beevik/ntp"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
)

// NTPQuerier is an interface for querying NTP servers
type NTPQuerier interface {
	Query(ctx context.Context, server string) (*Response, error)
	// QueryMultiple sends a burst of queries and returns the responses and the
	// number of queries actually sent: the burst stops at the first
	// Kiss-o'-Death, as required by RFC 5905
	QueryMultiple(ctx context.Context, server string, count int) ([]*Response, int, error)
}

// Client is an enhanced NTP client with advanced features
type Client struct {
	timeout time.Duration
	version int
	// spacing is the delay between two queries of a burst to the same server
	spacing time.Duration
}

// Response represents an NTP query response with additional metadata
type Response struct {
	Server         string
	Offset         time.Duration
	RTT            time.Duration
	Stratum        uint8
	ReferenceTime  time.Time
	RootDelay      time.Duration
	RootDispersion time.Duration
	RootDistance   time.Duration
	Precision      time.Duration
	LeapIndicator  uint8
	Poll           time.Duration
	ValidateError  error
	ReferenceID    uint32
	Time           time.Time
	MinError       time.Duration
	KissCode       string
}

// NewClient creates a new NTP client
func NewClient(timeout time.Duration, version int) *Client {
	return &Client{
		timeout: timeout,
		version: version,
		spacing: DelayBetweenSamples,
	}
}

// SetSampleSpacing sets the delay between two queries of a burst
func (c *Client) SetSampleSpacing(spacing time.Duration) {
	c.spacing = spacing
}

// Query performs a single NTP query to the specified server
func (c *Client) Query(ctx context.Context, server string) (*Response, error) {
	// Create NTP query options
	opts := ntp.QueryOptions{
		Timeout: c.timeout,
		Version: c.version,
	}

	// Structure to encapsulate the result
	type queryResult struct {
		response *ntp.Response
		err      error
	}

	// Buffered channel to prevent goroutine leak
	resultChan := make(chan queryResult, 1)

	go func() {
		resp, err := ntp.QueryWithOptions(server, opts)
		// Non-blocking write thanks to buffer
		resultChan <- queryResult{response: resp, err: err}
	}()

	select {
	case <-ctx.Done():
		// The goroutine will finish and write to the buffered channel
		// The garbage collector will clean everything up
		return nil, fmt.Errorf("query context cancelled: %w", ctx.Err())
	case result := <-resultChan:
		if result.err != nil {
			logger.SafeDebug("ntp", "NTP query failed", map[string]interface{}{
				"server": server,
				"error":  result.err.Error(),
			})
			return nil, fmt.Errorf("ntp query to %s failed: %w", server, result.err)
		}

		resp := fromNTP(server, result.response)

		logger.SafeDebug("ntp", "NTP query successful", map[string]interface{}{
			"server":  server,
			"offset":  resp.Offset.Seconds(),
			"rtt":     resp.RTT.Seconds(),
			"stratum": resp.Stratum,
		})

		return resp, nil
	}
}

// fromNTP converts a beevik/ntp response. The response is validated once:
// invalid responses (KoD, unsynchronized server, excessive root distance...)
// are returned but flagged so callers never publish them as measurements.
func fromNTP(server string, r *ntp.Response) *Response {
	validateErr := r.Validate()
	if validateErr != nil {
		logger.SafeDebug("ntp", "NTP response validation failed", map[string]interface{}{
			"server": server,
			"error":  validateErr.Error(),
		})
	}

	return &Response{
		Server:         server,
		Offset:         r.ClockOffset,
		RTT:            r.RTT,
		Stratum:        r.Stratum,
		ReferenceTime:  r.ReferenceTime,
		RootDelay:      r.RootDelay,
		RootDispersion: r.RootDispersion,
		RootDistance:   r.RootDistance,
		Precision:      r.Precision,
		LeapIndicator:  uint8(r.Leap),
		Poll:           r.Poll,
		ValidateError:  validateErr,
		ReferenceID:    r.ReferenceID,
		Time:           r.Time,
		MinError:       r.MinError,
		KissCode:       r.KissCode,
	}
}

// QueryMultiple performs a burst of NTP queries and returns every response
// and the number of queries sent. A Kiss-o'-Death ends the burst: the server
// asked the client to slow down or stop (RFC 5905 §7.4).
func (c *Client) QueryMultiple(ctx context.Context, server string, count int) ([]*Response, int, error) {
	responses := make([]*Response, 0, count)
	sent := 0
	var lastErr error

	for i := 0; i < count; i++ {
		select {
		case <-ctx.Done():
			return responses, sent, ctx.Err()
		default:
		}

		sent++
		resp, err := c.Query(ctx, server)
		if err != nil {
			lastErr = err
			logger.SafeDebug("ntp", "NTP query attempt failed", map[string]interface{}{
				"server":  server,
				"attempt": i + 1,
				"error":   err.Error(),
			})
			continue
		}

		responses = append(responses, resp)
		if resp.IsKissOfDeath() {
			logger.SafeDebug("ntp", "Kiss-o'-Death received, burst stopped", map[string]interface{}{
				"server":  server,
				"code":    resp.KissCode,
				"attempt": i + 1,
			})
			break
		}

		// Space queries out: NTP servers rate-limit clients sending packets
		// closer than 2s (ntpd "discard minimum", chrony "ratelimit")
		if i < count-1 && c.spacing > 0 {
			timer := time.NewTimer(c.spacing)
			select {
			case <-ctx.Done():
				timer.Stop()
				return responses, sent, ctx.Err()
			case <-timer.C:
			}
		}
	}

	if len(responses) == 0 {
		if lastErr == nil {
			lastErr = errors.New("no query sent")
		}
		return nil, sent, fmt.Errorf("all %d NTP queries failed for server %s: %w", sent, server, lastErr)
	}

	logger.SafeDebug("ntp", "Multiple NTP queries completed", map[string]interface{}{
		"server":     server,
		"successful": len(responses),
		"total":      sent,
	})

	return responses, sent, nil
}

// IsKissOfDeath checks if the response contains a Kiss-of-Death code
func (r *Response) IsKissOfDeath() bool {
	return r.KissCode != ""
}

// IsValid checks if the response can be used as a time measurement:
// it passed RFC 5905 validation, is not a Kiss-o'-Death, comes from a
// synchronized server (leap indicator != 3) and has a valid stratum (1-15)
func (r *Response) IsValid() bool {
	return r.ValidateError == nil &&
		r.KissCode == "" &&
		r.LeapIndicator != 3 &&
		r.Stratum >= 1 && r.Stratum <= 15
}

// ReferenceIDString renders the reference identifier: a 4-character ASCII
// code for stratum 0/1 sources (GPS, PPS, KoD codes...), an IPv4 address (or
// the first bytes of the IPv6 address hash) for upper strata
func (r *Response) ReferenceIDString() string {
	b := binary.BigEndian.AppendUint32(nil, r.ReferenceID)
	if r.Stratum <= 1 {
		end := len(b)
		for end > 0 && b[end-1] == 0 {
			end--
		}
		out := make([]byte, 0, end)
		for _, c := range b[:end] {
			if c < 0x20 || c > 0x7e {
				c = '?'
			}
			out = append(out, c)
		}
		return string(out)
	}
	return net.IPv4(b[0], b[1], b[2], b[3]).String()
}

// Reasons for which a response is not a valid time measurement
const (
	ReasonKoD            = "kod"
	ReasonUnsync         = "unsync"
	ReasonStratum        = "stratum"
	ReasonRootDistance   = "root_distance"
	ReasonStaleReference = "stale_reference"
	ReasonInvalidTime    = "invalid_time"
	ReasonOther          = "other"
	// ReasonTimeout is used for queries left without any response
	ReasonTimeout = "timeout"
)

// InvalidReason returns a short reason, from a closed set, explaining why the
// response is not valid, or an empty string for a valid response
func (r *Response) InvalidReason() string {
	switch {
	case r.KissCode != "" || errors.Is(r.ValidateError, ntp.ErrKissOfDeath):
		return ReasonKoD
	case r.LeapIndicator == 3 ||
		errors.Is(r.ValidateError, ntp.ErrServerNotSynchronized) ||
		errors.Is(r.ValidateError, ntp.ErrInvalidLeapSecond):
		return ReasonUnsync
	case r.Stratum < 1 || r.Stratum > 15 || errors.Is(r.ValidateError, ntp.ErrInvalidStratum):
		return ReasonStratum
	case errors.Is(r.ValidateError, ntp.ErrInvalidDispersion):
		return ReasonRootDistance
	case errors.Is(r.ValidateError, ntp.ErrServerClockFreshness):
		return ReasonStaleReference
	case errors.Is(r.ValidateError, ntp.ErrInvalidTime),
		errors.Is(r.ValidateError, ntp.ErrServerTickedBackwards),
		errors.Is(r.ValidateError, ntp.ErrInvalidTransmitTime):
		return ReasonInvalidTime
	case r.ValidateError != nil:
		return ReasonOther
	default:
		return ""
	}
}

// ValidResponses returns only the responses that can be used as time measurements
func ValidResponses(responses []*Response) []*Response {
	valid := make([]*Response, 0, len(responses))
	for _, r := range responses {
		if r != nil && r.IsValid() {
			valid = append(valid, r)
		}
	}
	return valid
}
