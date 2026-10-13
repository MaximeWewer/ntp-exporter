package ntp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/beevik/ntp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_QueryLocalServer(t *testing.T) {
	server := startFakeServer(t, "")
	client := NewClient(2*time.Second, 4)

	resp, err := client.Query(context.Background(), server.addr())

	require.NoError(t, err)
	assert.True(t, resp.IsValid(), "reason: %s, err: %v", resp.InvalidReason(), resp.ValidateError)
	assert.Equal(t, uint8(2), resp.Stratum)
	assert.Less(t, resp.Offset.Abs(), 100*time.Millisecond)
}

func TestClient_QueryLocalServer_KissOfDeath(t *testing.T) {
	server := startFakeServer(t, "RATE")
	client := NewClient(2*time.Second, 4)

	resp, err := client.Query(context.Background(), server.addr())

	require.NoError(t, err, "a KoD is a response, not a transport error")
	assert.Equal(t, "RATE", resp.KissCode)
	assert.False(t, resp.IsValid())
	assert.Equal(t, "kod", resp.InvalidReason())
}

func TestClient_QueryMultiple(t *testing.T) {
	client := NewClient(2*time.Second, 4)

	t.Run("samples_are_spaced", func(t *testing.T) {
		server := startFakeServer(t, "")
		client := NewClient(2*time.Second, 4)
		client.SetSampleSpacing(150 * time.Millisecond)

		responses, sent, err := client.QueryMultiple(context.Background(), server.addr(), 3)

		require.NoError(t, err)
		assert.Len(t, responses, 3)
		assert.Equal(t, 3, sent)
		received := server.requests()
		require.Len(t, received, 3)
		for i := 1; i < len(received); i++ {
			assert.GreaterOrEqual(t, received[i].Sub(received[i-1]), 140*time.Millisecond)
		}
	})

	t.Run("spacing_honors_context", func(t *testing.T) {
		server := startFakeServer(t, "")
		client := NewClient(2*time.Second, 4)
		client.SetSampleSpacing(time.Hour)

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		start := time.Now()
		responses, sent, err := client.QueryMultiple(ctx, server.addr(), 3)

		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Len(t, responses, 1, "samples received before cancellation are kept")
		assert.Equal(t, 1, sent, "queries never sent are not counted")
		assert.Less(t, time.Since(start), time.Second)
	})

	t.Run("context_cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		responses, sent, err := client.QueryMultiple(ctx, "192.0.2.1", 5)

		assert.Error(t, err)
		assert.Empty(t, responses)
		assert.Equal(t, 0, sent)
	})

	t.Run("unreachable_server", func(t *testing.T) {
		client := NewClient(100*time.Millisecond, 4)
		client.SetSampleSpacing(time.Millisecond)
		silent := silentServer(t)
		responses, sent, err := client.QueryMultiple(context.Background(), silent, 2)

		// Should fail with all queries failing, keeping the last error
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "all 2 NTP queries failed")
		assert.Contains(t, err.Error(), silent)
		assert.Equal(t, 0, len(responses))
		assert.Equal(t, 2, sent)
	})

	// RFC 5905 §7.4: a Kiss-o'-Death asks the client to slow down or stop,
	// the rest of the burst must not be sent
	t.Run("kiss_of_death_stops_burst", func(t *testing.T) {
		server := startFakeServer(t, "RATE")
		client := NewClient(2*time.Second, 4)
		client.SetSampleSpacing(10 * time.Millisecond)

		responses, sent, err := client.QueryMultiple(context.Background(), server.addr(), 4)

		require.NoError(t, err)
		require.Len(t, responses, 1)
		assert.Equal(t, "RATE", responses[0].KissCode)
		assert.Equal(t, 1, sent)
		assert.Len(t, server.requests(), 1, "no query after the Kiss-o'-Death")
	})

	t.Run("kiss_of_death_mid_burst", func(t *testing.T) {
		server := startFakeServer(t, "DENY")
		server.setKissFrom(2)
		client := NewClient(2*time.Second, 4)
		client.SetSampleSpacing(10 * time.Millisecond)

		responses, sent, err := client.QueryMultiple(context.Background(), server.addr(), 4)

		require.NoError(t, err)
		require.Len(t, responses, 2)
		assert.True(t, responses[0].IsValid())
		assert.Equal(t, "DENY", responses[1].KissCode)
		assert.Equal(t, 2, sent)
		assert.Len(t, server.requests(), 2, "no third query after DENY")
	})
}

func TestClient_QueryWithTimeout(t *testing.T) {
	client := NewClient(50*time.Millisecond, 4)

	ctx := context.Background()
	_, err := client.Query(ctx, silentServer(t))

	assert.Error(t, err, "Should timeout on unreachable server")
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

func TestClient_RejectedPacketIsAnInvalidResponse(t *testing.T) {
	server := startFakeServer(t, "")
	server.setZeroTransmit()
	client := NewClient(2*time.Second, 4)

	resp, err := client.Query(context.Background(), server.addr())

	require.NoError(t, err, "the server answered: not a lost query")
	assert.False(t, resp.IsValid())
	assert.ErrorIs(t, resp.ValidateError, ntp.ErrInvalidTransmitTime)
	assert.Equal(t, ReasonInvalidTime, resp.InvalidReason())
}

func TestInvalidReason_ProtocolErrors(t *testing.T) {
	for err, want := range map[error]string{
		ntp.ErrInvalidTransmitTime:    ReasonInvalidTime,
		ntp.ErrServerTickedBackwards:  ReasonInvalidTime,
		ntp.ErrServerResponseMismatch: ReasonInvalidTime,
		ntp.ErrInvalidMode:            ReasonOther,
	} {
		resp, ok := protocolErrorResponse("s", fmt.Errorf("query: %w", err))
		require.True(t, ok, err)
		assert.Equal(t, want, resp.InvalidReason(), err)
	}

	_, ok := protocolErrorResponse("s", errors.New("i/o timeout"))
	assert.False(t, ok, "a timeout stays a lost query")
}

// silentServer returns the address of a local UDP socket that never answers
func silentServer(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn.LocalAddr().String()
}
