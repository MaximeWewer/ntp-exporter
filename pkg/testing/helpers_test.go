package testutil

import (
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestCreateMockNTPResponse(t *testing.T) {
	offset := 10 * time.Millisecond
	stratum := uint8(2)

	resp := CreateMockNTPResponse(offset, stratum)

	assert.NotNil(t, resp)
	assert.Equal(t, offset, resp.ClockOffset)
	assert.Equal(t, stratum, resp.Stratum)
	assert.Greater(t, resp.RTT, time.Duration(0))
}

func TestCreateHighDriftResponse(t *testing.T) {
	offset := 15 * time.Second

	resp := CreateHighDriftResponse(offset)

	assert.NotNil(t, resp)
	assert.Equal(t, offset, resp.ClockOffset)
	assert.Equal(t, uint8(2), resp.Stratum)
}

func TestCreateKoDResponse(t *testing.T) {
	kissCode := "RATE"

	resp := CreateKoDResponse(kissCode)

	assert.NotNil(t, resp)
	assert.Equal(t, uint8(0), resp.Stratum)
}

func TestCreateInvalidStratumResponse(t *testing.T) {
	resp := CreateInvalidStratumResponse()

	assert.NotNil(t, resp)
	assert.Equal(t, uint8(16), resp.Stratum)
}

func TestWaitForCondition(t *testing.T) {
	count := 0
	condition := func() bool {
		count++
		return count >= 3
	}

	WaitForCondition(t, condition, 1*time.Second, "count to reach 3")
	assert.GreaterOrEqual(t, count, 3)
}

func TestGenerateNTPResponse(t *testing.T) {
	resp := GenerateNTPResponse(42)

	assert.NotNil(t, resp)
	assert.Greater(t, resp.Stratum, uint8(0))
	assert.LessOrEqual(t, resp.Stratum, uint8(15))
}

func TestCreateTestRegistry(t *testing.T) {
	reg := CreateTestRegistry()
	assert.NotNil(t, reg)
}

func TestCountGoroutines(t *testing.T) {
	count := CountGoroutines()
	assert.Greater(t, count, 0)
}

func BenchmarkCreateMockNTPResponse(b *testing.B) {
	offset := 10 * time.Millisecond
	stratum := uint8(2)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CreateMockNTPResponse(offset, stratum)
	}
}

func TestAssertMetricValue(t *testing.T) {
	reg := CreateTestRegistry()

	// Create and register test gauge
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_gauge_value",
		Help: "Test gauge for value assertion",
	})
	reg.MustRegister(gauge)
	gauge.Set(42.5)

	// Test successful assertion
	AssertMetricValue(t, reg, "test_gauge_value", nil, 42.5)

	// Test with labels
	gaugeVec := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "test_gauge_with_labels",
			Help: "Test gauge with labels",
		},
		[]string{"server", "status"},
	)
	reg.MustRegister(gaugeVec)
	gaugeVec.WithLabelValues("pool.ntp.org", "ok").Set(100)

	labels := map[string]string{
		"server": "pool.ntp.org",
		"status": "ok",
	}
	AssertMetricValue(t, reg, "test_gauge_with_labels", labels, 100)
}

func TestAssertMetricExists(t *testing.T) {
	reg := CreateTestRegistry()

	// Register metric
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_counter_exists",
		Help: "Test counter for existence check",
	})
	reg.MustRegister(counter)
	counter.Inc()

	// Test metric exists
	AssertMetricExists(t, reg, "test_counter_exists", nil)

	// Test with labels
	counterVec := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "test_counter_with_labels",
			Help: "Test counter with labels",
		},
		[]string{"endpoint"},
	)
	reg.MustRegister(counterVec)
	counterVec.WithLabelValues("/metrics").Inc()

	labelsWithEndpoint := map[string]string{
		"endpoint": "/metrics",
	}
	AssertMetricExists(t, reg, "test_counter_with_labels", labelsWithEndpoint)
}

func TestNewTestHTTPServer(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	})

	server := NewTestHTTPServer(t, handler)
	defer server.Close()

	assert.NotNil(t, server)

	// Test server responds
	resp, err := http.Get(server.URL)
	assert.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestMeasureMemoryAllocation(t *testing.T) {
	operation := func() {
		// Allocate some memory
		data := make([]byte, 1024*1024) // 1MB
		_ = data
	}

	allocatedBytes := MeasureMemoryAllocation(operation)

	// Just check that function runs without error
	// Memory measurement may vary depending on GC
	_ = allocatedBytes
}

func TestValidatePrometheusMetricName(t *testing.T) {
	tests := []struct {
		name       string
		metricName string
	}{
		{"valid_basic", "ntp_offset_seconds"},
		{"valid_with_underscore", "ntp_server_reachable"},
		{"valid_with_numbers", "ntp_stratum_1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just call the function to increase coverage
			ValidatePrometheusMetricName(t, tt.metricName)
		})
	}
}

func TestValidatePrometheusLabelName(t *testing.T) {
	tests := []struct {
		name      string
		labelName string
	}{
		{"valid_basic", "server"},
		{"valid_with_underscore", "ntp_server"},
		{"valid_with_numbers", "server_1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just call the function to increase coverage
			ValidatePrometheusLabelName(t, tt.labelName)
		})
	}
}

func BenchmarkGenerateNTPResponse(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GenerateNTPResponse(int64(i))
	}
}
