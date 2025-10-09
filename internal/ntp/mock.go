package ntp

import (
	"context"
	"errors"
	"sync"
	"time"
)

// MockNTPClient is a mock NTP client for testing
type MockNTPClient struct {
	mu          sync.RWMutex
	responses   map[string]*Response
	errors      map[string]error
	delays      map[string]time.Duration
	callCounts  map[string]int
	flapping    map[string]bool
	flapCounter map[string]int
}

// NewMockNTPClient creates a new mock NTP client
func NewMockNTPClient() *MockNTPClient {
	return &MockNTPClient{
		responses:   make(map[string]*Response),
		errors:      make(map[string]error),
		delays:      make(map[string]time.Duration),
		callCounts:  make(map[string]int),
		flapping:    make(map[string]bool),
		flapCounter: make(map[string]int),
	}
}

// Query performs a mock NTP query
func (m *MockNTPClient) Query(ctx context.Context, server string) (*Response, error) {
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

	// Check for flapping server
	if m.flapping[server] {
		m.flapCounter[server]++
		if m.flapCounter[server]%2 == 0 {
			m.mu.Unlock()
			return nil, errors.New("connection refused")
		}
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

// QueryMultiple performs multiple mock NTP queries
func (m *MockNTPClient) QueryMultiple(ctx context.Context, server string, count int) ([]*Response, error) {
	responses := make([]*Response, 0, count)

	for i := 0; i < count; i++ {
		select {
		case <-ctx.Done():
			return responses, ctx.Err()
		default:
		}

		resp, err := m.Query(ctx, server)
		if err != nil {
			continue
		}

		responses = append(responses, resp)
	}

	if len(responses) == 0 {
		return nil, errors.New("all queries failed")
	}

	return responses, nil
}

// SetupSuccessfulServer configures a successful server response
func (m *MockNTPClient) SetupSuccessfulServer(server string, offset time.Duration, stratum uint8) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.responses[server] = &Response{
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
	m.responses[server] = &Response{
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
		ValidateError:  nil,
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
	m.responses[server] = &Response{
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

// SetupHighLatencyServer configures a server with high latency
func (m *MockNTPClient) SetupHighLatencyServer(server string, latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.delays[server] = latency

	now := time.Now()
	m.responses[server] = &Response{
		Server:         server,
		Offset:         0,
		RTT:            latency,
		Stratum:        2,
		ReferenceTime:  now.Add(-1 * time.Hour),
		RootDelay:      10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
		RootDistance:   15 * time.Millisecond,
		Precision:      time.Microsecond,
		LeapIndicator:  0,
		Poll:           6 * time.Second,
		ValidateError:  nil,
		ReferenceID:    0x4E495354,
		Time:           now,
		MinError:       time.Millisecond,
		KissCode:       "",
	}
}

// SetupHighDriftServer configures a server with high clock drift
func (m *MockNTPClient) SetupHighDriftServer(server string, drift time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.responses[server] = &Response{
		Server:         server,
		Offset:         drift,
		RTT:            50 * time.Millisecond,
		Stratum:        2,
		ReferenceTime:  now.Add(-1 * time.Hour),
		RootDelay:      10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
		RootDistance:   15 * time.Millisecond,
		Precision:      time.Microsecond,
		LeapIndicator:  0,
		Poll:           6 * time.Second,
		ValidateError:  nil,
		ReferenceID:    0x4E495354,
		Time:           now,
		MinError:       time.Millisecond,
		KissCode:       "",
	}
}

// SetupFlappingServer configures a server that alternates between success and failure
func (m *MockNTPClient) SetupFlappingServer(server string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.flapping[server] = true
	m.flapCounter[server] = 0

	now := time.Now()
	m.responses[server] = &Response{
		Server:         server,
		Offset:         0,
		RTT:            50 * time.Millisecond,
		Stratum:        2,
		ReferenceTime:  now.Add(-1 * time.Hour),
		RootDelay:      10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
		RootDistance:   15 * time.Millisecond,
		Precision:      time.Microsecond,
		LeapIndicator:  0,
		Poll:           6 * time.Second,
		ValidateError:  nil,
		ReferenceID:    0x4E495354,
		Time:           now,
		MinError:       time.Millisecond,
		KissCode:       "",
	}
}

// SetError sets a custom error for a server
func (m *MockNTPClient) SetError(server string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.errors[server] = err
}

// SetResponse sets a custom response for a server
func (m *MockNTPClient) SetResponse(server string, resp *Response) {
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

// Reset clears all mock configurations
func (m *MockNTPClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.responses = make(map[string]*Response)
	m.errors = make(map[string]error)
	m.delays = make(map[string]time.Duration)
	m.callCounts = make(map[string]int)
	m.flapping = make(map[string]bool)
	m.flapCounter = make(map[string]int)
}
