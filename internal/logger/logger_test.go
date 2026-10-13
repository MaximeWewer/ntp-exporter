package logger

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capture redirects the logger to a buffer for the test
func capture(t *testing.T, format, lvl string) *bytes.Buffer {
	t.Helper()
	previous, previousLevel := log, level.Level()
	t.Cleanup(func() { log = previous; level.Set(previousLevel) })
	var buf bytes.Buffer
	level.Set(parseLevel(lvl))
	log = newLogger(&buf, format, "test")
	return &buf
}

func TestParseLevel(t *testing.T) {
	for name, want := range map[string]slog.Level{
		"debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn,
		"warning": slog.LevelWarn, "error": slog.LevelError, "": slog.LevelInfo, "DEBUG": slog.LevelDebug,
	} {
		assert.Equal(t, want, parseLevel(name), name)
	}
}

func TestLogger_JSON(t *testing.T) {
	buf := capture(t, "json", "info")

	Info("probe", "cycle done", "targets", 3)
	Debug("probe", "hidden")
	Error("main", "failed", errors.New("boom"), "server", "a.example.org")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2, "debug is below the level")
	var entry map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &entry))
	assert.Equal(t, "cycle done", entry["msg"])
	assert.Equal(t, "probe", entry["package"])
	assert.Equal(t, "test", entry["component"])
	assert.EqualValues(t, 3, entry["targets"])
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &entry))
	assert.Equal(t, "boom", entry["error"])
	assert.Equal(t, "a.example.org", entry["server"])
}

func TestLogger_ExternalValuesCannotForgeLines(t *testing.T) {
	buf := capture(t, "console", "debug")

	Warn("probe", "refused", "target", "evil\nlevel=ERROR msg=forged")

	assert.Equal(t, 1, strings.Count(buf.String(), "\n"), "the value is escaped on one line")
	assert.Contains(t, buf.String(), `target="evil\nlevel=ERROR msg=forged"`)
}

func TestLogger_HTTPRequestsAtDebug(t *testing.T) {
	buf := capture(t, "json", "info")
	HTTP("GET", "/metrics", 200, 0, "127.0.0.1:1234")
	assert.Empty(t, buf.String())
}

func BenchmarkDisabledDebug(b *testing.B) {
	previous := level.Level()
	level.Set(slog.LevelInfo)
	defer level.Set(previous)
	b.ReportAllocs()
	for b.Loop() {
		Debug("probe", "query", "server", "a.example.org", "attempt", 2)
	}
}
