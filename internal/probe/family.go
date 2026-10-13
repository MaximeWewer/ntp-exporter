package probe

import (
	"maps"
	"net"
	"sync"

	"github.com/maximewewer/ntp-exporter/internal/config"
)

// Address families
const (
	familyIPv4 = "ipv4"
	familyIPv6 = "ipv6"
)

// Addresses used to check that the host has a route for a family. Dialing UDP
// sends no packet: it only selects a source address from the routing table.
var routeProbes = map[string]string{
	familyIPv4: "192.0.2.1:123",     // TEST-NET-1 (RFC 5737)
	familyIPv6: "[2001:db8::1]:123", // documentation prefix (RFC 3849)
}

// hasRoute reports whether the host can reach a family through its routing table
func hasRoute(family string) bool {
	conn, err := net.Dial("udp", routeProbes[family])
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// addressFamily returns the family of an address, with or without port
func addressFamily(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		return familyIPv6
	}
	return familyIPv4
}

// families selects the addresses to query according to ip_family and to the
// routes of the host, and remembers the family that answered each target.
// In auto mode, a dual-stack server is queried over the family that answered
// last; when it does not answer, the other family is tried in the same cycle.
type families struct {
	setting  string
	hasRoute func(family string) bool

	mu        sync.Mutex
	routable  map[string]bool
	preferred map[string]preference
	// tick orders the uses of the preferences, to evict the least recently
	// used one when the memory is full
	tick uint64
}

// preference is the family that answered a target last
type preference struct {
	family string
	used   uint64
}

func newFamilies(setting string) *families {
	if setting == "" {
		setting = config.IPFamilyAuto
	}
	return &families{
		setting:   setting,
		hasRoute:  hasRoute,
		routable:  map[string]bool{familyIPv4: true, familyIPv6: true},
		preferred: make(map[string]preference),
	}
}

// refresh checks the routes of the host, once per cycle
func (f *families) refresh() {
	routable := map[string]bool{
		familyIPv4: f.hasRoute(familyIPv4),
		familyIPv6: f.hasRoute(familyIPv6),
	}
	f.mu.Lock()
	f.routable = routable
	f.mu.Unlock()
}

// copyRoutes takes the routes checked by another instance
func (f *families) copyRoutes(from *families) {
	from.mu.Lock()
	routable := maps.Clone(from.routable)
	from.mu.Unlock()
	f.mu.Lock()
	f.routable = routable
	f.mu.Unlock()
}

// usable reports whether a family may be queried
func (f *families) usable(family string) bool {
	switch f.setting {
	case config.IPFamilyIPv4:
		return family == familyIPv4
	case config.IPFamilyIPv6:
		return family == familyIPv6
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.routable[family]
}

// filter keeps the addresses of usable families. In auto mode, when no
// address is routable, every address is kept so that the failure shows up
// as an unreachable server rather than as a missing one.
func (f *families) filter(addrs []string) []string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if f.usable(addressFamily(a)) {
			out = append(out, a)
		}
	}
	if len(out) == 0 && f.setting == config.IPFamilyAuto {
		return addrs
	}
	return out
}

// candidates returns at most one address per family, in the order to try:
// the family that answered last first, then the resolver order
func (f *families) candidates(key string, addrs []string) []string {
	var out []string
	seen := make(map[string]bool, 2)
	for _, a := range f.filter(addrs) {
		family := addressFamily(a)
		if !seen[family] {
			seen[family] = true
			out = append(out, a)
		}
	}

	f.mu.Lock()
	pref, ok := f.preferred[key]
	if ok {
		f.tick++
		pref.used = f.tick
		f.preferred[key] = pref
	}
	f.mu.Unlock()
	if len(out) == 2 && addressFamily(out[1]) == pref.family {
		out[0], out[1] = out[1], out[0]
	}
	return out
}

// maxRemembered bounds the memory of the families that answered: /probe
// targets come from HTTP requests and are never forgotten by a cycle
const maxRemembered = 4096

// remember records the family that answered a target
func (f *families) remember(key, address string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.preferred[key]; !ok && len(f.preferred) >= maxRemembered {
		// Evict the least recently used target
		var oldest string
		var oldestUse uint64
		for k, p := range f.preferred {
			if oldest == "" || p.used < oldestUse {
				oldest, oldestUse = k, p.used
			}
		}
		delete(f.preferred, oldest)
	}
	f.tick++
	f.preferred[key] = preference{family: addressFamily(address), used: f.tick}
}

// forget drops the state of targets that are no longer probed
func (f *families) forget(keep map[string]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key := range f.preferred {
		if !keep[key] {
			delete(f.preferred, key)
		}
	}
}
