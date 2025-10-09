//go:build !linux
// +build !linux

package ntp

import (
	"fmt"
	"time"
)

// KernelTimex represents the kernel NTP state (stub for non-Linux platforms)
type KernelTimex struct {
	Offset     time.Duration
	Frequency  int64
	MaxError   time.Duration
	EstError   time.Duration
	Status     int32
	Constant   int64
	Precision  time.Duration
	Tolerance  int64
	Tick       int64
	PpsFreq    int64
	PpsJitter  int64
	Shift      int32
	Stabil     int64
	JitCnt     int64
	CalCnt     int64
	ErrCnt     int64
	StbCnt     int64
	SyncStatus string
	StatusCode int32
}

// KernelReader reads kernel NTP state (stub for non-Linux platforms)
type KernelReader struct {
	enabled bool
}

// NewKernelReader creates a new kernel reader
func NewKernelReader(enabled bool) *KernelReader {
	return &KernelReader{
		enabled: enabled,
	}
}

// Read reads the current kernel NTP state (not supported on non-Linux)
func (k *KernelReader) Read() (*KernelTimex, error) {
	return nil, fmt.Errorf("kernel timex reading is not supported on this platform (Linux only)")
}

// IsSynchronized returns true if the kernel clock is synchronized
func (k *KernelTimex) IsSynchronized() bool {
	return false
}

// HasLeapSecond returns true if a leap second is pending
func (k *KernelTimex) HasLeapSecond() bool {
	return false
}

// IsPPSActive returns true if PPS signal is active
func (k *KernelTimex) IsPPSActive() bool {
	return false
}

// GetOffsetSeconds returns the offset in seconds (for metrics)
func (k *KernelTimex) GetOffsetSeconds() float64 {
	return 0.0
}

// GetFrequencyPPM returns the frequency offset in PPM
func (k *KernelTimex) GetFrequencyPPM() float64 {
	return 0.0
}

// GetMaxErrorSeconds returns max error in seconds
func (k *KernelTimex) GetMaxErrorSeconds() float64 {
	return 0.0
}

// GetEstErrorSeconds returns estimated error in seconds
func (k *KernelTimex) GetEstErrorSeconds() float64 {
	return 0.0
}

// GetPrecisionSeconds returns precision in seconds
func (k *KernelTimex) GetPrecisionSeconds() float64 {
	return 0.0
}
