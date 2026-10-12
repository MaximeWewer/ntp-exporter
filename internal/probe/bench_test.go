package probe

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/maximewewer/ntp-exporter/internal/ntp/ntptest"
)

// BenchmarkProberCycle measures one collection cycle over 50 targets
func BenchmarkProberCycle(b *testing.B) {
	mock := ntptest.NewMockNTPClient()
	servers := make([]string, 50)
	for i := range servers {
		servers[i] = fmt.Sprintf("s%d.example.org", i)
		mock.SetupSuccessfulServer(servers[i], time.Duration(i)*time.Millisecond, 2)
	}
	cfg := newTestConfig(servers...)
	cfg.NTP.SamplesPerServer = 4
	p := newProber(cfg, mock)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := p.Run(context.Background())
		for _, m := range result.Measurements {
			_ = m.Stats()
			_ = m.Best()
		}
	}
}
