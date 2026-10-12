package ntp

import (
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"
)

// fakeServer is a minimal local NTP server for tests that need the real client
// without network access
type fakeServer struct {
	conn     *net.UDPConn
	mu       sync.Mutex
	received []time.Time
	stratum  uint8
	kissCode string
}

const ntpEpochOffset = 2208988800

func putNTPTime(b []byte, t time.Time) {
	secs := uint64(t.Unix() + ntpEpochOffset)
	frac := uint64(t.Nanosecond()) << 32 / 1e9
	binary.BigEndian.PutUint32(b[0:4], uint32(secs))
	binary.BigEndian.PutUint32(b[4:8], uint32(frac))
}

// startFakeServer starts a server answering every request with a valid
// stratum 2 response (or a Kiss-o'-Death when kissCode is set)
func startFakeServer(t *testing.T, kissCode string) *fakeServer {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeServer{conn: conn, stratum: 2, kissCode: kissCode}
	t.Cleanup(func() { _ = conn.Close() })

	go func() {
		buf := make([]byte, 128)
		for {
			n, addr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if n < 48 {
				continue
			}
			now := time.Now()
			s.mu.Lock()
			s.received = append(s.received, now)
			s.mu.Unlock()

			resp := make([]byte, 48)
			resp[0] = 0<<6 | 4<<3 | 4 // LI=0, VN=4, mode=server
			resp[1] = s.stratum
			resp[2] = 6
			resp[3] = 0xEC                                     // precision 2^-20
			binary.BigEndian.PutUint32(resp[4:8], 0x00000080)  // root delay ~2ms
			binary.BigEndian.PutUint32(resp[8:12], 0x00000080) // root dispersion ~2ms
			copy(resp[12:16], []byte{192, 0, 2, 1})
			if s.kissCode != "" {
				resp[0] = 3<<6 | 4<<3 | 4
				resp[1] = 0
				copy(resp[12:16], s.kissCode)
			}
			putNTPTime(resp[16:24], now.Add(-10*time.Second))
			copy(resp[24:32], buf[40:48]) // origin = client transmit time
			putNTPTime(resp[32:40], now)
			putNTPTime(resp[40:48], now)
			_, _ = conn.WriteToUDP(resp, addr)
		}
	}()

	return s
}

func (s *fakeServer) addr() string {
	return s.conn.LocalAddr().String()
}

func (s *fakeServer) requests() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.received...)
}
