//go:build !linux

package ntp

import "fmt"

// Read reads the current kernel NTP state (not supported on non-Linux)
func (k *KernelReader) Read() (*KernelTimex, error) {
	return nil, fmt.Errorf("kernel timex reading is not supported on this platform (Linux only)")
}
