package ntp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// chronyd command protocol (cmdmon), version 6. Only the read-only
// "tracking" request is implemented: it is allowed from localhost over UDP
// port 323 by default, and from the unix socket for its owner group.
const (
	chronyProtoVersion = 6
	chronyPktRequest   = 1
	chronyPktReply     = 2
	chronyReqTracking  = 33
	chronyRpyTracking  = 5
	chronyStatusOK     = 0
	// Requests are padded to the reply size (anti-amplification rule)
	chronyRequestLen = 20 + 396
	chronyReplyHead  = 28
	chronyTrackLen   = 4 + 20 + 2 + 2 + 12 + 9*4
	chronyNoHighSec  = 0x7fffffff
)

// DefaultChronyAddress is the command port of chronyd on the local host
const DefaultChronyAddress = "udp://127.0.0.1:323"

// ChronyTracking is the "tracking" report of chronyd: the source selected by
// the daemon and the state of the system clock it disciplines
type ChronyTracking struct {
	RefID uint32
	// Source is the IP address of the selected source, or the reference
	// identifier as text for reference clocks (PPS, GPS...)
	Source             string
	Stratum            uint16
	LeapStatus         uint16
	RefTime            time.Time
	CurrentCorrection  float64 // seconds, positive when the system clock is slow
	LastOffset         float64 // seconds
	RMSOffset          float64 // seconds
	FreqPPM            float64
	ResidFreqPPM       float64
	SkewPPM            float64
	RootDelay          float64 // seconds
	RootDispersion     float64 // seconds
	LastUpdateInterval float64 // seconds
}

// RefIDHex renders the reference identifier like chronyc
func (t *ChronyTracking) RefIDHex() string {
	return fmt.Sprintf("%08X", t.RefID)
}

// ChronyClient reads the tracking report of a chronyd instance
type ChronyClient struct {
	network string // "udp" or "unixgram"
	address string
	timeout time.Duration
}

// NewChronyClient creates a client for an address such as
// udp://127.0.0.1:323 or unix:///run/chrony/chronyd.sock
func NewChronyClient(address string, timeout time.Duration) (*ChronyClient, error) {
	network, addr, err := ParseChronyAddress(address)
	if err != nil {
		return nil, err
	}
	return &ChronyClient{network: network, address: addr, timeout: timeout}, nil
}

// ParseChronyAddress returns the network and address of a chronyd URL
func ParseChronyAddress(address string) (network, addr string, err error) {
	u, err := url.Parse(address)
	if err != nil {
		return "", "", fmt.Errorf("invalid chrony address %q: %w", address, err)
	}
	switch u.Scheme {
	case "udp":
		if _, _, err := net.SplitHostPort(u.Host); err != nil {
			return "", "", fmt.Errorf("invalid chrony address %q: expected udp://host:port", address)
		}
		return "udp", u.Host, nil
	case "unix":
		if u.Path == "" {
			return "", "", fmt.Errorf("invalid chrony address %q: expected unix:///path/to/chronyd.sock", address)
		}
		return "unixgram", u.Path, nil
	default:
		return "", "", fmt.Errorf("invalid chrony address %q: scheme must be udp or unix", address)
	}
}

// Tracking sends a tracking request and decodes the reply
func (c *ChronyClient) Tracking(ctx context.Context) (*ChronyTracking, error) {
	conn, cleanup, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	deadline := time.Now().Add(c.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}

	var seq [4]byte
	if _, err := rand.Read(seq[:]); err != nil {
		return nil, err
	}
	sequence := binary.BigEndian.Uint32(seq[:])

	if _, err := conn.Write(chronyTrackingRequest(sequence)); err != nil {
		return nil, fmt.Errorf("chrony request to %s failed: %w", c.address, err)
	}

	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return nil, fmt.Errorf("no chrony reply from %s: %w", c.address, err)
		}
		tracking, err := decodeChronyTracking(buf[:n], sequence)
		if errors.Is(err, errChronyOtherReply) {
			continue
		}
		return tracking, err
	}
}

// dial connects to chronyd. A unix datagram client needs its own bound
// socket so that chronyd can send the reply back.
func (c *ChronyClient) dial(ctx context.Context) (net.Conn, func(), error) {
	if c.network == "udp" {
		var d net.Dialer
		conn, err := d.DialContext(ctx, "udp", c.address)
		if err != nil {
			return nil, nil, fmt.Errorf("chrony connection to %s failed: %w", c.address, err)
		}
		return conn, func() { _ = conn.Close() }, nil
	}

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return nil, nil, err
	}
	local := filepath.Join(os.TempDir(), "ntp-exporter-chrony-"+hex.EncodeToString(suffix[:])+".sock")
	conn, err := net.DialUnix("unixgram",
		&net.UnixAddr{Name: local, Net: "unixgram"},
		&net.UnixAddr{Name: c.address, Net: "unixgram"})
	if err != nil {
		_ = os.Remove(local)
		return nil, nil, fmt.Errorf("chrony connection to %s failed: %w", c.address, err)
	}
	// chronyd runs as another user: it must be able to write the reply
	_ = os.Chmod(local, 0o666) //nolint:gosec // short-lived reply socket, removed after the request
	return conn, func() {
		_ = conn.Close()
		_ = os.Remove(local)
	}, nil
}

