package ntp

import (
	"context"
	"encoding/binary"
	"math"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// encodeChronyFloat is the inverse of chronyFloat (chrony UTI_FloatHostToNetwork)
func encodeChronyFloat(x float64) int32 {
	if x == 0 {
		return 0
	}
	e := int(math.Floor(math.Log2(math.Abs(x)))) + 1 - 24
	coef := int32(math.Round(x / math.Pow(2, float64(e))))
	field := uint32(e+25) & 0x7f
	return int32(field<<25 | uint32(coef)&(1<<25-1))
}

func trackingReply(sequence uint32, status uint16, family uint16) []byte {
	b := make([]byte, chronyReplyHead+chronyTrackLen)
	b[0] = chronyProtoVersion
	b[1] = chronyPktReply
	binary.BigEndian.PutUint16(b[4:], chronyReqTracking)
	binary.BigEndian.PutUint16(b[6:], chronyRpyTracking)
	binary.BigEndian.PutUint16(b[8:], status)
	binary.BigEndian.PutUint32(b[16:], sequence)

	t := b[chronyReplyHead:]
	switch family {
	case 1:
		binary.BigEndian.PutUint32(t, 0xC0000201) // ref ID of 192.0.2.1
		copy(t[4:], net.ParseIP("192.0.2.1").To4())
	case 0:
		copy(t, "PPS\x00")
	}
	binary.BigEndian.PutUint16(t[20:], family)
	binary.BigEndian.PutUint16(t[24:], 2) // stratum
	binary.BigEndian.PutUint16(t[26:], 0) // leap status
	binary.BigEndian.PutUint32(t[28:], chronyNoHighSec)
	binary.BigEndian.PutUint32(t[32:], 1_800_000_000)
	binary.BigEndian.PutUint32(t[36:], 500_000_000)
	for i, v := range []float64{-0.0011, 0.00034, 0.0012, -33.6, 0.007, 0.39, 0.046, 0.005, 1030.5} {
		binary.BigEndian.PutUint32(t[40+4*i:], uint32(encodeChronyFloat(v)))
	}
	return b
}

// fakeChronyd answers tracking requests; reply builds the answer for a sequence
func fakeChronyd(t *testing.T, conn net.PacketConn, reply func(seq uint32) [][]byte) {
	t.Helper()
	go func() {
		buf := make([]byte, 1024)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if n != chronyRequestLen || buf[0] != chronyProtoVersion || buf[1] != chronyPktRequest ||
				binary.BigEndian.Uint16(buf[4:]) != chronyReqTracking {
				continue
			}
			for _, r := range reply(binary.BigEndian.Uint32(buf[8:])) {
				_, _ = conn.WriteTo(r, addr)
			}
		}
	}()
}

func TestChronyFloat(t *testing.T) {
	assert.Equal(t, 0.0, chronyFloat(0))
	for _, v := range []float64{1, -1, 0.5, 1e-9, -33.6, 1030.5, 123456.789} {
		assert.InEpsilon(t, v, chronyFloat(encodeChronyFloat(v)), 1e-6, "%v", v)
	}
}

func TestChronyClient_TrackingUDP(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer conn.Close()
	fakeChronyd(t, conn, func(seq uint32) [][]byte {
		// A stale reply to another request comes first and must be skipped
		return [][]byte{trackingReply(seq+1, 0, 1), trackingReply(seq, 0, 1)}
	})

	c, err := NewChronyClient("udp://"+conn.LocalAddr().String(), time.Second)
	require.NoError(t, err)
	tr, err := c.Tracking(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "C0000201", tr.RefIDHex())
	assert.Equal(t, "192.0.2.1", tr.Source)
	assert.Equal(t, uint16(2), tr.Stratum)
	assert.Equal(t, time.Unix(1_800_000_000, 500_000_000), tr.RefTime)
	assert.InEpsilon(t, -0.0011, tr.CurrentCorrection, 1e-6)
	assert.InEpsilon(t, -33.6, tr.FreqPPM, 1e-6)
	assert.InEpsilon(t, 1030.5, tr.LastUpdateInterval, 1e-6)
}

