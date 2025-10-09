package testutil

import (
	"math/rand"
	"net/http"
	"net/http/httptest"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/beevik/ntp"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// CreateMockNTPResponse creates a valid mock NTP response for testing
func CreateMockNTPResponse(offset time.Duration, stratum uint8) *ntp.Response {
	now := time.Now()
	return &ntp.Response{
		Time:           now.Add(offset),
		ClockOffset:    offset,
		RTT:            50 * time.Millisecond,
		Precision:      time.Microsecond,
		Stratum:        stratum,
		ReferenceID:    0x4E495354, // NIST
		ReferenceTime:  now.Add(-1 * time.Hour),
		RootDelay:      10 * time.Millisecond,
		RootDispersion: 5 * time.Millisecond,
		RootDistance:   15 * time.Millisecond,
		Leap:           ntp.LeapNoWarning,
		MinError:       time.Millisecond,
		KissCode:       "",
		Poll:           6,
	}
}

// CreateInvalidStratumResponse creates an NTP response with invalid stratum
func CreateInvalidStratumResponse() *ntp.Response {
	resp := CreateMockNTPResponse(0, 16) // Stratum 16 is unsynchronized
	resp.Leap = ntp.LeapNotInSync
	return resp
}

// CreateKoDResponse creates a Kiss-of-Death NTP response
func CreateKoDResponse(code string) *ntp.Response {
	resp := CreateMockNTPResponse(0, 0)
	resp.Stratum = 0
	resp.KissCode = code
	return resp
}

// CreateHighDriftResponse creates a response with high time drift
func CreateHighDriftResponse(drift time.Duration) *ntp.Response {
	return CreateMockNTPResponse(drift, 2)
}

// AssertMetricValue validates a Prometheus metric value
func AssertMetricValue(t *testing.T, registry *prometheus.Registry, metricName string, labels map[string]string, expected float64) {
	t.Helper()

	metrics, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	for _, mf := range metrics {
		if mf.GetName() != metricName {
			continue
		}

		for _, m := range mf.GetMetric() {
			if labelsMatch(m.GetLabel(), labels) {
				var value float64
				switch mf.GetType() {
				case dto.MetricType_GAUGE:
					value = m.GetGauge().GetValue()
				case dto.MetricType_COUNTER:
					value = m.GetCounter().GetValue()
				case dto.MetricType_HISTOGRAM:
					value = m.GetHistogram().GetSampleSum()
				default:
					t.Fatalf("Unsupported metric type: %v", mf.GetType())
				}

				if value != expected {
					t.Errorf("Metric %s with labels %v: expected %f, got %f", metricName, labels, expected, value)
				}
				return
			}
		}
	}

	t.Errorf("Metric %s with labels %v not found", metricName, labels)
}

// AssertMetricExists checks if a metric exists with given labels
func AssertMetricExists(t *testing.T, registry *prometheus.Registry, metricName string, labels map[string]string) {
	t.Helper()

	metrics, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	for _, mf := range metrics {
		if mf.GetName() != metricName {
			continue
		}

		for _, m := range mf.GetMetric() {
			if labelsMatch(m.GetLabel(), labels) {
				return
			}
		}
	}

	t.Errorf("Metric %s with labels %v not found", metricName, labels)
}

// labelsMatch checks if metric labels match expected labels
func labelsMatch(metricLabels []*dto.LabelPair, expected map[string]string) bool {
	if len(metricLabels) != len(expected) {
		return false
	}

	for _, label := range metricLabels {
		expectedValue, exists := expected[label.GetName()]
		if !exists || expectedValue != label.GetValue() {
			return false
		}
	}

	return true
}

// WaitForCondition waits for a condition to be true with timeout
func WaitForCondition(t *testing.T, condition func() bool, timeout time.Duration, message string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		if condition() {
			return
		}

		select {
		case <-ticker.C:
			if time.Now().After(deadline) {
				t.Fatalf("Timeout waiting for condition: %s", message)
			}
		}
	}
}

// GenerateNTPResponse generates a deterministic NTP response based on seed
func GenerateNTPResponse(seed int64) *ntp.Response {
	r := rand.New(rand.NewSource(seed))

	offset := time.Duration(r.Int63n(1000000000)) // 0-1s
	stratum := uint8(r.Intn(14) + 1)              // 1-15
	rtt := time.Duration(r.Int63n(100000000))     // 0-100ms

	resp := CreateMockNTPResponse(offset, stratum)
	resp.RTT = rtt
	return resp
}

// NewTestHTTPServer creates a test HTTP server for integration tests
func NewTestHTTPServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(func() {
		server.Close()
	})

	return server
}

// MeasureMemoryAllocation measures memory allocated during function execution
func MeasureMemoryAllocation(fn func()) uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	before := m.Alloc

	fn()

	runtime.GC()
	runtime.ReadMemStats(&m)
	after := m.Alloc

	if after > before {
		return after - before
	}
	return 0
}

// CountGoroutines returns the current number of goroutines
func CountGoroutines() int {
	return runtime.NumGoroutine()
}

// CreateTestRegistry creates a new Prometheus registry for testing
func CreateTestRegistry() *prometheus.Registry {
	return prometheus.NewRegistry()
}

// ValidatePrometheusMetricName validates that a metric name follows Prometheus conventions
func ValidatePrometheusMetricName(t *testing.T, name string) {
	t.Helper()

	if len(name) == 0 {
		t.Error("Metric name cannot be empty")
	}

	// Must match regex: [a-zA-Z_:][a-zA-Z0-9_:]*
	validName := regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
	if !validName.MatchString(name) {
		t.Errorf("Invalid metric name: %s (must match [a-zA-Z_:][a-zA-Z0-9_:]*)", name)
	}

	// Should contain namespace prefix
	if !strings.HasPrefix(name, "ntp_") && !strings.HasPrefix(name, "ntp_exporter_") {
		t.Errorf("Metric name %s should have ntp_ or ntp_exporter_ prefix", name)
	}

	// Should use underscores, not hyphens
	if strings.Contains(name, "-") {
		t.Errorf("Metric name %s should use underscores, not hyphens", name)
	}
}

// ValidatePrometheusLabelName validates that a label name follows Prometheus conventions
func ValidatePrometheusLabelName(t *testing.T, name string) {
	t.Helper()

	// Must match regex: [a-zA-Z_][a-zA-Z0-9_]*
	validLabel := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	if !validLabel.MatchString(name) {
		t.Errorf("Invalid label name: %s (must match [a-zA-Z_][a-zA-Z0-9_]*)", name)
	}

	// Reserved label names
	reserved := []string{"__name__", "job", "instance"}
	for _, r := range reserved {
		if name == r {
			t.Errorf("Label name %s is reserved", name)
		}
	}
}
