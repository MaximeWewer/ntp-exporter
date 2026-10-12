package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/config"
	"github.com/maximewewer/ntp-exporter/internal/ntp"
	"github.com/maximewewer/ntp-exporter/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMockedBaseCollector(t *testing.T, mock *ntp.MockNTPClient, servers ...string) (*BaseCollector, *metrics.NTPMetrics) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.NTP.Servers = servers
	m := metrics.NewNTPMetrics()
	c := NewBaseCollector(cfg, m)
	c.client = mock
	return c, m
}

func TestBaseCollector_InvalidResponsesAreNotPublished(t *testing.T) {
	tests := []struct {
		name  string
		setup func(m *ntp.MockNTPClient, server string)
	}{
		{"kiss of death", func(m *ntp.MockNTPClient, s string) { m.SetupKoDServer(s, "RATE") }},
		{"stratum 16", func(m *ntp.MockNTPClient, s string) { m.SetupInvalidStratumServer(s) }},
		{"leap indicator 3", func(m *ntp.MockNTPClient, s string) {
			m.SetupSuccessfulServer(s, 5*time.Millisecond, 2)
			resp, _ := m.Query(context.Background(), s)
			bad := *resp
			bad.LeapIndicator = 3
			m.SetResponse(s, &bad)
		}},
		{"validation error", func(m *ntp.MockNTPClient, s string) {
			m.SetupSuccessfulServer(s, 5*time.Millisecond, 2)
			resp, _ := m.Query(context.Background(), s)
			bad := *resp
			bad.ValidateError = errors.New("invalid dispersion")
			m.SetResponse(s, &bad)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const server = "bad.example.org"
			mock := ntp.NewMockNTPClient()
			tt.setup(mock, server)
			c, m := newMockedBaseCollector(t, mock, server)

			require.NoError(t, c.Collect(context.Background()))

			assert.Equal(t, 1.0, testutil.ToFloat64(m.ServerReachable.WithLabelValues(server)), "server answered")
			assert.Equal(t, 0, testutil.CollectAndCount(m.OffsetSeconds), "offset must not be published")
			assert.Equal(t, 0, testutil.CollectAndCount(m.Stratum), "stratum must not be published")
			assert.Equal(t, 0, testutil.CollectAndCount(m.RTTSeconds), "rtt must not be published")
		})
	}
}

func TestBaseCollector_ValidResponseIsPublished(t *testing.T) {
	const server = "good.example.org"
	mock := ntp.NewMockNTPClient()
	mock.SetupSuccessfulServer(server, 5*time.Millisecond, 2)
	c, m := newMockedBaseCollector(t, mock, server)

	require.NoError(t, c.Collect(context.Background()))

	assert.Equal(t, 1, testutil.CollectAndCount(m.OffsetSeconds))
	assert.Equal(t, 2.0, testutil.ToFloat64(m.Stratum.WithLabelValues(server)))
}

func TestResponseIsValid(t *testing.T) {
	valid := &ntp.Response{Stratum: 2}
	assert.True(t, valid.IsValid())
	assert.False(t, (&ntp.Response{Stratum: 0, KissCode: "DENY"}).IsValid())
	assert.False(t, (&ntp.Response{Stratum: 16}).IsValid())
	assert.False(t, (&ntp.Response{Stratum: 2, LeapIndicator: 3}).IsValid())
	assert.False(t, (&ntp.Response{Stratum: 2, ValidateError: errors.New("x")}).IsValid())
}
