package ip

import (
	"net"
	"net/netip"
	"strings"

	"github.com/gin-gonic/gin"
)

const proxyPolicyKey = "sub2api.trusted_proxy_policy"

var CloudflareProxyRanges = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22", "2400:cb00::/32",
	"2606:4700::/32", "2803:f800::/32", "2405:b500::/32", "2405:8100::/32",
	"2a06:98c0::/29", "2c0f:f248::/32",
}

type ProxyPolicy struct {
	Local []netip.Prefix
	CDN   []netip.Prefix
	// LegacyMetadata only affects request metadata; security callers stay strict.
	LegacyMetadata bool
}

func ParseProxyRanges(entries []string) (prefixes []netip.Prefix, err error) {
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		prefix, parseErr := netip.ParsePrefix(entry)
		if parseErr != nil {
			address, addrErr := netip.ParseAddr(entry)
			if addrErr != nil || address.Zone() != "" {
				return nil, &net.ParseError{Type: "IP address or CIDR", Text: entry}
			}
			address = address.Unmap()
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func SetProxyPolicy(c *gin.Context, policy ProxyPolicy) {
	c.Set(proxyPolicyKey, policy)
}

func peerAddress(c *gin.Context) netip.Addr {
	if c == nil || c.Request == nil {
		return netip.Addr{}
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		host = c.Request.RemoteAddr
	}
	address, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return netip.Addr{}
	}
	return address.Unmap()
}

func containsProxy(prefixes []netip.Prefix, address netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func resolveTrustedClientIP(c *gin.Context) string {
	peer := peerAddress(c)
	if !peer.IsValid() {
		return ""
	}
	value, exists := c.Get(proxyPolicyKey)
	policy, valid := value.(ProxyPolicy)
	if !exists || !valid {
		return normalizeIP(c.ClientIP())
	}
	if !containsProxy(policy.Local, peer) && !containsProxy(policy.CDN, peer) {
		return peer.String()
	}
	current := peer
	chain := strings.Join(c.Request.Header.Values("X-Forwarded-For"), ",")
	if chain != "" {
		parts := strings.Split(chain, ",")
		if len(parts) > 64 {
			return peer.String()
		}
		for index := len(parts) - 1; index >= 0; index-- {
			candidate, err := netip.ParseAddr(strings.TrimSpace(parts[index]))
			if err != nil || candidate.Zone() != "" {
				return peer.String()
			}
			candidate = candidate.Unmap()
			if !containsProxy(policy.Local, candidate) && !containsProxy(policy.CDN, candidate) {
				return candidate.String()
			}
			current = candidate
		}
	}
	if containsProxy(policy.CDN, current) {
		settings, _ := requestForwardedIPSettings(c)
		headers := append(append([]string{}, settings.headers...), "X-Real-IP")
		cloudflare, _ := ParseProxyRanges(CloudflareProxyRanges)
		if containsProxy(cloudflare, current) {
			headers = append([]string{"CF-Connecting-IP"}, headers...)
		}
		for _, header := range headers {
			if address, err := netip.ParseAddr(strings.TrimSpace(c.GetHeader(header))); err == nil && address.Zone() == "" {
				return address.Unmap().String()
			}
		}
	}
	if chain == "" && containsProxy(policy.Local, peer) {
		if address, err := netip.ParseAddr(strings.TrimSpace(c.GetHeader("X-Real-IP"))); err == nil && address.Zone() == "" {
			return address.Unmap().String()
		}
	}
	return current.String()
}
