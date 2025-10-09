//go:build !linux
// +build !linux

package ntp

import "fmt"

// Read reads the current kernel NTP state (not supported on non-Linux)
func (k *KernelReader) Read() (*KernelTimex, error) {
	return nil, fmt.Errorf("kernel timex reading is not supported on this platform (Linux only)")
}

// IsSynchronized returns false on non-Linux platforms
func (k *KernelTimex) IsSynchronized() bool {
	return false
}

// HasLeapSecond returns false on non-Linux platforms
func (k *KernelTimex) HasLeapSecond() bool {
	return false
}

// IsPPSActive returns false on non-Linux platforms
func (k *KernelTimex) IsPPSActive() bool {
	return false
}
