// Package outbound enforces the destination policy for server-side HTTP requests.
package outbound

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type Policy struct {
	targets      map[string]struct{}
	ranges       []netip.Prefix
	lookup       func(context.Context, string) ([]net.IPAddr, error)
	now          func() time.Time
	cacheMu      sync.Mutex
	allowedHosts map[string]time.Time
}

const (
	preflightTTL      = 30 * time.Second
	maxPreflightHosts = 4096
)

var errHostResolution = errors.New("resolve outbound host")
var nat64Prefix = netip.MustParsePrefix("64:ff9b::/96")
var current atomic.Pointer[Policy]
var publicOnly, _ = NewPolicy(nil)

func NewPolicy(entries []string) (*Policy, error) {
	p := &Policy{
		targets: make(map[string]struct{}), lookup: net.DefaultResolver.LookupIPAddr,
		now: time.Now, allowedHosts: make(map[string]time.Time),
	}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			if prefix.Addr().Is4In6() {
				if prefix.Bits() < 96 {
					return nil, errors.New("outbound allow range has an invalid mapped IPv4 prefix")
				}
				prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
			}
			p.ranges = append(p.ranges, prefix.Masked())
			continue
		}
		host, port, err := net.SplitHostPort(entry)
		n, portErr := strconv.Atoi(port)
		if err != nil || host == "" || strings.ContainsAny(host, "/@?#% \t\r\n") || portErr != nil || n < 1 || n > 65535 {
			return nil, errors.New("outbound allow target must be host:port or CIDR")
		}
		p.targets[net.JoinHostPort(normalizeHost(host), strconv.Itoa(n))] = struct{}{}
	}
	return p, nil
}

// Configure replaces the process-wide policy and its preflight cache.
func Configure(entries []string) error {
	p, err := NewPolicy(entries)
	if err != nil {
		return err
	}
	current.Store(p)
	return nil
}

func Default() *Policy {
	if p := current.Load(); p != nil {
		return p
	}
	return publicOnly
}

func normalizeHost(host string) string {
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Unmap().String()
	}
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

// IsPrivateIP includes non-unicast, shared, unspecified and mapped private addresses.
func IsPrivateIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		return v4[0] == 0 || (v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127) ||
			(v4[0] == 192 && v4[1] == 0 && v4[2] == 0) ||
			(v4[0] == 198 && v4[1] >= 18 && v4[1] <= 19) || v4[0] >= 240
	}
	v6 := ip.To16()
	if v6 != nil {
		if v6[0] == 0x20 && v6[1] == 0x02 {
			return IsPrivateIP(net.IP(v6[2:6]))
		}
		if nat64Prefix.Contains(netip.AddrFrom16([16]byte(v6))) {
			return IsPrivateIP(net.IP(v6[12:16]))
		}
	}
	return false
}

func (p *Policy) allowed(host, port string, ip net.IP) bool {
	if _, ok := p.targets[net.JoinHostPort(normalizeHost(host), port)]; ok {
		return true
	}
	if ip == nil {
		return false
	}
	if _, ok := p.targets[net.JoinHostPort(ip.String(), port)]; ok {
		return true
	}
	address, ok := netip.AddrFromSlice(ip)
	if ok {
		for _, prefix := range p.ranges {
			if prefix.Contains(address.Unmap()) {
				return true
			}
		}
	}
	return false
}

func (p *Policy) checkIP(host, port string, ip net.IP) error {
	if !IsPrivateIP(ip) || p.allowed(host, port, ip) {
		return nil
	}
	slog.Warn("outbound target blocked", "host", host, "ip", ip.String())
	return errors.New("target address is not allowed: private or non-public address")
}

func urlAddress(u *url.URL) (string, string, error) {
	if u == nil || u.Hostname() == "" {
		return "", "", errors.New("invalid outbound URL")
	}
	if u.User != nil {
		return "", "", errors.New("outbound URL userinfo is not allowed")
	}
	port := u.Port()
	switch u.Scheme {
	case "http", "ws":
		if port == "" {
			port = "80"
		}
	case "https", "wss":
		if port == "" {
			port = "443"
		}
	default:
		return "", "", errors.New("outbound URL must use HTTP(S)")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || strings.Contains(u.Hostname(), "%") {
		return "", "", errors.New("invalid outbound URL address")
	}
	return normalizeHost(u.Hostname()), strconv.Itoa(n), nil
}

// ValidateLiteralURL performs the early save/update check without requiring DNS
// availability. Every request and connection independently validates DNS results.
func ValidateLiteralURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return errors.New("invalid outbound URL")
	}
	host, port, err := urlAddress(u)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip != nil {
		return Default().checkIP(host, port, ip)
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "metadata.google.internal" || host == "metadata.goog" || host == "instance-data.ec2.internal" {
		if !Default().allowed(host, port, nil) {
			slog.Warn("outbound target blocked", "host", host, "ip", "")
			return errors.New("target address is not allowed: local or metadata hostname")
		}
	}
	return nil
}

