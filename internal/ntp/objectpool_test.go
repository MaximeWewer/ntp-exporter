package ntp

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsePool(t *testing.T) {
	// Get response from pool
	r1 := GetResponse()
	if r1 == nil {
		t.Fatal("GetResponse returned nil")
	}

	// Modify response
	r1.Server = "test.server.com"
	r1.Offset = 100 * time.Millisecond
	r1.RTT = 50 * time.Millisecond
	r1.Stratum = 2

	// Return to pool
	PutResponse(r1)

	// Get another response (should be the same cleared object)
	r2 := GetResponse()
	if r2 == nil {
		t.Fatal("GetResponse returned nil after put")
	}

	// Verify it was cleared
	if r2.Server != "" {
		t.Errorf("Response.Server was not cleared: got %q, want empty", r2.Server)
	}
	if r2.Offset != 0 {
		t.Errorf("Response.Offset was not cleared: got %v, want 0", r2.Offset)
	}
	if r2.RTT != 0 {
		t.Errorf("Response.RTT was not cleared: got %v, want 0", r2.RTT)
	}
	if r2.Stratum != 0 {
		t.Errorf("Response.Stratum was not cleared: got %v, want 0", r2.Stratum)
	}

	PutResponse(r2)
}

func TestResponsePoolNil(t *testing.T) {
	// Should not panic
	PutResponse(nil)
}

func TestResponseSlicePool(t *testing.T) {
	// Get slice from pool
	s1 := GetResponseSlice()
	if s1 == nil {
		t.Fatal("GetResponseSlice returned nil")
	}
	if len(*s1) != 0 {
		t.Errorf("Initial slice length = %d, want 0", len(*s1))
	}

	// Add some responses
	*s1 = append(*s1, &Response{Server: "server1"})
	*s1 = append(*s1, &Response{Server: "server2"})
	if len(*s1) != 2 {
		t.Errorf("Slice length after append = %d, want 2", len(*s1))
	}

	// Return to pool
	PutResponseSlice(s1)

	// Get another slice (should be cleared)
	s2 := GetResponseSlice()
	if s2 == nil {
		t.Fatal("GetResponseSlice returned nil after put")
	}
	if len(*s2) != 0 {
		t.Errorf("Slice length after clear = %d, want 0", len(*s2))
	}

	PutResponseSlice(s2)
}

func TestResponseSlicePoolNil(t *testing.T) {
	// Should not panic
	PutResponseSlice(nil)
}

func BenchmarkResponsePoolAllocation(b *testing.B) {
	b.Run("WithPool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			r := GetResponse()
			r.Server = "test.server.com"
			r.Offset = 100 * time.Millisecond
			PutResponse(r)
		}
	})

	b.Run("WithoutPool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			r := &Response{
				Server: "test.server.com",
				Offset: 100 * time.Millisecond,
			}
			_ = r
		}
	})
}

func BenchmarkResponseSlicePoolAllocation(b *testing.B) {
	b.Run("WithPool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			s := GetResponseSlice()
			*s = append(*s, &Response{Server: "server1"})
			*s = append(*s, &Response{Server: "server2"})
			PutResponseSlice(s)
		}
	})

	b.Run("WithoutPool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			s := make([]*Response, 0, 10)
			s = append(s, &Response{Server: "server1"})
			s = append(s, &Response{Server: "server2"})
			_ = s
		}
	})
}

func TestFloat64SlicePool(t *testing.T) {
	// Get slice from pool
	s := GetFloat64Slice(10)
	require.NotNil(t, s)
	assert.Equal(t, 0, len(*s))
	assert.GreaterOrEqual(t, cap(*s), 10)

	// Use the slice
	*s = append(*s, 1.0, 2.0, 3.0)
	assert.Equal(t, 3, len(*s))

	// Return to pool
	PutFloat64Slice(s)

	// Get again - should be reset
	s2 := GetFloat64Slice(5)
	assert.Equal(t, 0, len(*s2))

	PutFloat64Slice(s2)
}

func TestFloat64SlicePool_Nil(t *testing.T) {
	// Should not panic
	assert.NotPanics(t, func() {
		PutFloat64Slice(nil)
	})
}

func TestFloat64SlicePool_LargeCapacity(t *testing.T) {
	// Request large capacity
	s := GetFloat64Slice(1000)
	assert.GreaterOrEqual(t, cap(*s), 1000)
	PutFloat64Slice(s)
}

func TestStringSlicePool(t *testing.T) {
	// Get slice from pool
	s := GetStringSlice()
	require.NotNil(t, s)
	assert.Equal(t, 0, len(*s))

	// Use the slice
	*s = append(*s, "error1", "error2", "warning1")
	assert.Equal(t, 3, len(*s))

	// Return to pool
	PutStringSlice(s)

	// Get again - should be reset
	s2 := GetStringSlice()
	assert.Equal(t, 0, len(*s2))

	PutStringSlice(s2)
}

func TestStringSlicePool_Nil(t *testing.T) {
	// Should not panic
	assert.NotPanics(t, func() {
		PutStringSlice(nil)
	})
}

func TestStringSlicePool_Concurrent(t *testing.T) {
	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < 10; j++ {
				s := GetStringSlice()
				*s = append(*s, fmt.Sprintf("msg-%d-%d", id, j))
				PutStringSlice(s)
			}
		}(i)
	}

	wg.Wait()
}

func BenchmarkFloat64SlicePoolAllocation(b *testing.B) {
	b.Run("WithPool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			s := GetFloat64Slice(10)
			*s = append(*s, 1.0, 2.0, 3.0, 4.0, 5.0)
			PutFloat64Slice(s)
		}
	})

	b.Run("WithoutPool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			s := make([]float64, 0, 10)
			s = append(s, 1.0, 2.0, 3.0, 4.0, 5.0)
		}
	})
}

func BenchmarkStringSlicePoolAllocation(b *testing.B) {
	b.Run("WithPool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			s := GetStringSlice()
			*s = append(*s, "error1", "error2", "warning1")
			PutStringSlice(s)
		}
	})

	b.Run("WithoutPool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			s := make([]string, 0, 10)
			s = append(s, "error1", "error2", "warning1")
		}
	})
}
