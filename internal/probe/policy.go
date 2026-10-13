package probe

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strings"
)

// ErrTargetForbidden is returned for a /probe target outside of the allowed
// targets, or resolving to a sensitive address
var ErrTargetForbidden = errors.New("target not allowed")

// cloudMetadata are the instance metadata services of the cloud providers
// outside of the link-local range
var cloudMetadata = []netip.Addr{
	netip.MustParseAddr("fd00:ec2::254"), // AWS over IPv6
}

// TargetPolicy decides which addresses a /probe request may reach, so that the
// endpoint cannot be used to scan the network of the exporter.
//
// An address is allowed when it lies in one of the allowed CIDR ranges.
// Otherwise loopback, link-local (including 169.254.169.254, the metadata
// service of the cloud providers), multicast and unspecified addresses are
// refused; any other address is allowed when no target list is configured,
// or when the target name matches one of the allowed expressions.
type TargetPolicy struct {
	prefixes []netip.Prefix
	names    []*regexp.Regexp
}

// NewTargetPolicy parses allowed targets: CIDR ranges (192.0.2.0/24) or
// regular expressions matched against the whole host name
// (time\.example\.org, .*\.ntp\.example\.org)
func NewTargetPolicy(allowed []string) (*TargetPolicy, error) {
	p := &TargetPolicy{}
	for _, entry := range allowed {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			p.prefixes = append(p.prefixes, prefix.Masked())
			continue
		}
		re, err := regexp.Compile("^(?:" + entry + ")$")
		if err != nil {
			return nil, fmt.Errorf("invalid allowed target %q: neither a CIDR range nor a regular expression: %w", entry, err)
		}
		p.names = append(p.names, re)
	}
	return p, nil
}

// restricted reports whether a target list is configured
func (p *TargetPolicy) restricted() bool {
	return len(p.prefixes) > 0 || len(p.names) > 0
}

// Check returns ErrTargetForbidden when host may not be queried at address.
// The address may carry a port.
func (p *TargetPolicy) Check(host, address string) error {
	if p == nil {
		return nil
	}
	ip := address
	if h, _, err := net.SplitHostPort(address); err == nil {
		ip = h
	}
	addr, err := netip.ParseAddr(strings.Trim(ip, "[]"))
	if err != nil {
		return fmt.Errorf("%w: %s is not an IP address", ErrTargetForbidden, address)
	}
	addr = addr.Unmap()

	for _, prefix := range p.prefixes {
		if prefix.Contains(addr) {
			return nil
		}
	}
	if sensitive(addr) {
		return fmt.Errorf("%w: %s resolves to %s", ErrTargetForbidden, host, addr)
	}
	if !p.restricted() {
		return nil
	}
	for _, re := range p.names {
		if re.MatchString(strings.ToLower(host)) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s (%s) is not in probe_endpoint.allowed_targets", ErrTargetForbidden, host, addr)
}

// sensitive reports whether an address must not be reached unless explicitly
// allowed: the exporter host itself, link-local services and metadata
// endpoints, multicast groups
func sensitive(addr netip.Addr) bool {
	if addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return true
	}
	for _, m := range cloudMetadata {
		if addr == m {
			return true
		}
	}
	return false
}
