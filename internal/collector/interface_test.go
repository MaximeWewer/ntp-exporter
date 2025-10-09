package collector

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Mock collector for testing
type mockCollector struct {
	name    string
	enabled bool
	err     error
}

func (m *mockCollector) Collect(ctx context.Context) error {
	return m.err
}

func (m *mockCollector) Name() string {
	return m.name
}

func (m *mockCollector) Enabled() bool {
	return m.enabled
}

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if r.collectors == nil {
		t.Error("collectors slice is nil")
	}
	if len(r.collectors) != 0 {
		t.Errorf("new registry should have 0 collectors, got %d", len(r.collectors))
	}
}

func TestRegistryRegister(t *testing.T) {
	r := NewRegistry()

	// Register first collector
	c1 := &mockCollector{name: "test1", enabled: true}
	r.Register(c1)

	if r.Count() != 1 {
		t.Errorf("Count() = %d, want 1", r.Count())
	}

	// Register second collector
	c2 := &mockCollector{name: "test2", enabled: true}
	r.Register(c2)

	if r.Count() != 2 {
		t.Errorf("Count() = %d, want 2", r.Count())
	}
}

func TestRegistryCount(t *testing.T) {
	r := NewRegistry()

	if r.Count() != 0 {
		t.Errorf("Empty registry Count() = %d, want 0", r.Count())
	}

	r.Register(&mockCollector{name: "test1", enabled: true})
	r.Register(&mockCollector{name: "test2", enabled: false})
	r.Register(&mockCollector{name: "test3", enabled: true})

	if r.Count() != 3 {
		t.Errorf("Count() = %d, want 3", r.Count())
	}
}

func TestRegistryEnabledCount(t *testing.T) {
	r := NewRegistry()

	if r.EnabledCount() != 0 {
		t.Errorf("Empty registry EnabledCount() = %d, want 0", r.EnabledCount())
	}

	r.Register(&mockCollector{name: "test1", enabled: true})
	r.Register(&mockCollector{name: "test2", enabled: false})
	r.Register(&mockCollector{name: "test3", enabled: true})
	r.Register(&mockCollector{name: "test4", enabled: false})

	expected := 2
	if r.EnabledCount() != expected {
		t.Errorf("EnabledCount() = %d, want %d", r.EnabledCount(), expected)
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()

	c1 := &mockCollector{name: "test1", enabled: true}
	c2 := &mockCollector{name: "test2", enabled: false}

	r.Register(c1)
	r.Register(c2)

	list := r.List()
	if len(list) != 2 {
		t.Errorf("List() length = %d, want 2", len(list))
	}

	if list[0].Name() != "test1" {
		t.Errorf("First collector name = %s, want test1", list[0].Name())
	}
	if list[1].Name() != "test2" {
		t.Errorf("Second collector name = %s, want test2", list[1].Name())
	}
}

func TestRegistryCollectAll(t *testing.T) {
	tests := []struct {
		name         string
		collectors   []Collector
		expectError  bool
		errorMessage string
	}{
		{
			name:        "empty registry",
			collectors:  []Collector{},
			expectError: false,
		},
		{
			name: "all collectors succeed",
			collectors: []Collector{
				&mockCollector{name: "test1", enabled: true, err: nil},
				&mockCollector{name: "test2", enabled: true, err: nil},
			},
			expectError: false,
		},
		{
			name: "one collector fails",
			collectors: []Collector{
				&mockCollector{name: "test1", enabled: true, err: nil},
				&mockCollector{name: "test2", enabled: true, err: errors.New("test error")},
			},
			expectError:  true,
			errorMessage: "test2",
		},
		{
			name: "disabled collector not executed",
			collectors: []Collector{
				&mockCollector{name: "test1", enabled: true, err: nil},
				&mockCollector{name: "test2", enabled: false, err: errors.New("should not run")},
			},
			expectError: false,
		},
		{
			name: "all disabled",
			collectors: []Collector{
				&mockCollector{name: "test1", enabled: false, err: nil},
				&mockCollector{name: "test2", enabled: false, err: nil},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			for _, c := range tt.collectors {
				r.Register(c)
			}

			ctx := context.Background()
			err := r.CollectAll(ctx)

			if tt.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if tt.expectError && err != nil && tt.errorMessage != "" {
				if !strings.Contains(err.Error(), tt.errorMessage) {
					t.Errorf("Error message %q does not contain %q", err.Error(), tt.errorMessage)
				}
			}
		})
	}
}

func TestRegistryCollectAllContextCancellation(t *testing.T) {
	r := NewRegistry()

	// Create a collector that checks context
	c := &contextAwareCollector{name: "test", enabled: true}
	r.Register(c)

	// Cancel context before collection
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.CollectAll(ctx)
	if err == nil {
		t.Error("Expected error when context is cancelled, got nil")
	}
}

// contextAwareCollector checks if context is cancelled
type contextAwareCollector struct {
	name    string
	enabled bool
}

func (c *contextAwareCollector) Collect(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (c *contextAwareCollector) Name() string {
	return c.name
}

func (c *contextAwareCollector) Enabled() bool {
	return c.enabled
}


func BenchmarkRegistryCollectAll(b *testing.B) {
	r := NewRegistry()

	// Register several mock collectors
	for i := 0; i < 10; i++ {
		r.Register(&mockCollector{name: "test", enabled: true, err: nil})
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.CollectAll(ctx)
	}
}