func TestChronyClient_TrackingUnixSocket(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chronyd.sock")
	conn, err := net.ListenPacket("unixgram", path)
	require.NoError(t, err)
	defer conn.Close()

	// Record where the client bound its reply socket
	replyDirs := make(chan string, 1)
	recorder := &recordingConn{PacketConn: conn, from: replyDirs}
	fakeChronyd(t, recorder, func(seq uint32) [][]byte { return [][]byte{trackingReply(seq, 0, 0)} })

	c, err := NewChronyClient("unix://"+path, time.Second)
	require.NoError(t, err)
	tr, err := c.Tracking(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "PPS", tr.Source, "reference clocks are named by their reference ID")

	// Like chronyc, the reply socket sits next to the chronyd socket: chronyd
	// may not see the /tmp of the exporter (PrivateTmp, pod)
	assert.Equal(t, dir, <-replyDirs)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "the reply socket is removed after the request")
}

// recordingConn reports the directory of the sender of each datagram
type recordingConn struct {
	net.PacketConn
	from chan string
}

func (r *recordingConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := r.PacketConn.ReadFrom(b)
	if err == nil {
		select {
		case r.from <- filepath.Dir(addr.String()):
		default:
		}
	}
	return n, addr, err
}

func TestChronyClient_Errors(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer conn.Close()
	fakeChronyd(t, conn, func(seq uint32) [][]byte { return [][]byte{trackingReply(seq, 2, 1)} })

	c, err := NewChronyClient("udp://"+conn.LocalAddr().String(), time.Second)
	require.NoError(t, err)
	_, err = c.Tracking(context.Background())
	assert.ErrorContains(t, err, "refused")

	silent, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer silent.Close()
	c, err = NewChronyClient("udp://"+silent.LocalAddr().String(), 100*time.Millisecond)
	require.NoError(t, err)
	_, err = c.Tracking(context.Background())
	assert.ErrorContains(t, err, "no chrony reply")

	c, err = NewChronyClient("unix://"+filepath.Join(t.TempDir(), "missing.sock"), time.Second)
	require.NoError(t, err)
	_, err = c.Tracking(context.Background())
	assert.ErrorContains(t, err, "connection")
}

func TestDecodeChronyTracking_Invalid(t *testing.T) {
	_, err := decodeChronyTracking([]byte{6, 2}, 1)
	assert.ErrorContains(t, err, "too short")

	r := trackingReply(1, 0, 1)
	r[0] = 5
	_, err = decodeChronyTracking(r, 1)
	assert.ErrorContains(t, err, "unsupported")

	r = trackingReply(1, 0, 1)
	binary.BigEndian.PutUint16(r[6:], 3)
	_, err = decodeChronyTracking(r, 1)
	assert.ErrorContains(t, err, "unexpected")

	_, err = decodeChronyTracking(trackingReply(1, 0, 1)[:40], 1)
	assert.ErrorContains(t, err, "too short")
}

func TestParseChronyAddress(t *testing.T) {
	network, addr, err := ParseChronyAddress("udp://127.0.0.1:323")
	require.NoError(t, err)
	assert.Equal(t, "udp", network)
	assert.Equal(t, "127.0.0.1:323", addr)

	network, addr, err = ParseChronyAddress("unix:///run/chrony/chronyd.sock")
	require.NoError(t, err)
	assert.Equal(t, "unixgram", network)
	assert.Equal(t, "/run/chrony/chronyd.sock", addr)

	for _, bad := range []string{"127.0.0.1:323", "udp://127.0.0.1", "unix://", "tcp://127.0.0.1:323", "%zz"} {
		_, _, err := ParseChronyAddress(bad)
		assert.Error(t, err, bad)
	}
}
