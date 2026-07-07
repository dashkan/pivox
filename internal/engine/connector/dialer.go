package connector

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

const (
	// defaultDialTimeout bounds the TCP connect for a single outbound attempt on
	// the broker's default client. It sits under defaultHTTPTimeout, which bounds
	// the whole round-trip; ctx cancellation still applies on top of both.
	defaultDialTimeout = 10 * time.Second
)

// errBlockedInternalTarget is the sentinel the safe dialer's Control hook returns
// when an outbound connection's RESOLVED peer address falls in an internal range.
// It is deliberately matchable via errors.Is so [Broker.Do] can classify a
// blocked target as TERMINAL — retrying an internal target never unblocks it —
// rather than as a retryable transport fault.
var errBlockedInternalTarget = errors.New("connection to an internal network address is not allowed")

// cgnatPrefix is RFC 6598 shared address space (carrier-grade NAT). netip's
// IsPrivate/IsLinkLocalUnicast predicates do NOT cover it, and it is routinely
// used for NAT gateways and some cloud-metadata proxies — so the safe default is
// to block it.
var cgnatPrefix = netip.MustParsePrefix("100.64.0.0/10")

// thisHostPrefix is RFC 1122 "this host on this network" (0.0.0.0/8). netip's
// IsUnspecified covers only 0.0.0.0 itself; some stacks route the rest of the
// block to loopback — a known localhost-bypass class — so block the whole /8.
var thisHostPrefix = netip.MustParsePrefix("0.0.0.0/8")

// nat64Prefix is the RFC 6052 NAT64 well-known prefix (64:ff9b::/96). It embeds
// an IPv4 address in its low 32 bits, so 64:ff9b::a9fe:a9fe reaches the metadata
// IP 169.254.169.254 through a NAT64 gateway. Block the whole prefix rather than
// decode the embedded v4.
var nat64Prefix = netip.MustParsePrefix("64:ff9b::/96")

// sixToFourPrefix is 6to4 (RFC 3056, 2002::/16), which embeds an IPv4 address in
// bits 16–48; a crafted 6to4 address can route toward an internal v4. 6to4 is
// deprecated, so blocking the whole prefix costs nothing legitimate.
var sixToFourPrefix = netip.MustParsePrefix("2002::/16")

// classEPrefix is 240.0.0.0/4 (RFC 1112 reserved/experimental). Not valid public
// unicast, so no legitimate target uses it; block it.
var classEPrefix = netip.MustParsePrefix("240.0.0.0/4")

// ipv4Broadcast is the limited-broadcast address; no netip predicate covers it.
var ipv4Broadcast = netip.MustParseAddr("255.255.255.255")

// newHTTPClient builds the broker's default *http.Client. When
// allowInternalNetworks is false — the REQUIRED default for shared multi-tenant
// cloud — the transport dials through a Control hook that refuses any connection
// whose RESOLVED peer IP is internal. Checking the resolved IP inside Control
// (not the URL host at parse time) is what makes the guard DNS-rebinding-safe: a
// hostname that resolves to 169.254.169.254 (or any internal address) is caught
// at connect time, after resolution, no matter what the URL looked like.
//
// When allowInternalNetworks is true (single-tenant on-prem, where the worker
// legitimately reaches internal systems) a plain dialer is used.
//
// No Proxy is configured: a forward proxy would make Control see the PROXY's IP
// instead of the target's, silently defeating the resolved-IP guard.
func newHTTPClient(allowInternalNetworks bool) *http.Client {
	dialer := &net.Dialer{
		Timeout:   defaultDialTimeout,
		KeepAlive: 30 * time.Second,
	}
	if !allowInternalNetworks {
		dialer.Control = blockInternalControl
	}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Timeout:   defaultHTTPTimeout,
		Transport: transport,
		// Do NOT follow redirects. Go re-sends custom request headers across a
		// cross-origin redirect (it strips only Authorization/Cookie), and a
		// connector's credential is commonly a custom header (X-Api-Key,
		// X-Auth-Token). Following a 302 to an attacker-controlled host would
		// leak that credential to a host the operator never configured. Return
		// the 3xx as the response for the activity to classify; the workflow
		// author handles redirects explicitly.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// blockInternalControl is the [net.Dialer.Control] hook that rejects a connection
// whose RESOLVED peer address is internal. address is the "ip:port" the stack is
// about to connect to (already resolved from the hostname); it is parsed with
// net/netip and matched against [isInternalAddr]. A blocked address returns
// [errBlockedInternalTarget] (wrapped with the offending address for logs). The
// c *syscall.RawConn is unused — the decision needs only the peer address.
func blockInternalControl(_, address string, _ syscall.RawConn) error {
	addrPort, err := netip.ParseAddrPort(address)
	if err != nil {
		// An unparseable dial address is not a target we can vet — fail closed.
		return fmt.Errorf("%w: unparseable dial address %q", errBlockedInternalTarget, address)
	}
	if addr := addrPort.Addr(); isInternalAddr(addr) {
		return fmt.Errorf("%w: %s", errBlockedInternalTarget, addr)
	}
	return nil
}

// isInternalAddr reports whether addr falls in a network range the safe dialer
// must refuse. It normalizes IPv4-mapped IPv6 (e.g. ::ffff:169.254.169.254) with
// Unmap so the IPv4 predicates apply, and fails closed on an invalid address.
//
// The blocked set: loopback, unspecified, RFC1918/RFC4193 private (incl. ULA),
// link-local unicast (169.254.0.0/16 — the cloud metadata IP — and fe80::/10),
// all multicast (link-local and interface-local included), and RFC 6598 CGNAT.
// Everything else — genuine public unicast — passes.
func isInternalAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	addr = addr.Unmap()
	switch {
	case addr.IsLoopback(), // 127.0.0.0/8, ::1
		addr.IsUnspecified(),             // 0.0.0.0, ::
		addr.IsPrivate(),                 // RFC1918 (10/8, 172.16/12, 192.168/16) + RFC4193 ULA fc00::/7
		addr.IsLinkLocalUnicast(),        // 169.254.0.0/16 (metadata) + fe80::/10
		addr.IsLinkLocalMulticast(),      // 224.0.0.0/24, ff02::/16
		addr.IsInterfaceLocalMulticast(), // ff01::/16
		addr.IsMulticast(),               // 224.0.0.0/4 + ff00::/8 (superset of the two above)
		cgnatPrefix.Contains(addr),       // RFC 6598 100.64.0.0/10 — carrier-grade NAT / shared space
		thisHostPrefix.Contains(addr),    // RFC 1122 0.0.0.0/8 — "this host"
		classEPrefix.Contains(addr),      // RFC 1112 240.0.0.0/4 — reserved/experimental
		nat64Prefix.Contains(addr),       // RFC 6052 64:ff9b::/96 — NAT64 (embeds internal v4)
		sixToFourPrefix.Contains(addr),   // RFC 3056 2002::/16 — 6to4 (embeds v4)
		addr == ipv4Broadcast:            // 255.255.255.255 — limited broadcast
		return true
	}
	return false
}
