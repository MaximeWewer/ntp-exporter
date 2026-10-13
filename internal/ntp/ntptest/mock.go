// Package ntptest provides a mock NTP client for tests.
package ntptest

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/ntp"
)

// MockNTPClient is a mock NTP client for testing
type MockNTPClient struct {
	mu         sync.RWMutex
	responses  map[string]*ntp.Response
	errors     map[string]error
	delays     map[string]time.Duration
	callCounts map[string]int
}

// NewMockNTPClient creates a new mock NTP client
func NewMockNTPClient() *MockNTPClient {
	return &MockNTPClient{
		responses:  make(map[string]*ntp.Response),
		errors:     make(map[string]error),
		delays:     make(map[string]time.Duration),
		callCounts: make(map[string]int),
	}
}

// Query performs a mock NTP query
func (m *MockNTPClient) Query(ctx context.Context, server string) (*ntp.Response, error) {
	m.mu.Lock()
	m.callCounts[server]++

	// Check for configured delay
	if delay, ok := m.delays[server]; ok {
		m.mu.Unlock()
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		m.mu.Lock()
	}

	// Check for configured error
	if err, ok := m.errors[server]; ok {
		m.mu.Unlock()
		return nil, err
	}

	// Return configured response
	if resp, ok := m.responses[server]; ok {
		m.mu.Unlock()
		return resp, nil
	}

	m.mu.Unlock()
	return nil, errors.New("server not configured in mock")
}

// QueryMultiple performs multiple mock NTP queries. Like the real client,
// it stops at the first Kiss-o'-Death.
func (m *MockNTPClient) QueryMultiple(ctx context.Context, server string, count int) ([]*ntp.Response, int, error) {
	responses := make([]*ntp.Response, 0, count)
	sent := 0

	for i := 0; i < count; i++ {
		select {
		case <-ctx.Done():
			return responses, sent, ctx.Err()
		default:
		}

		sent++
		resp, err := m.Query(ctx, server)
		if err != nil {
			continue
		}

		responses = append(responses, resp)
		if resp.IsKissOfDeath() {
			break
		}
	}

	if len(responses) == 0 {
		return nil, sent, errors.New("all queries failed")
	}

	return responses, sent, nil
}

// SetupSuccessfulServer configures a successful server response
func (m *MockNTPClient) SetupSuccessfulServer(server string, offset time.Duration, stratum uint8) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.responses[server] = &ntp.Response{
		Server:         server,
		Offset:         offset,
		RTT:            50 * time.Millisecond,
		Stratum:        stratum,
		ReferenceTime:  now.Add(-1 * time.Hour),
		RootDelay:      10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
		RootDistance:   15 * time.Millisecond,
		Precision:      time.Microsecond,
		LeapIndicator:  0, // LeapNoWarning
		Poll:           6 * time.Second,
		ValidateError:  nil,
		ReferenceID:    0x4E495354,
		Time:           now,
		MinError:       time.Millisecond,
		KissCode:       "",
	}
}

// SetupUnreachableServer configures an unreachable server
func (m *MockNTPClient) SetupUnreachableServer(server string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.errors[server] = errors.New("i/o timeout")
}

// SetupKoDServer configures a Kiss-of-Death server response
func (m *MockNTPClient) SetupKoDServer(server string, code string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.responses[server] = &ntp.Response{
		Server:         server,
		Offset:         0,
		RTT:            50 * time.Millisecond,
		Stratum:        0,
		ReferenceTime:  now,
		RootDelay:      0,
		RootDispersion: 0,
		RootDistance:   0,
		Precision:      time.Microsecond,
		LeapIndicator:  3, // LeapNotInSync
		Poll:           0,
		ValidateError:  errors.New("kiss of death received: " + code),
		ReferenceID:    0,
		Time:           now,
		MinError:       0,
		KissCode:       code,
	}
}

// SetupInvalidStratumServer configures a server with invalid stratum
func (m *MockNTPClient) SetupInvalidStratumServer(server string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.responses[server] = &ntp.Response{
		Server:         server,
		Offset:         0,
		RTT:            50 * time.Millisecond,
		Stratum:        16, // Invalid stratum
		ReferenceTime:  now,
		RootDelay:      0,
		RootDispersion: 0,
		RootDistance:   0,
		Precision:      time.Microsecond,
		LeapIndicator:  3, // LeapNotInSync
		Poll:           0,
		ValidateError:  errors.New("invalid stratum"),
		ReferenceID:    0,
		Time:           now,
		MinError:       0,
		KissCode:       "",
	}
}

// SetResponse sets a custom response for a server
func (m *MockNTPClient) SetResponse(server string, resp *ntp.Response) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.responses[server] = resp
}

// SetDelay sets a delay before responding
func (m *MockNTPClient) SetDelay(server string, delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.delays[server] = delay
}

// GetCallCount returns the number of times a server was queried
func (m *MockNTPClient) GetCallCount(server string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.callCounts[server]
}
