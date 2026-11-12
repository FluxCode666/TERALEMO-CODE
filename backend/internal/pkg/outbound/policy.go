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
	"sync/atomic"
	"syscall"
	"time"
)

type Policy struct {
	targets map[string]struct{}
	ranges  []netip.Prefix
	lookup  func(context.Context, string) ([]net.IPAddr, error)
}

var current atomic.Pointer[Policy]
var publicOnly, _ = NewPolicy(nil)

func NewPolicy(entries []string) (*Policy, error) {
	p := &Policy{targets: make(map[string]struct{}), lookup: net.DefaultResolver.LookupIPAddr}
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

// Configure installs an immutable process-wide policy before clients are created.
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
		return v4[0] == 0 || (v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127) || v4[0] >= 240
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
		return nil, fmt.Errorf("resolve outbound host: %w", err)
	}
	if len(addrs) == 0 {
		return nil, errors.New("outbound host has no addresses")
	}
	for _, addr := range addrs {
		if err := p.checkIP(host, port, addr.IP); err != nil {
			return nil, err
		}
	}
	return addrs, nil
}

func (p *Policy) ValidateURL(ctx context.Context, u *url.URL) error {
	host, port, err := urlAddress(u)
	if err != nil {
		return err
	}
	_, err = p.resolve(ctx, host, port)
	return err
}

// DialContext resolves once and connects only to those checked addresses. The
// socket hook also checks the actual destination before establishing a connection.
func (p *Policy) DialContext(ctx context.Context, dialer *net.Dialer, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addrs, err := p.resolve(ctx, host, port)
	if err != nil {
		return nil, err
	}
	guarded := *dialer
	control := guarded.Control
	guarded.Control = func(network, address string, conn syscall.RawConn) error {
		resolved, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if err := p.checkIP(host, port, net.ParseIP(resolved)); err != nil {
			return err
		}
		if control != nil {
			return control(network, address, conn)
		}
		return nil
	}
	var lastErr error
	for _, addr := range addrs {
		conn, err := guarded.DialContext(ctx, network, net.JoinHostPort(addr.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func DialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return Default().DialContext(ctx, dialer, network, address)
	}
}

type guardedTransport struct{ base http.RoundTripper }

func WrapTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = NewTransport()
	}
	return &guardedTransport{base: base}
}

func (t *guardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := Default().ValidateURL(req.Context(), req.URL); err != nil {
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
