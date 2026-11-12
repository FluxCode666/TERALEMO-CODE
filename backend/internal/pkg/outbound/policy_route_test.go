package outbound

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPolicyProxyPreflightDoesNotUseDirectCache(t *testing.T) {
	for _, result := range []string{"public", "private", "unavailable"} {
		t.Run(result, func(t *testing.T) {
			p, err := NewPolicy(nil)
			require.NoError(t, err)
			lookups := 0
			p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
				lookups++
				if lookups > 1 {
					switch result {
					case "private":
						return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
					case "unavailable":
						return nil, errors.New("DNS unavailable")
					}
				}
				return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
			}
			target, err := url.Parse("https://upstream.example")
			require.NoError(t, err)
			require.NoError(t, p.ValidateRequest(t.Context(), target, false))
			require.Len(t, p.allowedHosts, 1)
			expires := p.allowedHosts[target.Host+":443"]
			for range 2 {
				err := p.ValidateRequest(t.Context(), target, true)
				if result == "public" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			}
			require.Equal(t, 3, lookups)
			require.Equal(t, expires, p.allowedHosts[target.Host+":443"], "proxy validation must not refresh a direct allow entry")
		})
	}
}

func TestPolicyURLAndProxyPreflightDoNotPopulateCache(t *testing.T) {
	for _, route := range []string{"proxy", "unspecified"} {
		t.Run(route, func(t *testing.T) {
			p, err := NewPolicy(nil)
			require.NoError(t, err)
			lookups := 0
			p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
				lookups++
				return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
			}
			target, err := url.Parse("https://upstream.example")
			require.NoError(t, err)
			for range 2 {
				if route == "proxy" {
					require.NoError(t, p.ValidateRequest(t.Context(), target, true))
				} else {
					require.NoError(t, p.ValidateURL(t.Context(), target))
				}
			}
			require.Equal(t, 2, lookups)
			require.Empty(t, p.allowedHosts)
		})
	}
}

type routeTestTransport func(*http.Request) (*http.Response, error)

func (rt routeTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return rt(req)
}

func TestUnknownTransportRouteValidatesEveryRequest(t *testing.T) {
	p, err := NewPolicy(nil)
	require.NoError(t, err)
	lookups := 0
	p.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		lookups++
		if lookups > 1 {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}
	previous := current.Swap(p)
	t.Cleanup(func() { current.Store(previous) })
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://upstream.example", nil)
	require.NoError(t, err)
	require.NoError(t, p.ValidateRequest(t.Context(), req.URL, false))
	transport := WrapTransport(routeTestTransport(func(*http.Request) (*http.Response, error) {
		t.Error("a rejected target must not reach the wrapped transport")
		return nil, errors.New("unexpected request")
	}))
	_, err = transport.RoundTrip(req)
	require.ErrorContains(t, err, "not allowed")
	require.Equal(t, 2, lookups)
}

func TestProxyTransportRevalidatesTargetWithDirectCache(t *testing.T) {
	var targetRequests, proxyRequests atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		_, _ = io.WriteString(w, "target")
	}))
	defer target.Close()
	privateURL, err := url.Parse(target.URL)
	require.NoError(t, err)
	targetURL, err := url.Parse("http://upstream.example:" + privateURL.Port())
	require.NoError(t, err)
	var private atomic.Bool
	resolve := func(context.Context, string) ([]net.IPAddr, error) {
		address := "8.8.8.8"
		if private.Load() {
			address = "127.0.0.1"
		}
		return []net.IPAddr{{IP: net.ParseIP(address)}}, nil
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests.Add(1)
		addresses, err := resolve(r.Context(), r.URL.Hostname())
		if err != nil {
			http.Error(w, "resolution failed", http.StatusBadGateway)
			return
		}
		if !addresses[0].IP.IsLoopback() {
			_, _ = io.WriteString(w, "public")
			return
		}
		copyURL := *r.URL
		copyURL.Host = net.JoinHostPort(addresses[0].IP.String(), r.URL.Port())
		req, err := http.NewRequestWithContext(r.Context(), r.Method, copyURL.String(), nil)
		if err != nil {
			http.Error(w, "request failed", http.StatusBadGateway)
			return
		}
		transport := &http.Transport{Proxy: nil}
		defer transport.CloseIdleConnections()
		resp, err := transport.RoundTrip(req)
		if err != nil {
			http.Error(w, "upstream failed", http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.Copy(w, resp.Body)
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	p, err := NewPolicy([]string{proxyURL.Host})
	require.NoError(t, err)
	p.lookup = resolve
	previous := current.Swap(p)
	t.Cleanup(func() { current.Store(previous) })
	require.NoError(t, p.ValidateRequest(t.Context(), targetURL, false))
	transport := NewTransport()
	transport.Proxy = http.ProxyURL(proxyURL)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: WrapTransport(transport), Timeout: time.Second}
	resp, err := client.Get(targetURL.String())
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, "public", string(body))
	private.Store(true)
	transport.CloseIdleConnections()
	resp, err = client.Get(targetURL.String())
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.ErrorContains(t, err, "not allowed")
	require.EqualValues(t, 1, proxyRequests.Load())
	require.Zero(t, targetRequests.Load())
}
