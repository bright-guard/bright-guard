// Package exposure classifies MCP server endpoint addresses as internal vs
// public reachable, purely by URL/host syntax — no DNS lookups, no probes.
// DNS resolution is deliberately avoided here: it would be slow, flaky in
// tests, and non-deterministic across environments. A later pass may add an
// active reachability probe; for now we only label based on the address
// itself.
package exposure

import (
	"net"
	"net/url"
	"strings"
)

// State values stored on mcp_servers.exposure_state.
const (
	StateUnknown       = "unknown"
	StateInternal      = "internal"
	StateCloudInternal = "cloud_internal"
	StatePublic        = "public"
	StateUnreachable   = "unreachable"
)

// internalSuffixes are DNS suffixes that mark a host as RFC1918-equivalent.
var internalSuffixes = []string{
	".local",
	".internal",
	".lan",
	".intranet",
	".svc.cluster.local",
	".cluster.local",
	".svc",
}

// cloudInternalSuffixes are non-public hosted suffixes from a cloud provider's
// private-IP DNS zones.
var cloudInternalSuffixes = []string{
	".internal.amazonaws.com",
	".compute.internal",         // AWS EC2 private DNS
	".ec2.internal",             // AWS EC2 (us-east-1 legacy)
	".internal.gcp.local",
	".internal.cloudapp.net",    // Azure internal
}

// explicitInternalHosts are well-known bare hostnames that always resolve to
// loopback / the local machine.
var explicitInternalHosts = map[string]struct{}{
	"localhost":             {},
	"host.docker.internal":  {},
	"gateway.docker.internal": {},
}

// Classify returns (state, reason) for a server address string. It never
// returns an error; failure modes are folded into StateUnknown / reason.
func Classify(address string) (string, string) {
	addr := strings.TrimSpace(address)
	if addr == "" {
		return StateUnknown, "empty address"
	}

	// Tolerate bare host[:port] inputs in addition to full URLs.
	u, err := parseAddress(addr)
	if err != nil || u.Host == "" {
		return StateUnknown, "unparseable address"
	}

	host := u.Hostname()
	if host == "" {
		return StateUnknown, "no host in address"
	}
	host = strings.ToLower(host)

	if _, ok := explicitInternalHosts[host]; ok {
		return StateInternal, "loopback/docker host alias"
	}

	if ip := net.ParseIP(host); ip != nil {
		return classifyIP(ip)
	}

	// DNS name path.
	for _, suf := range cloudInternalSuffixes {
		if strings.HasSuffix(host, suf) {
			return StateCloudInternal, "matches cloud-internal suffix " + suf
		}
	}
	for _, suf := range internalSuffixes {
		if strings.HasSuffix(host, suf) {
			return StateInternal, "matches internal suffix " + suf
		}
	}

	// Single-label hostname with no dot: typically resolves only on a private
	// search domain.
	if !strings.Contains(host, ".") {
		return StateInternal, "single-label hostname"
	}

	return StatePublic, "public DNS name (assumed)"
}

// parseAddress accepts either a full URL ("https://x.example/path") or a bare
// "host[:port]" form and returns a url.URL with the Host populated.
func parseAddress(addr string) (*url.URL, error) {
	if strings.Contains(addr, "://") {
		return url.Parse(addr)
	}
	// Bare host[:port] — wrap in a scheme so url.Parse fills Host.
	return url.Parse("placeholder://" + addr)
}

func classifyIP(ip net.IP) (string, string) {
	if ip.IsLoopback() {
		return StateInternal, "loopback IP"
	}
	if ip.IsUnspecified() {
		return StateInternal, "unspecified IP"
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return StateInternal, "link-local IP"
	}
	if ip.IsPrivate() {
		return StateInternal, "RFC1918/private IP"
	}
	// IPv6 unique-local (fc00::/7) — net.IP.IsPrivate covers this, but be
	// explicit for any IPv6 ULAs that slip past on older Go versions.
	if ip.To4() == nil && len(ip) == net.IPv6len && ip[0]&0xfe == 0xfc {
		return StateInternal, "IPv6 unique-local"
	}
	return StatePublic, "public IP literal"
}
