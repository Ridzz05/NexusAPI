package network

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

// ClientIP resolves the client address using forwarding headers only when the
// immediate peer is in the configured trusted proxy set. Forwarded values are
// interpreted as a client-to-proxy chain and scanned from right to left; the
// first address outside the trusted set is the effective client. This keeps a
// client from spoofing its identity when it connects directly to the API.
func ClientIP(r *http.Request, trusted []netip.Prefix) netip.Addr {
	peer, ok := parseAddress(r.RemoteAddr)
	if !ok {
		return netip.Addr{}
	}
	if !contains(trusted, peer) {
		return peer
	}

	forwarded := parseForwarded(r.Header.Values("Forwarded"))
	if len(forwarded) == 0 {
		forwarded = parseXForwardedFor(r.Header.Values("X-Forwarded-For"))
	}
	if len(forwarded) == 0 {
		if realIP, ok := parseAddress(r.Header.Get("X-Real-IP")); ok {
			forwarded = []netip.Addr{realIP}
		}
	}
	if len(forwarded) == 0 {
		return peer
	}

	chain := append(forwarded, peer)
	for i := len(chain) - 1; i >= 0; i-- {
		if !contains(trusted, chain[i]) {
			return chain[i]
		}
	}
	return chain[0]
}

func contains(prefixes []netip.Prefix, address netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func parseXForwardedFor(values []string) []netip.Addr {
	var addresses []netip.Addr
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			if address, ok := parseAddress(item); ok {
				addresses = append(addresses, address)
			}
		}
	}
	return addresses
}

func parseForwarded(values []string) []netip.Addr {
	var addresses []netip.Addr
	for _, value := range values {
		for _, element := range strings.Split(value, ",") {
			for _, parameter := range strings.Split(element, ";") {
				key, raw, ok := strings.Cut(strings.TrimSpace(parameter), "=")
				if !ok || !strings.EqualFold(strings.TrimSpace(key), "for") {
					continue
				}
				if address, ok := parseAddress(raw); ok {
					addresses = append(addresses, address)
				}
				break
			}
		}
	}
	return addresses
}

func parseAddress(value string) (netip.Addr, bool) {
	value = strings.TrimSpace(strings.Trim(value, "\""))
	if value == "" || strings.EqualFold(value, "unknown") || strings.HasPrefix(value, "_") {
		return netip.Addr{}, false
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return address, true
	}
	if addressPort, err := netip.ParseAddrPort(value); err == nil {
		return addressPort.Addr(), true
	}
	if host, port, err := net.SplitHostPort(value); err == nil {
		if _, err := strconv.ParseUint(port, 10, 16); err == nil {
			if address, err := netip.ParseAddr(host); err == nil {
				return address, true
			}
		}
	}
	return netip.Addr{}, false
}
