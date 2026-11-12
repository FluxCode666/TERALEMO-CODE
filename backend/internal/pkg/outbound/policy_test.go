package outbound

import (
	"context"
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
	} {
		t.Run(address, func(t *testing.T) {
			require.Error(t, p.checkIP(address, "443", net.ParseIP(address)))
		})
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "100.63.255.255", "100.128.0.1", "172.15.255.255", "172.32.0.1", "2606:4700:4700::1111"} {
		require.NoError(t, p.checkIP(address, "8787", net.ParseIP(address)))
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
	_, err = p.DialContext(context.Background(), &net.Dialer{Timeout: time.Second}, "tcp", "upstream.example:443")
	require.ErrorContains(t, err, "not allowed")
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
	conn, err := p.DialContext(context.Background(), &net.Dialer{Timeout: time.Second}, "tcp", net.JoinHostPort("upstream.example", u.Port()))
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	require.Equal(t, 1, lookups)
	previous := current.Swap(p)
	t.Cleanup(func() { current.Store(previous) })
	resp, err := NewClient(time.Second).Get(server.URL)
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
