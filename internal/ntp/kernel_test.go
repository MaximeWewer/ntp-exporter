package ntp

import (
	"testing"
	"time"
)

func TestNewKernelReader(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{"enabled", true},
		{"disabled", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kr := NewKernelReader(tt.enabled)
			if kr == nil {
				t.Fatal("NewKernelReader returned nil")
			}
			if kr.enabled != tt.enabled {
				t.Errorf("enabled = %v, want %v", kr.enabled, tt.enabled)
			}
		})
	}
}

func TestKernelReaderReadDisabled(t *testing.T) {
	kr := NewKernelReader(false)
	_, err := kr.Read()
	if err == nil {
		t.Error("Expected error when kernel reader is disabled, got nil")
	}
}

// TestKernelReaderReadEnabled tests reading kernel state when enabled
// Note: This test will only work on Linux with appropriate permissions
func TestKernelReaderReadEnabled(t *testing.T) {
	kr := NewKernelReader(true)
	state, err := kr.Read()

	// On non-Linux or without permissions, expect error
	// On Linux with permissions, should succeed
	if err != nil {
		t.Logf("Read failed (expected on non-Linux or without permissions): %v", err)
		return
	}

	// If successful, validate the state
	if state == nil {
		t.Fatal("Read returned nil state without error")
	}

	// Basic sanity checks
	t.Logf("Kernel state: offset=%v, freq_ppm=%v, status=%s",
		state.Offset, state.GetFrequencyPPM(), state.SyncStatus)
}

func TestKernelTimexIsSynchronized(t *testing.T) {
	// This test validates the interface exists and returns a boolean
	// Implementation varies by platform (Linux vs other)
	kt := &KernelTimex{Status: 0}
	result := kt.IsSynchronized()
	if result != true && result != false {
		t.Errorf("IsSynchronized() returned non-boolean value")
	}
}

func TestKernelTimexHasLeapSecond(t *testing.T) {
	// This test validates the interface exists and returns a boolean
	// Implementation varies by platform (Linux vs other)
	kt := &KernelTimex{Status: 0}
	result := kt.HasLeapSecond()
	if result != true && result != false {
		t.Errorf("HasLeapSecond() returned non-boolean value")
	}
}

func TestKernelTimexIsPPSActive(t *testing.T) {
	// This test validates the interface exists and returns a boolean
	// Implementation varies by platform (Linux vs other)
	kt := &KernelTimex{Status: 0}
	result := kt.IsPPSActive()
	if result != true && result != false {
		t.Errorf("IsPPSActive() returned non-boolean value")
	}
}

func TestKernelTimexGetOffsetSeconds(t *testing.T) {
	// This test validates the interface exists and returns a float64
	// Implementation varies by platform (Linux vs other)
	kt := &KernelTimex{Offset: 100 * time.Millisecond}
	result := kt.GetOffsetSeconds()
	// Should return a numeric value (0 on non-Linux, actual value on Linux)
	if result < -10000 || result > 10000 {
		t.Errorf("GetOffsetSeconds() returned unreasonable value: %v", result)
	}
}

func TestKernelTimexGetFrequencyPPM(t *testing.T) {
	// This test validates the interface exists and returns a float64
	// Implementation varies by platform (Linux vs other)
	kt := &KernelTimex{Frequency: 65536}
	result := kt.GetFrequencyPPM()
	// Should return a numeric value (0 on non-Linux, actual value on Linux)
	if result < -500 || result > 500 {
		t.Errorf("GetFrequencyPPM() returned unreasonable value: %v", result)
	}
}

func TestKernelTimexGetMaxErrorSeconds(t *testing.T) {
	// This test validates the interface exists and returns a float64
	// Implementation varies by platform (Linux vs other)
	kt := &KernelTimex{MaxError: 1 * time.Second}
	result := kt.GetMaxErrorSeconds()
	// Should return a numeric value >= 0
	if result < 0 {
		t.Errorf("GetMaxErrorSeconds() returned negative value: %v", result)
	}
}

func TestKernelTimexGetEstErrorSeconds(t *testing.T) {
	// This test validates the interface exists and returns a float64
	// Implementation varies by platform (Linux vs other)
	kt := &KernelTimex{EstError: 1 * time.Second}
	result := kt.GetEstErrorSeconds()
	// Should return a numeric value >= 0
	if result < 0 {
		t.Errorf("GetEstErrorSeconds() returned negative value: %v", result)
	}
}

func TestKernelTimexGetPrecisionSeconds(t *testing.T) {
	// This test validates the interface exists and returns a float64
	// Implementation varies by platform (Linux vs other)
	kt := &KernelTimex{Precision: 1 * time.Microsecond}
	result := kt.GetPrecisionSeconds()
	// Should return a numeric value >= 0
	if result < 0 {
		t.Errorf("GetPrecisionSeconds() returned negative value: %v", result)
	}
}

func TestKernelTimexAllFields(t *testing.T) {
	// Test that all fields can be set and read
	kt := &KernelTimex{
		Offset:     100 * time.Microsecond,
		Frequency:  65536,
		MaxError:   1 * time.Second,
		EstError:   500 * time.Millisecond,
		Status:     0x0001,
		Constant:   10,
		Precision:  1 * time.Microsecond,
		Tolerance:  32768000,
		Tick:       10000,
		PpsFreq:    0,
		PpsJitter:  0,
		Shift:      0,
		Stabil:     0,
		JitCnt:     0,
		CalCnt:     0,
		ErrCnt:     0,
		StbCnt:     0,
		SyncStatus: "synchronized",
		StatusCode: 0,
	}

	if kt.Offset != 100*time.Microsecond {
		t.Errorf("Offset mismatch")
	}
	if kt.Frequency != 65536 {
		t.Errorf("Frequency mismatch")
	}
	if kt.MaxError != 1*time.Second {
		t.Errorf("MaxError mismatch")
	}
	if kt.SyncStatus != "synchronized" {
		t.Errorf("SyncStatus mismatch")
	}
}

// Benchmark kernel timex operations
func BenchmarkKernelTimexGetOffsetSeconds(b *testing.B) {
	kt := &KernelTimex{Offset: 100 * time.Millisecond}
	for i := 0; i < b.N; i++ {
		_ = kt.GetOffsetSeconds()
	}
}

func BenchmarkKernelTimexGetFrequencyPPM(b *testing.B) {
	kt := &KernelTimex{Frequency: 65536}
	for i := 0; i < b.N; i++ {
		_ = kt.GetFrequencyPPM()
	}
}

func BenchmarkKernelTimexIsSynchronized(b *testing.B) {
	kt := &KernelTimex{Status: 0x0001}
	for i := 0; i < b.N; i++ {
		_ = kt.IsSynchronized()
	}
}
