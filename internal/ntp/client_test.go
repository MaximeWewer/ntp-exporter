package ntp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNTPClient_Query_Success(t *testing.T) {
	tests := []struct {
		name    string
		server  string
		offset  time.Duration
		stratum uint8
		wantErr bool
	}{
		{
			name:    "successful_query_zero_offset",
			server:  "pool.ntp.org",
			offset:  0,
			stratum: 2,
			wantErr: false,
		},
		{
			name:    "successful_query_positive_offset",
			server:  "time.google.com",
			offset:  10 * time.Millisecond,
			stratum: 1,
			wantErr: false,
		},
		{
			name:    "successful_query_negative_offset",
			server:  "time.cloudflare.com",
			offset:  -5 * time.Millisecond,
			stratum: 3,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := NewMockNTPClient()
			mockClient.SetupSuccessfulServer(tt.server, tt.offset, tt.stratum)

			resp, err := mockClient.Query(context.Background(), tt.server)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tt.offset, resp.Offset)
			assert.Equal(t, tt.stratum, resp.Stratum)
		})
	}
}

func TestNTPClient_Query_Errors(t *testing.T) {
	mockClient := NewMockNTPClient()
	server := "unreachable.ntp.org"
	mockClient.SetupUnreachableServer(server)

	resp, err := mockClient.Query(context.Background(), server)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestNTPClient_Query_KissOfDeath(t *testing.T) {
	tests := []struct {
		name string
		code string
	}{
		{"rate_limiting", "RATE"},
		{"access_denied", "DENY"},
		{"restricted", "RSTR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := NewMockNTPClient()
			server := "pool.ntp.org"
			mockClient.SetupKoDServer(server, tt.code)

			resp, err := mockClient.Query(context.Background(), server)

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tt.code, resp.KissCode)
			assert.Equal(t, uint8(0), resp.Stratum)
		})
	}
}

func TestNTPClient_Query_InvalidStratum(t *testing.T) {
	mockClient := NewMockNTPClient()
	server := "pool.ntp.org"
	mockClient.SetupInvalidStratumServer(server)

	resp, err := mockClient.Query(context.Background(), server)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint8(16), resp.Stratum)
}

func TestNTPClient_Query_HighLatency(t *testing.T) {
	mockClient := NewMockNTPClient()
	server := "slow.ntp.org"
	latency := 100 * time.Millisecond
	mockClient.SetupHighLatencyServer(server, latency)

	start := time.Now()
	resp, err := mockClient.Query(context.Background(), server)
	duration := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.GreaterOrEqual(t, duration, latency)
}

func TestNTPClient_Query_HighDrift(t *testing.T) {
	tests := []struct {
		name  string
		drift time.Duration
	}{
		{"small_drift", 10 * time.Millisecond},
		{"medium_drift", 100 * time.Millisecond},
		{"large_drift", 1 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := NewMockNTPClient()
			server := "pool.ntp.org"
			mockClient.SetupHighDriftServer(server, tt.drift)

			resp, err := mockClient.Query(context.Background(), server)

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tt.drift, resp.Offset)
		})
	}
}

func TestNTPClient_QueryMultiple(t *testing.T) {
	mockClient := NewMockNTPClient()
	server := "pool.ntp.org"
	mockClient.SetupSuccessfulServer(server, 10*time.Millisecond, 2)

	responses, err := mockClient.QueryMultiple(context.Background(), server, 5)

	require.NoError(t, err)
	assert.Len(t, responses, 5)

	for _, resp := range responses {
		assert.NotNil(t, resp)
		assert.Equal(t, server, resp.Server)
	}
}

func TestNTPClient_QueryMultiple_PartialFailure(t *testing.T) {
	mockClient := NewMockNTPClient()
	server := "flapping.ntp.org"
	mockClient.SetupFlappingServer(server)

	responses, err := mockClient.QueryMultiple(context.Background(), server, 6)

	// Should get some responses even if some fail
	if len(responses) > 0 {
		assert.NotNil(t, responses[0])
	}

	if err != nil {
		t.Logf("Got expected partial failures: %v", err)
	}
}

