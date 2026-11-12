package outbound

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPolicyAddressRanges(t *testing.T) {
	p, err := NewPolicy(nil)
	require.NoError(t, err)
	for _, address := range []string{
		"127.0.0.1", "127.255.255.255", "0.0.0.0", "10.1.2.3", "172.16.0.1", "172.31.255.255",
		"192.168.1.1", "169.254.169.254", "100.64.0.1", "100.127.255.255", "224.0.0.1", "255.255.255.255",
		"::", "::1", "fc00::1", "fdff::1", "fe80::1", "ff02::1", "::ffff:127.0.0.1", "::ffff:10.1.2.3",
		"192.0.0.0", "192.0.0.255", "198.18.0.0", "198.19.255.255", "::ffff:198.18.0.1",
		"64:ff9b::7f00:1", "64:ff9b::a00:1", "64:ff9b::a9fe:a9fe", "64:ff9b::c000:1",
		"64:ff9b::c612:1", "2002:7f00:1::", "2002:a9fe:a9fe::", "2002:c612:1::",
	} {
		t.Run(address, func(t *testing.T) {
			require.Error(t, p.checkIP(address, "443", net.ParseIP(address)))
		})
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "100.63.255.255", "100.128.0.1", "172.15.255.255", "172.32.0.1", "2606:4700:4700::1111", "192.0.1.0", "198.17.255.255", "198.20.0.0", "64:ff9b::808:808", "2002:808:808::"} {
		require.NoError(t, p.checkIP(address, "8787", net.ParseIP(address)))
	}
}

func TestPolicyPreflightCachesAllowedHostPort(t *testing.T) {
	p, err := NewPolicy(nil)
	require.NoError(t, err)
	lookups := 0
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		lookups++
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}
	target, err := url.Parse("https://upstream.example")
	require.NoError(t, err)
	for range 1000 {
		require.NoError(t, p.ValidateURL(t.Context(), target))
	}
	require.Equal(t, 1, lookups, "a warm preflight must not repeat DNS lookups")
	target.Host = "upstream.example:8443"
	require.NoError(t, p.ValidateURL(t.Context(), target))
	require.Equal(t, 2, lookups, "allow decisions are scoped to host and port")
}

func TestPolicyPreflightLookupFailuresAreNotCached(t *testing.T) {
	p, err := NewPolicy(nil)
	require.NoError(t, err)
	lookups := 0
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		lookups++
		if lookups < 3 {
			return nil, errors.New("DNS unavailable")
		}
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}
	target, err := url.Parse("https://upstream.example")
	require.NoError(t, err)
	require.Error(t, p.ValidateURL(t.Context(), target))
	require.Error(t, p.ValidateURL(t.Context(), target))
	require.NoError(t, p.ValidateURL(t.Context(), target))
	require.NoError(t, p.ValidateURL(t.Context(), target))
	require.Equal(t, 3, lookups)
}

func TestPolicyPreflightExpiryAndCapacity(t *testing.T) {
	p, err := NewPolicy(nil)
	require.NoError(t, err)
	now := time.Now()
	p.now = func() time.Time { return now }
	lookups := 0
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		lookups++
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}
	target, err := url.Parse("https://upstream.example")
	require.NoError(t, err)
	require.NoError(t, p.ValidateURL(t.Context(), target))
	now = now.Add(preflightTTL - time.Millisecond)
	require.NoError(t, p.ValidateURL(t.Context(), target))
	require.Equal(t, 1, lookups)
	now = now.Add(time.Millisecond)
	require.NoError(t, p.ValidateURL(t.Context(), target))
	require.Equal(t, 2, lookups)
	for i := range maxPreflightHosts + 10 {
		target.Host = fmt.Sprintf("upstream-%d.example", i)
		require.NoError(t, p.ValidateURL(t.Context(), target))
	}
	require.Len(t, p.allowedHosts, maxPreflightHosts)
}

