package ntp

import "time"

// KernelTimex represents the kernel NTP state
type KernelTimex struct {
	Offset     time.Duration // Time offset in microseconds
	Frequency  int64         // Frequency offset (scaled ppm)
	MaxError   time.Duration // Maximum error
	EstError   time.Duration // Estimated error
	Status     int32         // Clock status
	Constant   int64         // PLL time constant
	Precision  time.Duration // Clock precision
	Tolerance  int64         // Clock frequency tolerance
	Tick       int64         // Microseconds per tick
	PpsFreq    int64         // PPS frequency
	PpsJitter  int64         // PPS jitter
	Shift      int32         // PPS interval duration (seconds)
	Stabil     int64         // PPS stability
	JitCnt     int64         // PPS jitter limit exceeded count
	CalCnt     int64         // PPS calibration intervals
	ErrCnt     int64         // PPS calibration errors
	StbCnt     int64         // PPS stability limit exceeded count
	SyncStatus string        // Human-readable sync status
	StatusCode int32         // Numeric status code
}

// KernelReader reads kernel NTP state
type KernelReader struct {
	enabled bool
}

// NewKernelReader creates a new kernel reader
func NewKernelReader(enabled bool) *KernelReader {
	return &KernelReader{
		enabled: enabled,
	}
}

// GetOffsetSeconds returns the offset in seconds (for metrics)
func (k *KernelTimex) GetOffsetSeconds() float64 {
	return k.Offset.Seconds()
}

// GetFrequencyPPM returns the frequency offset in PPM
func (k *KernelTimex) GetFrequencyPPM() float64 {
	// Kernel frequency is in scaled PPM (65536 = 1 ppm)
	return float64(k.Frequency) / 65536.0
}

// GetMaxErrorSeconds returns max error in seconds
func (k *KernelTimex) GetMaxErrorSeconds() float64 {
	return k.MaxError.Seconds()
}

// GetEstErrorSeconds returns estimated error in seconds
func (k *KernelTimex) GetEstErrorSeconds() float64 {
	return k.EstError.Seconds()
}

// GetPrecisionSeconds returns precision in seconds
func (k *KernelTimex) GetPrecisionSeconds() float64 {
	return k.Precision.Seconds()
}
