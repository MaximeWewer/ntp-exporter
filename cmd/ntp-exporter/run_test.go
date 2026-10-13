package main

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_Version(t *testing.T) {
	var out bytes.Buffer
	assert.Equal(t, 0, run([]string{"--version"}, nil, &out, io.Discard))
	assert.Contains(t, out.String(), "ntp-exporter version dev")
}

func TestRun_InvalidFlag(t *testing.T) {
	assert.Equal(t, 2, run([]string{"--unknown"}, nil, io.Discard, io.Discard))
	assert.Equal(t, 0, run([]string{"--help"}, nil, io.Discard, io.Discard))
}

func TestRun_InvalidConfiguration(t *testing.T) {
	var stderr bytes.Buffer
	assert.Equal(t, 1, run([]string{"--config", t.TempDir() + "/missing.yaml"}, nil, io.Discard, &stderr))
	assert.Contains(t, stderr.String(), "Failed to load configuration")

	stderr.Reset()
	t.Setenv("NTP_EXPORTER_SERVERS", "time.example.org")
	assert.Equal(t, 1, run([]string{"--web.listen-address", "nope"}, nil, io.Discard, &stderr))
	assert.Contains(t, stderr.String(), "--web.listen-address")
}

func TestRun_HealthcheckWithInvalidConfiguration(t *testing.T) {
	var stderr bytes.Buffer
	assert.Equal(t, 1, run([]string{"--healthcheck", "--web.listen-address", "nope"}, nil, io.Discard, &stderr))
	assert.Contains(t, stderr.String(), "healthcheck: invalid configuration")
}

// freePort returns a loopback port nobody listens on
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

func TestRun_ServesUntilSignal(t *testing.T) {
	// /probe only: no target is queried, so the test needs no network
	t.Setenv("NTP_EXPORTER_PROBE_ENDPOINT_ENABLED", "true")
	t.Setenv("NTP_EXPORTER_LOG_LEVEL", "error")
	addr := "127.0.0.1:" + strconv.Itoa(freePort(t))

	signals := make(chan os.Signal, 1)
	done := make(chan int, 1)
	go func() { done <- run([]string{"--web.listen-address", addr}, signals, io.Discard, io.Discard) }()

	require.Eventually(t, func() bool {
		return run([]string{"--healthcheck", "--web.listen-address", addr}, nil, io.Discard, io.Discard) == 0
	}, 5*time.Second, 20*time.Millisecond)

	resp, err := http.Get("http://" + addr + "/metrics")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.True(t, strings.Contains(string(body), "ntp_exporter_build_info"))

	signals <- syscall.SIGTERM
	select {
	case code := <-done:
		assert.Equal(t, 0, code)
	case <-time.After(5 * time.Second):
		t.Fatal("the exporter did not stop on SIGTERM")
	}
}