func (p *Policy) resolve(ctx context.Context, host, port string) ([]net.IPAddr, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IPAddr{{IP: ip}}, p.checkIP(host, port, ip)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := p.lookup(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errHostResolution, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("%w: no addresses", errHostResolution)
	}
	for _, addr := range addrs {
		if err := p.checkIP(host, port, addr.IP); err != nil {
			return nil, err
		}
	}
	return addrs, nil
}

// ValidateURL validates without caching when the connection route is unknown.
// Only known direct transports may use the preflight allow cache.
func (p *Policy) ValidateURL(ctx context.Context, u *url.URL) error {
	host, port, err := urlAddress(u)
	if err != nil {
		return err
	}
	_, err = p.resolve(ctx, host, port)
	return err
}

func (p *Policy) validateDirectURL(ctx context.Context, u *url.URL) error {
	host, port, err := urlAddress(u)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip != nil {
		return p.checkIP(host, port, ip)
	}
	key := net.JoinHostPort(host, port)
	p.cacheMu.Lock()
	if expires, ok := p.allowedHosts[key]; ok && p.now().Before(expires) {
		p.cacheMu.Unlock()
		return nil
	}
	delete(p.allowedHosts, key)
	p.cacheMu.Unlock()
	if _, err := p.resolve(ctx, host, port); err != nil {
		return err
	}
	p.cacheMu.Lock()
	if len(p.allowedHosts) >= maxPreflightHosts {
		for existing := range p.allowedHosts {
			delete(p.allowedHosts, existing)
			break
		}
	}
	p.allowedHosts[key] = p.now().Add(preflightTTL)
	p.cacheMu.Unlock()
	return nil
}

// ValidateRequest only caches direct preflight decisions, since the actual
// destination is checked again on each new direct socket. A proxy hides that
// destination, so its target must be resolved on every request without caching.
func (p *Policy) ValidateRequest(ctx context.Context, u *url.URL, proxied bool) error {
	if proxied {
		return p.ValidateURL(ctx, u)
	}
	err := p.validateDirectURL(ctx, u)
	if ctx.Err() == nil && errors.Is(err, errHostResolution) {
		return nil
	}
	return err
}

// DialContext delegates name resolution and address racing to net.Dialer. The
// socket hook checks every actual destination without using the preflight cache,
// preserving DNS rebinding protection while retaining the standard timeout and
// Happy Eyeballs semantics.
func (p *Policy) DialContext(ctx context.Context, dialer *net.Dialer, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	guarded := *dialer
	controlContext := guarded.ControlContext
	control := guarded.Control
	guarded.ControlContext = func(ctx context.Context, network, address string, conn syscall.RawConn) error {
		resolved, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if err := p.checkIP(host, port, net.ParseIP(resolved)); err != nil {
			return err
		}
		if controlContext != nil {
			return controlContext(ctx, network, address, conn)
		}
		if control != nil {
			return control(network, address, conn)
		}
		return nil
	}
	return guarded.DialContext(ctx, network, address)
}

func DialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return Default().DialContext(ctx, dialer, network, address)
	}
}

type guardedTransport struct {
	base      http.RoundTripper
	proxy     func(*http.Request) (*url.URL, error)
	strictDNS bool
}

func WrapTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = NewTransport()
	}
	if transport, ok := base.(*http.Transport); ok {
		return WrapTransportWithProxy(base, transport.Proxy)
	}
	return &guardedTransport{base: base, strictDNS: true}
}

// WrapTransportWithProxy retains route information when another wrapper hides
// the transport, or when SOCKS uses DialContext instead of Transport.Proxy.
func WrapTransportWithProxy(base http.RoundTripper, proxy func(*http.Request) (*url.URL, error)) http.RoundTripper {
	return &guardedTransport{base: base, proxy: proxy}
}

func (t *guardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	proxied := t.strictDNS
	if t.proxy != nil {
		proxyURL, err := t.proxy(req)
		if err != nil {
			if req.Body != nil {
				_ = req.Body.Close()
			}
			return nil, err
		}
		proxied = proxyURL != nil
	}
	if err := Default().ValidateRequest(req.Context(), req.URL, proxied); err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	return t.base.RoundTrip(req)
}

func (t *guardedTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func NewTransport() *http.Transport {
	return &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		DialContext:       DialContext(&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}),
		ForceAttemptHTTP2: true, MaxIdleConns: 100, IdleConnTimeout: 90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second, ExpectContinueTimeout: time.Second,
	}
}

func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{Transport: WrapTransport(NewTransport()), Timeout: timeout}
}
