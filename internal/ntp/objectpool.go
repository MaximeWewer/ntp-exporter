package ntp

import (
	"sync"

	"github.com/beevik/ntp"
)

// Global object pools to reduce allocations in hot paths
var (
	// responsePool reduces allocations for NTP Response objects
	responsePool = sync.Pool{
		New: func() interface{} {
			return &Response{}
		},
	}

	// responseSlicePool reduces allocations for response slices
	responseSlicePool = sync.Pool{
		New: func() interface{} {
			s := make([]*Response, 0, 10)
			return &s
		},
	}

	// ntpResponsePool reduces allocations for beevik/ntp Response objects
	ntpResponsePool = sync.Pool{
		New: func() interface{} {
			return &ntp.Response{}
		},
	}

	// float64SlicePool reduces allocations for float64 slices used in statistics
	float64SlicePool = sync.Pool{
		New: func() interface{} {
			s := make([]float64, 0, 100) // Pre-allocate for common case
			return &s
		},
	}

	// stringSlicePool reduces allocations for string slices (errors/warnings in validator)
	stringSlicePool = sync.Pool{
		New: func() interface{} {
			s := make([]string, 0, 10)
			return &s
		},
	}
)

// GetResponse gets a Response from the pool
func GetResponse() *Response {
	return responsePool.Get().(*Response)
}

// PutResponse returns a Response to the pool
func PutResponse(r *Response) {
	if r == nil {
		return
	}
	// Clear sensitive data before returning to pool
	*r = Response{}
	responsePool.Put(r)
}

// GetResponseSlice gets a response slice from the pool
func GetResponseSlice() *[]*Response {
	s := responseSlicePool.Get().(*[]*Response)
	*s = (*s)[:0] // Reset slice length to 0
	return s
}

// PutResponseSlice returns a response slice to the pool
func PutResponseSlice(s *[]*Response) {
	if s == nil {
		return
	}
	// Clear the slice
	for i := range *s {
		(*s)[i] = nil
	}
	*s = (*s)[:0]
	responseSlicePool.Put(s)
}

// GetNTPResponse gets a beevik/ntp Response from the pool
func GetNTPResponse() *ntp.Response {
	return ntpResponsePool.Get().(*ntp.Response)
}

// PutNTPResponse returns a beevik/ntp Response to the pool
func PutNTPResponse(r *ntp.Response) {
	if r == nil {
		return
	}
	// Clear sensitive data before returning to pool
	*r = ntp.Response{}
	ntpResponsePool.Put(r)
}

// GetFloat64Slice gets a float64 slice from the pool
func GetFloat64Slice(capacity int) *[]float64 {
	s := float64SlicePool.Get().(*[]float64)
	// Resize if needed
	if cap(*s) < capacity {
		*s = make([]float64, 0, capacity)
	} else {
		*s = (*s)[:0] // Reset length to 0
	}
	return s
}

// PutFloat64Slice returns a float64 slice to the pool
func PutFloat64Slice(s *[]float64) {
	if s == nil {
		return
	}
	// Clear the slice
	*s = (*s)[:0]
	float64SlicePool.Put(s)
}

// GetStringSlice gets a string slice from the pool
func GetStringSlice() *[]string {
	s := stringSlicePool.Get().(*[]string)
	*s = (*s)[:0] // Reset length to 0
	return s
}

// PutStringSlice returns a string slice to the pool
func PutStringSlice(s *[]string) {
	if s == nil {
		return
	}
	// Clear the slice
	for i := range *s {
		(*s)[i] = ""
	}
	*s = (*s)[:0]
	stringSlicePool.Put(s)
}