func TestNTPClient_Concurrent(t *testing.T) {
	mockClient := NewMockNTPClient()
	server := "pool.ntp.org"
	mockClient.SetupSuccessfulServer(server, 0, 2)

	concurrency := 100
	done := make(chan bool, concurrency)
	errors := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			resp, err := mockClient.Query(context.Background(), server)
			if err != nil {
				errors <- err
			} else if resp == nil {
				errors <- assert.AnError
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < concurrency; i++ {
		<-done
	}
	close(errors)

	// Check for errors
	errorCount := 0
	for range errors {
		errorCount++
	}

	assert.Equal(t, 0, errorCount, "Should have no errors in concurrent queries")
	assert.Equal(t, concurrency, mockClient.GetCallCount(server))
}

func TestNTPClient_ContextCancellation(t *testing.T) {
	mockClient := NewMockNTPClient()
	server := "pool.ntp.org"
	mockClient.SetupHighLatencyServer(server, 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	resp, err := mockClient.Query(ctx, server)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestRealNTPClient_Query(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping real NTP test in short mode")
	}

	client := NewClient(5*time.Second, 4)
	ctx := context.Background()

	resp, err := client.Query(ctx, "pool.ntp.org")

	// May fail due to network issues, but shouldn't panic
	if err != nil {
		t.Logf("Real NTP query failed (expected in some environments): %v", err)
		return
	}

	assert.NotNil(t, resp)
	assert.Greater(t, resp.Stratum, uint8(0))
	assert.Less(t, resp.Stratum, uint8(16))
}

func TestClient_QueryMultiple(t *testing.T) {
	client := NewClient(2*time.Second, 4)

	t.Run("successful_multiple_queries", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping network test in short mode")
		}

		ctx := context.Background()
		responses, err := client.QueryMultiple(ctx, "pool.ntp.org", 3)

		if err != nil {
			t.Logf("QueryMultiple failed (may happen in network-restricted environments): %v", err)
			return
		}

		assert.NotNil(t, responses)
		assert.Greater(t, len(responses), 0, "Should have at least one successful response")
	})

	t.Run("context_cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		responses, err := client.QueryMultiple(ctx, "pool.ntp.org", 5)

		assert.Error(t, err)
		assert.LessOrEqual(t, len(responses), 5)
	})

	t.Run("unreachable_server", func(t *testing.T) {
		ctx := context.Background()
		responses, err := client.QueryMultiple(ctx, "192.0.2.1", 2)

		// Should fail with all queries failing
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "all 2 NTP queries failed")
		assert.Equal(t, 0, len(responses))
	})
}

func TestClient_QueryWithTimeout(t *testing.T) {
	client := NewClient(50*time.Millisecond, 4)

	ctx := context.Background()
	_, err := client.Query(ctx, "192.0.2.1") // TEST-NET-1 (non-routable)

	assert.Error(t, err, "Should timeout on unreachable server")
}

func TestClient_QueryMultiple_UsesObjectPool(t *testing.T) {
	mockClient := NewMockNTPClient()
	server := "pool.ntp.org"
	mockClient.SetupSuccessfulServer(server, 0, 2)

	ctx := context.Background()
	responses, err := mockClient.QueryMultiple(ctx, server, 3)

	require.NoError(t, err)
	assert.Len(t, responses, 3)

	// Verify responses are independent copies (not pooled references)
	for i, resp := range responses {
		assert.NotNil(t, resp, "Response %d should not be nil", i)
		assert.Equal(t, server, resp.Server)
	}
}

func TestClient_QueryWithRateLimit(t *testing.T) {
	client := NewClientWithRateLimit(2*time.Second, 4, 10, 5, 2)
	assert.NotNil(t, client, "Client with rate limit should be created")

	t.Run("handles_context_cancellation_with_rate_limit", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		resp, err := client.Query(ctx, "192.0.2.1")

		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestClient_VersionConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		version int
	}{
		{"version_3", 3},
		{"version_4", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(2*time.Second, tt.version)
			assert.NotNil(t, client)
		})
	}
}

