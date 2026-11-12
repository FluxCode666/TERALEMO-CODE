package outbound

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"
)

type testDNS struct {
	resolver *net.Resolver
	queries  atomic.Int64
	failed   atomic.Bool
}

func newTestDNS(t *testing.T, addresses ...string) *testDNS {
	t.Helper()
	server, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	dns := &testDNS{}
	dns.resolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		if dns.failed.Load() {
			return nil, errors.New("DNS unavailable")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.LocalAddr().String())
	}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, peer, err := server.ReadFrom(buf)
			if err != nil {
				return
			}
			var query dnsmessage.Message
			if err := query.Unpack(buf[:n]); err != nil || len(query.Questions) != 1 {
				continue
			}
			dns.queries.Add(1)
			question := query.Questions[0]
			reply := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: query.ID, Response: true, Authoritative: true, RecursionAvailable: true},
				Questions: query.Questions,
			}
			for _, address := range addresses {
				ip := net.ParseIP(address)
				header := dnsmessage.ResourceHeader{Name: question.Name, Type: question.Type, Class: dnsmessage.ClassINET, TTL: 1}
				if v4 := ip.To4(); v4 != nil && question.Type == dnsmessage.TypeA {
					reply.Answers = append(reply.Answers, dnsmessage.Resource{Header: header, Body: &dnsmessage.AResource{A: [4]byte(v4)}})
				} else if v6 := ip.To16(); v4 == nil && v6 != nil && question.Type == dnsmessage.TypeAAAA {
					reply.Answers = append(reply.Answers, dnsmessage.Resource{Header: header, Body: &dnsmessage.AAAAResource{AAAA: [16]byte(v6)}})
				}
			}
			if response, err := reply.Pack(); err == nil {
				_, _ = server.WriteTo(response, peer)
			}
		}
	}()
	t.Cleanup(func() {
		_ = server.Close()
		<-done
	})
	return dns
}

func TestPolicyDialUsesOneTimeoutBudget(t *testing.T) {
	dns := newTestDNS(t, "127.0.0.2", "127.0.0.3", "127.0.0.1")
	p, err := NewPolicy([]string{"127.0.0.0/8"})
	require.NoError(t, err)
	p.lookup = dns.resolver.LookupIPAddr
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	dialer := &net.Dialer{Timeout: 120 * time.Millisecond, Resolver: dns.resolver,
		ControlContext: func(ctx context.Context, _, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if host != "127.0.0.1" {
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		},
	}
	started := time.Now()
	conn, err := p.DialContext(t.Context(), dialer, "tcp4", net.JoinHostPort("upstream.example", port))
	if conn != nil {
		_ = conn.Close()
	}
	_ = err
	require.Less(t, time.Since(started), 220*time.Millisecond)
}

func TestPolicyDialFallsBackAcrossAddresses(t *testing.T) {
	dns := newTestDNS(t, "127.0.0.2", "127.0.0.1")
	p, err := NewPolicy([]string{"127.0.0.0/8"})
	require.NoError(t, err)
	p.lookup = dns.resolver.LookupIPAddr
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	var attempts atomic.Int64
	dialer := &net.Dialer{Timeout: time.Second, Resolver: dns.resolver,
		ControlContext: func(_ context.Context, _, address string, _ syscall.RawConn) error {
			attempts.Add(1)
			if address == net.JoinHostPort("127.0.0.2", port) {
				return errors.New("address unavailable")
			}
			return nil
		},
	}
	conn, err := p.DialContext(t.Context(), dialer, "tcp4", net.JoinHostPort("upstream.example", port))
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	require.EqualValues(t, 2, attempts.Load())
}

func TestPolicyDialPreservesDualStackFallback(t *testing.T) {
	dns := newTestDNS(t, "::1", "127.0.0.1")
	p, err := NewPolicy([]string{"127.0.0.0/8", "::1/128"})
	require.NoError(t, err)
	p.lookup = dns.resolver.LookupIPAddr
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	var v6Started atomic.Bool
	dialer := &net.Dialer{Timeout: 500 * time.Millisecond, FallbackDelay: 10 * time.Millisecond, Resolver: dns.resolver,
		ControlContext: func(ctx context.Context, network, _ string, _ syscall.RawConn) error {
			if network == "tcp6" {
				v6Started.Store(true)
				timer := time.NewTimer(20 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-timer.C:
					return errors.New("ipv6 unavailable")
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		},
	}
	started := time.Now()
	conn, err := p.DialContext(t.Context(), dialer, "tcp", net.JoinHostPort("upstream.example", port))
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	require.True(t, v6Started.Load())
	require.Less(t, time.Since(started), 300*time.Millisecond)
}

func TestPolicyDialChecksBeforeExistingControlHooks(t *testing.T) {
	p, err := NewPolicy(nil)
	require.NoError(t, err)
	for _, contextHook := range []bool{false, true} {
		called := false
		dialer := &net.Dialer{Timeout: time.Second}
		if contextHook {
			dialer.ControlContext = func(context.Context, string, string, syscall.RawConn) error { called = true; return nil }
		} else {
			dialer.Control = func(string, string, syscall.RawConn) error { called = true; return nil }
		}
		_, err := p.DialContext(t.Context(), dialer, "tcp", "127.0.0.1:443")
		require.ErrorContains(t, err, "not allowed")
		require.False(t, called)
	}
}