func TestPolicyPreflightNeverAuthorizesNewConnection(t *testing.T) {
	p, err := NewPolicy(nil)
	require.NoError(t, err)
	lookups := 0
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		lookups++
		if lookups == 1 {
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	target, err := url.Parse("https://upstream.example")
	require.NoError(t, err)
	require.NoError(t, p.ValidateURL(t.Context(), target))
	require.NoError(t, p.ValidateURL(t.Context(), target))
	dns := newTestDNS(t, "127.0.0.1")
	_, err = p.DialContext(t.Context(), &net.Dialer{Timeout: time.Second, Resolver: dns.resolver}, "tcp", "upstream.example:443")
	require.ErrorContains(t, err, "not allowed")
	require.Equal(t, 1, lookups, "native dial resolution must not use the preflight lookup hook")
}

func TestPolicyRequestResolutionFailureByRoute(t *testing.T) {
	p, err := NewPolicy(nil)
	require.NoError(t, err)
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return nil, errors.New("DNS unavailable")
	}
	target, err := url.Parse("https://upstream.example")
	require.NoError(t, err)
	require.NoError(t, p.ValidateRequest(t.Context(), target, false))
	require.Error(t, p.ValidateRequest(t.Context(), target, true))
	dns := newTestDNS(t)
	dns.failed.Store(true)
	_, err = p.DialContext(t.Context(), &net.Dialer{Timeout: time.Second, Resolver: dns.resolver}, "tcp", "upstream.example:443")
	require.Error(t, err)
	target.Host = "127.0.0.1"
	require.ErrorContains(t, p.ValidateRequest(t.Context(), target, false), "not allowed")
	target.Host = "upstream.example"
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, p.ValidateRequest(cancelled, target, false))
}

func TestTransportReusesDirectConnectionDuringResolverFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()
	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	p, err := NewPolicy([]string{target.Host})
	require.NoError(t, err)
	now := time.Now()
	p.now = func() time.Time { return now }
	lookups := 0
	failed := false
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		lookups++
		if failed {
			return nil, errors.New("DNS unavailable")
		}
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	dns := newTestDNS(t, "127.0.0.1")
	previous := current.Swap(p)
	t.Cleanup(func() { current.Store(previous) })
	transport := NewTransport()
	transport.Proxy = nil
	transport.DialContext = DialContext(&net.Dialer{Timeout: 30 * time.Second, Resolver: dns.resolver})
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: WrapTransport(transport), Timeout: time.Second}
	target.Host = net.JoinHostPort("upstream.example", target.Port())
	fetch := func() {
		resp, err := client.Get(target.String())
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	fetch()
	require.Equal(t, 1, lookups, "preflight and native dial use separate resolver hooks")
	for range 100 {
		fetch()
	}
	require.Equal(t, 1, lookups, "pooled requests perform no preflight DNS lookups")
	now = now.Add(preflightTTL)
	failed = true
	dns.failed.Store(true)
	fetch()
	require.Equal(t, 2, lookups, "DNS preflight failure must not discard a usable connection")
	transport.CloseIdleConnections()
	_, err = client.Get(target.String())
	require.Error(t, err)
}

func TestPolicyPreflightDoesNotCacheBlockedAddress(t *testing.T) {
	p, err := NewPolicy(nil)
	require.NoError(t, err)
	lookups := 0
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		lookups++
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	target, err := url.Parse("https://upstream.example")
	require.NoError(t, err)
	for range 2 {
		require.ErrorContains(t, p.ValidateURL(t.Context(), target), "not allowed")
	}
	require.Equal(t, 2, lookups)
}

func BenchmarkPolicyPreflightWarm(b *testing.B) {
	p, err := NewPolicy(nil)
	if err != nil {
		b.Fatal(err)
	}
	lookups := 0
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		lookups++
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}
	target, err := url.Parse("https://upstream.example")
	if err != nil {
		b.Fatal(err)
	}
	if err := p.ValidateURL(b.Context(), target); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		if err := p.ValidateURL(b.Context(), target); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(lookups-1)/float64(b.N), "lookups/op")
	if lookups != 1 {
		b.Fatalf("warm preflight performed %d DNS lookups", lookups)
	}
}

