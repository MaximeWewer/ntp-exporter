package ntp

import (
	"context"
	"fmt"
	"time"

	"github.com/beevik/ntp"
	"github.com/maximewewer/ntp-exporter/pkg/logger"
	"github.com/maximewewer/ntp-exporter/pkg/mathutil"
)

// Client is an enhanced NTP client with advanced features
type Client struct {
	timeout     time.Duration
	version     int
	rateLimiter *RateLimiter
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

// NewClient creates a new NTP client without rate limiting
func NewClient(timeout time.Duration, version int) *Client {
	return &Client{
		timeout:     timeout,
		version:     version,
		rateLimiter: nil,
	}
}

// NewClientWithRateLimit creates a new NTP client with rate limiting enabled
func NewClientWithRateLimit(timeout time.Duration, version int, globalRate, perServerRate, burstSize int) *Client {
	var limiter *RateLimiter
	if globalRate > 0 {
		limiter = NewRateLimiter(globalRate, perServerRate, burstSize)
	}

	return &Client{
		timeout:     timeout,
		version:     version,
		rateLimiter: limiter,
	}
}

// Query performs a single NTP query to the specified server
func (c *Client) Query(ctx context.Context, server string) (*Response, error) {
	// Apply rate limiting if enabled
	if c.rateLimiter != nil {
		if err := c.rateLimiter.Wait(ctx, server); err != nil {
			return nil, fmt.Errorf("rate limit exceeded: %w", err)
		}
	}

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

		// Validate response
		if err := result.response.Validate(); err != nil {
			logger.SafeWarn("ntp", "NTP response validation failed", map[string]interface{}{
				"server": server,
				"error":  err.Error(),
			})
		}

		// Convert to our response format using pooled Response
		resp := GetResponse()
		resp.Server = server
		resp.Offset = result.response.ClockOffset
		resp.RTT = result.response.RTT
		resp.Stratum = result.response.Stratum
		resp.ReferenceTime = result.response.ReferenceTime
		resp.RootDelay = result.response.RootDelay
		resp.RootDispersion = result.response.RootDispersion
		resp.RootDistance = result.response.RootDistance
		resp.Precision = result.response.Precision
		resp.LeapIndicator = uint8(result.response.Leap)
		resp.Poll = result.response.Poll
		resp.ValidateError = result.response.Validate()
		resp.ReferenceID = result.response.ReferenceID
		resp.Time = result.response.Time
		resp.MinError = result.response.MinError
		resp.KissCode = result.response.KissCode

		logger.SafeDebug("ntp", "NTP query successful", map[string]interface{}{
			"server":  server,
			"offset":  resp.Offset.Seconds(),
			"rtt":     resp.RTT.Seconds(),
			"stratum": resp.Stratum,
		})

		return resp, nil
	}
}

// QueryMultiple performs multiple NTP queries and returns all responses
// Uses object pooling to reduce allocations
func (c *Client) QueryMultiple(ctx context.Context, server string, count int) ([]*Response, error) {
	// Use pooled slice to reduce allocations
	responseSlicePtr := GetResponseSlice()
	defer PutResponseSlice(responseSlicePtr)
	responses := *responseSlicePtr

	for i := 0; i < count; i++ {
		select {
		case <-ctx.Done():
			// Copy results before returning the pooled slice
			result := make([]*Response, len(responses))
			copy(result, responses)
			return result, ctx.Err()
		default:
		}

		resp, err := c.Query(ctx, server)
		if err != nil {
			logger.SafeDebug("ntp", "NTP query attempt failed", map[string]interface{}{
				"server":  server,
				"attempt": i + 1,
				"error":   err.Error(),
			})
			continue
		}

		responses = append(responses, resp)

		// Small delay between queries to avoid rate limiting
		if i < count-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	if len(responses) == 0 {
		logger.Warnf("ntp", "All %d NTP queries failed for server %s", count, server)
		return nil, fmt.Errorf("all %d NTP queries failed for server %s", count, server)
	}

	logger.SafeDebug("ntp", "Multiple NTP queries completed", map[string]interface{}{
		"server":     server,
		"successful": len(responses),
		"total":      count,
	})

	// Copy results before returning the pooled slice
	result := make([]*Response, len(responses))
	copy(result, responses)
	return result, nil
}

// IsKissOfDeath checks if the response contains a Kiss-of-Death code
func (r *Response) IsKissOfDeath() bool {
	return r.KissCode != ""
}

// IsValid checks if the response passed validation
func (r *Response) IsValid() bool {
	return r.ValidateError == nil
}

// IsSuspicious checks if the response has suspicious characteristics
func (r *Response) IsSuspicious() bool {
	// Check for invalid stratum
	if r.Stratum == 0 || r.Stratum > 15 {
		return true
	}

	// Check for Kiss-of-Death
	if r.IsKissOfDeath() {
		return true
	}

	// Check for validation errors
	if !r.IsValid() {
		return true
	}

	// Check for unreasonable offset
	if mathutil.AbsDuration(r.Offset) > SuspiciousOffsetThreshold {
		return true
	}

	// Check for unreasonable RTT
	if r.RTT > MaxAcceptableRTT {
		return true
	}

	return false
}