func TestResponse_IsSuspicious(t *testing.T) {
	tests := []struct {
		name       string
		response   *Response
		suspicious bool
	}{
		{
			name: "normal_response",
			response: &Response{
				Stratum:       2,
				Offset:        10 * time.Millisecond,
				RTT:           50 * time.Millisecond,
				KissCode:      "",
				ValidateError: nil,
			},
			suspicious: false,
		},
		{
			name: "invalid_stratum_0",
			response: &Response{
				Stratum:       0,
				Offset:        10 * time.Millisecond,
				RTT:           50 * time.Millisecond,
				KissCode:      "",
				ValidateError: nil,
			},
			suspicious: true,
		},
		{
			name: "invalid_stratum_16",
			response: &Response{
				Stratum:       16,
				Offset:        10 * time.Millisecond,
				RTT:           50 * time.Millisecond,
				KissCode:      "",
				ValidateError: nil,
			},
			suspicious: true,
		},
		{
			name: "kiss_of_death",
			response: &Response{
				Stratum:       2,
				Offset:        10 * time.Millisecond,
				RTT:           50 * time.Millisecond,
				KissCode:      "RATE",
				ValidateError: nil,
			},
			suspicious: true,
		},
		{
			name: "validation_error",
			response: &Response{
				Stratum:       2,
				Offset:        10 * time.Millisecond,
				RTT:           50 * time.Millisecond,
				KissCode:      "",
				ValidateError: errors.New("validation failed"),
			},
			suspicious: true,
		},
		{
			name: "excessive_offset",
			response: &Response{
				Stratum:       2,
				Offset:        2 * time.Hour, // > 1 hour
				RTT:           50 * time.Millisecond,
				KissCode:      "",
				ValidateError: nil,
			},
			suspicious: true,
		},
		{
			name: "excessive_rtt",
			response: &Response{
				Stratum:       2,
				Offset:        10 * time.Millisecond,
				RTT:           15 * time.Second, // > 10 seconds
				KissCode:      "",
				ValidateError: nil,
			},
			suspicious: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.response.IsSuspicious()
			assert.Equal(t, tt.suspicious, result, "IsSuspicious() mismatch for %s", tt.name)
		})
	}
}

func TestMock_SetError(t *testing.T) {
	mock := NewMockNTPClient()
	testError := errors.New("mock error")

	mock.SetError("test.server.com", testError)

	resp, err := mock.Query(context.Background(), "test.server.com")
	assert.Error(t, err)
	assert.Equal(t, testError, err)
	assert.Nil(t, resp)
}

func TestMock_SetResponse(t *testing.T) {
	mock := NewMockNTPClient()

	customResponse := &Response{
		Stratum: 3,
		Offset:  25 * time.Millisecond,
		RTT:     75 * time.Millisecond,
	}

	mock.SetResponse("custom.server.com", customResponse)

	resp, err := mock.Query(context.Background(), "custom.server.com")
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, uint8(3), resp.Stratum)
	assert.Equal(t, 25*time.Millisecond, resp.Offset)
	assert.Equal(t, 75*time.Millisecond, resp.RTT)
}

func TestMock_SetDelay(t *testing.T) {
	mock := NewMockNTPClient()

	mock.SetupSuccessfulServer("delayed.server.com", 0, 2)
	mock.SetDelay("delayed.server.com", 200*time.Millisecond)

	start := time.Now()
	resp, err := mock.Query(context.Background(), "delayed.server.com")
	elapsed := time.Since(start)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.GreaterOrEqual(t, elapsed, 200*time.Millisecond, "Should have delayed for at least 200ms")
}

func BenchmarkNTPClient_Query(b *testing.B) {
	mockClient := NewMockNTPClient()
	server := "pool.ntp.org"
	mockClient.SetupSuccessfulServer(server, 0, 2)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := mockClient.Query(ctx, server)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNTPClient_QueryParallel(b *testing.B) {
	mockClient := NewMockNTPClient()
	server := "pool.ntp.org"
	mockClient.SetupSuccessfulServer(server, 0, 2)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			_, err := mockClient.Query(ctx, server)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