// chronyTrackingRequest encodes a tracking request, padded to the reply size
func chronyTrackingRequest(sequence uint32) []byte {
	req := make([]byte, chronyRequestLen)
	req[0] = chronyProtoVersion
	req[1] = chronyPktRequest
	binary.BigEndian.PutUint16(req[4:], chronyReqTracking)
	binary.BigEndian.PutUint32(req[8:], sequence)
	return req
}

var errChronyOtherReply = errors.New("reply to another request")

// decodeChronyTracking decodes a tracking reply
func decodeChronyTracking(b []byte, sequence uint32) (*ChronyTracking, error) {
	if len(b) < chronyReplyHead {
		return nil, fmt.Errorf("chrony reply too short: %d bytes", len(b))
	}
	if b[0] != chronyProtoVersion || b[1] != chronyPktReply {
		return nil, fmt.Errorf("unsupported chrony reply (version %d, type %d)", b[0], b[1])
	}
	if binary.BigEndian.Uint32(b[16:]) != sequence {
		return nil, errChronyOtherReply
	}
	if status := binary.BigEndian.Uint16(b[8:]); status != chronyStatusOK {
		return nil, fmt.Errorf("chrony refused the tracking request (status %d)", status)
	}
	if reply := binary.BigEndian.Uint16(b[6:]); reply != chronyRpyTracking {
		return nil, fmt.Errorf("unexpected chrony reply type %d", reply)
	}
	if len(b) < chronyReplyHead+chronyTrackLen {
		return nil, fmt.Errorf("chrony tracking reply too short: %d bytes", len(b))
	}

	r := bytes.NewReader(b[chronyReplyHead:])
	var raw struct {
		RefID      uint32
		IP         [16]byte
		Family     uint16
		Pad        uint16
		Stratum    uint16
		LeapStatus uint16
		SecHigh    uint32
		SecLow     uint32
		Nsec       uint32
		Floats     [9]int32
	}
	if err := binary.Read(r, binary.BigEndian, &raw); err != nil {
		return nil, fmt.Errorf("invalid chrony tracking reply: %w", err)
	}

	t := &ChronyTracking{
		RefID:              raw.RefID,
		Stratum:            raw.Stratum,
		LeapStatus:         raw.LeapStatus,
		CurrentCorrection:  chronyFloat(raw.Floats[0]),
		LastOffset:         chronyFloat(raw.Floats[1]),
		RMSOffset:          chronyFloat(raw.Floats[2]),
		FreqPPM:            chronyFloat(raw.Floats[3]),
		ResidFreqPPM:       chronyFloat(raw.Floats[4]),
		SkewPPM:            chronyFloat(raw.Floats[5]),
		RootDelay:          chronyFloat(raw.Floats[6]),
		RootDispersion:     chronyFloat(raw.Floats[7]),
		LastUpdateInterval: chronyFloat(raw.Floats[8]),
	}

	high := uint64(raw.SecHigh)
	if raw.SecHigh == chronyNoHighSec {
		high = 0
	}
	if sec := high<<32 | uint64(raw.SecLow); sec != 0 || raw.Nsec != 0 {
		t.RefTime = time.Unix(int64(sec), int64(raw.Nsec)) //nolint:gosec // seconds since 1970 fit in int64
	}

	switch raw.Family {
	case 1:
		t.Source = net.IP(raw.IP[:4]).String()
	case 2:
		t.Source = net.IP(raw.IP[:]).String()
	default:
		// Reference clock or no source: the reference ID is a 4-character name
		var name [4]byte
		binary.BigEndian.PutUint32(name[:], raw.RefID)
		t.Source = strings.TrimRight(strings.Map(func(r rune) rune {
			if r < 0x20 || r > 0x7e {
				return -1
			}
			return r
		}, string(name[:])), " ")
	}

	return t, nil
}

// chronyFloat decodes the 32-bit floating-point format of chronyd: a 7-bit
// signed exponent and a 25-bit signed coefficient, value = coef * 2^(exp-25)
func chronyFloat(x int32) float64 {
	const expBits, coefBits = 7, 25
	u := uint32(x)              //nolint:gosec // bit reinterpretation
	exp := int32(u >> coefBits) //nolint:gosec // 7-bit value
	if exp >= 1<<(expBits-1) {
		exp -= 1 << expBits
	}
	exp -= coefBits
	coef := int32(u % (1 << coefBits)) //nolint:gosec // 25-bit value
	if coef >= 1<<(coefBits-1) {
		coef -= 1 << coefBits
	}
	return float64(coef) * math.Pow(2, float64(exp))
}
