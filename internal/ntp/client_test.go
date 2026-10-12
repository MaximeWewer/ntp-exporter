package ntp

import (
	"context"
	"testing"
	"time"

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

		responses, err := client.QueryMultiple(context.Background(), server.addr(), 3)

		require.NoError(t, err)
		assert.Len(t, responses, 3)
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
		responses, err := client.QueryMultiple(ctx, server.addr(), 3)

		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Len(t, responses, 1, "samples received before cancellation are kept")
		assert.Less(t, time.Since(start), time.Second)
	})

	t.Run("context_cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		responses, err := client.QueryMultiple(ctx, "192.0.2.1", 5)

		assert.Error(t, err)
		assert.LessOrEqual(t, len(responses), 5)
	})

	t.Run("unreachable_server", func(t *testing.T) {
		client := NewClient(200*time.Millisecond, 4)
		responses, err := client.QueryMultiple(context.Background(), "192.0.2.1", 2)

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
