package phone

import (
	"encoding/pem"
	"net"
	"sort"
	"strings"
)

func pemBlock(typ string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func lowerSet(values []string) []string {
	out := make([]string, len(values))
	for index, value := range values {
		out[index] = strings.ToLower(value)
	}
	return out
}

func containsFold(set []string, want string) bool {
	want = strings.ToLower(want)
	for _, value := range set {
		if value == want {
			return true
		}
	}
	return false
}

// clientIP extracts the peer IP from a request, falling back to the whole
// RemoteAddr when it is not a host:port pair (e.g. unit-test fakes).
func clientIP(remoteAddr string) string {
	if remoteAddr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// LANAddress returns the first routable non-loopback IPv4 so a phone on the
// same network can reach the listener. It is this host's own address, empty
// when the machine has none (so the UI can fall back to a manual base).
func LANAddress() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipNet.IP.To4()
		if ip4 == nil || ip4.IsLoopback() || ip4.IsUnspecified() || ip4.IsLinkLocalUnicast() {
			continue
		}
		return ip4.String()
	}
	return ""
}

// PairingBase assembles the scheme, host and port a phone should dial. The
// scheme follows the TLS mode ("off" implies a fronting HTTPS/HTTP proxy, so we
// degrade to http since Phase 1 is keys only). When no LAN address is present
// the host is empty and callers must offer a manual base.
func PairingBase(tlsMode, addr string) (scheme, host, port string) {
	scheme = "https"
	if strings.EqualFold(strings.TrimSpace(tlsMode), "off") {
		scheme = "http"
	}
	host = LANAddress()
	if _, p, err := net.SplitHostPort(addr); err == nil {
		port = p
	}
	return scheme, host, port
}