func TestPolicyExceptions(t *testing.T) {
	p, err := NewPolicy([]string{"172.18.0.1:8787", "10.1.0.0/16", "[::1]:8443", "private.example:443"})
	require.NoError(t, err)
	require.NoError(t, p.checkIP("172.18.0.1", "8787", net.ParseIP("172.18.0.1")))
	require.Error(t, p.checkIP("172.18.0.1", "8080", net.ParseIP("172.18.0.1")))
	require.NoError(t, p.checkIP("internal.example", "9000", net.ParseIP("10.1.2.3")))
	require.Error(t, p.checkIP("internal.example", "9000", net.ParseIP("10.2.2.3")))
	require.NoError(t, p.checkIP("::1", "8443", net.ParseIP("::1")))
	require.NoError(t, p.checkIP("PRIVATE.EXAMPLE.", "443", net.ParseIP("192.168.1.1")))
	require.Error(t, p.checkIP("private.example", "80", net.ParseIP("192.168.1.1")))
	for _, entry := range []string{"127.0.0.1", "https://localhost:443", "localhost:0", "localhost:65536", "localhost:bad", "10.0.0.0/99", "[::1%lo0]:443"} {
		_, err := NewPolicy([]string{entry})
		require.Error(t, err, entry)
	}
}

func TestPolicyChecksEveryResolvedAddressAndConnection(t *testing.T) {
	p, err := NewPolicy(nil)
	require.NoError(t, err)
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("127.0.0.1")}}, nil
	}
	target, err := url.Parse("https://upstream.example")
	require.NoError(t, err)
	require.ErrorContains(t, p.ValidateURL(context.Background(), target), "not allowed")
	dns := newTestDNS(t)
	dns.failed.Store(true)
	_, err = p.DialContext(context.Background(), &net.Dialer{Timeout: time.Second, Resolver: dns.resolver}, "tcp", "upstream.example:443")
	require.Error(t, err)
}

func TestHTTPClientRejectsPrivateTargetsAndRedirects(t *testing.T) {
	require.NoError(t, Configure(nil))
	t.Cleanup(func() { require.NoError(t, Configure(nil)) })
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("blocked target reached") }))
	defer target.Close()
	client := NewClient(time.Second)
	_, err := client.Get(target.URL)
	require.ErrorContains(t, err, "not allowed")
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	u, err := url.Parse(redirect.URL)
	require.NoError(t, err)
	require.NoError(t, Configure([]string{u.Host}))
	_, err = client.Get(redirect.URL)
	require.ErrorContains(t, err, "not allowed")
}

func TestHTTPClientPrivateExceptionAndPinnedDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer server.Close()
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	p, err := NewPolicy([]string{u.Host})
	require.NoError(t, err)
	lookups := 0
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		lookups++
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	dns := newTestDNS(t, "127.0.0.1")
	conn, err := p.DialContext(context.Background(), &net.Dialer{Timeout: time.Second, Resolver: dns.resolver}, "tcp", net.JoinHostPort("upstream.example", u.Port()))
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	require.Equal(t, 0, lookups, "native dial uses the transport resolver, not the preflight hook")
	previous := current.Swap(p)
	t.Cleanup(func() { current.Store(previous) })
	transport := NewTransport()
	transport.Proxy = nil
	transport.DialContext = DialContext(&net.Dialer{Timeout: time.Second, Resolver: dns.resolver})
	defer transport.CloseIdleConnections()
	targetURL := "http://upstream.example:" + u.Port()
	resp, err := (&http.Client{Transport: WrapTransport(transport), Timeout: time.Second}).Get(targetURL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "ok", string(body))
}

func TestRequestTargetValidationPrecedesProxy(t *testing.T) {
	require.NoError(t, Configure(nil))
	t.Cleanup(func() { require.NoError(t, Configure(nil)) })
	for _, scheme := range []string{"http", "https", "socks5", "socks5h"} {
		t.Run(scheme, func(t *testing.T) {
			transport := NewTransport()
			proxyURL, err := url.Parse(scheme + "://127.0.0.1:1080")
			require.NoError(t, err)
			transport.Proxy = http.ProxyURL(proxyURL)
			client := &http.Client{Transport: WrapTransport(transport), Timeout: time.Second}
			_, err = client.Get("https://10.0.0.1")
			require.ErrorContains(t, err, "not allowed")
		})
	}
}

func TestLiteralURLChecks(t *testing.T) {
	require.NoError(t, Configure(nil))
	for _, raw := range []string{"http://127.0.0.1:8080", "https://localhost", "http://[::ffff:127.0.0.1]", "http://[fe80::1%25lo0]", "file:///etc/hosts", "http://user:password@8.8.8.8"} {
		err := ValidateLiteralURL(raw)
		require.Error(t, err, raw)
		require.False(t, strings.Contains(err.Error(), "password"))
	}
	require.NoError(t, ValidateLiteralURL("http://8.8.8.8:8787"))
}
