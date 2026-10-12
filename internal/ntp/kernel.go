package ntp

import "time"

// Kernel clock states (adjtimex return value) and status flags (STA_*)
const (
	TIME_OK       = 0 // Clock synchronized
	TIME_INS      = 1 // Insert leap second
	TIME_DEL      = 2 // Delete leap second
	TIME_OOP      = 3 // Leap second in progress
	TIME_WAIT     = 4 // Leap second has occurred
	TIME_ERROR    = 5 // Clock not synchronized
	STA_PLL       = 0x0001
	STA_PPSFREQ   = 0x0002
	STA_PPSTIME   = 0x0004
	STA_FLL       = 0x0008
	STA_INS       = 0x0010
	STA_DEL       = 0x0020
	STA_UNSYNC    = 0x0040
	STA_FREQHOLD  = 0x0080
	STA_PPSSIGNAL = 0x0100
	STA_PPSJITTER = 0x0200
	STA_PPSWANDER = 0x0400
	STA_PPSERROR  = 0x0800
	STA_CLOCKERR  = 0x1000
	STA_NANO      = 0x2000
	STA_MODE      = 0x4000
	STA_CLK       = 0x8000
)

// KernelTimex represents the kernel NTP state
type KernelTimex struct {
	Offset     time.Duration // Residual PLL time offset
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
	TAI        int64         // TAI-UTC offset in seconds
	State      int           // Clock state returned by adjtimex (TIME_*)
	SyncStatus string        // Human-readable sync status
	StatusCode int32         // Status flags (STA_*)
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

// IsSynchronized returns true if the kernel clock is synchronized
func (k *KernelTimex) IsSynchronized() bool {
	return (k.Status & STA_UNSYNC) == 0
}

// HasLeapSecond returns true if a leap second is pending
func (k *KernelTimex) HasLeapSecond() bool {
	return (k.Status&STA_INS) != 0 || (k.Status&STA_DEL) != 0
}

// IsPPSActive returns true if PPS signal is active
func (k *KernelTimex) IsPPSActive() bool {
	return (k.Status & STA_PPSSIGNAL) != 0
}

// getStatusString converts the clock state returned by adjtimex (TIME_*)
// and the status flags into a human-readable string
func getStatusString(state int, status int32) string {
	if (status & STA_UNSYNC) != 0 {
		return "unsynchronized"
	}

	if (status & STA_CLOCKERR) != 0 {
		return "clock_error"
	}

	return stateString(state)
}

// stateString returns the name of a TIME_* clock state
func stateString(state int) string {
	switch state {
	case TIME_OK:
		return "synchronized"
	case TIME_INS:
		return "leap_insert_pending"
	case TIME_DEL:
		return "leap_delete_pending"
	case TIME_OOP:
		return "leap_in_progress"
	case TIME_WAIT:
		return "leap_occurred"
	case TIME_ERROR:
		return "error"
	default:
		return "unknown"
	}
}

// StateName returns the name of the clock state returned by adjtimex
func (k *KernelTimex) StateName() string {
	return stateString(k.State)
}
