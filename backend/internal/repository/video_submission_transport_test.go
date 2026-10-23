//go:build unit

package repository

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type fingerprintSubmissionUpstream struct{ service.HTTPUpstream }

func (u fingerprintSubmissionUpstream) Do(req *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	return u.DoWithTLS(req, proxy, accountID, concurrency, &tlsfingerprint.Profile{Name: "video-local-test"})
}

func forwardTransportSeedance(t *testing.T, cfg *config.Config, upstream service.HTTPUpstream, ctx context.Context, base string) error {
	t.Helper()
	svc := service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, nil, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
	account := &service.Account{ID: 1, Platform: service.PlatformVideo, Type: service.AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "local-test-key", "base_url": base, "video_protocol": service.VideoProtocolSeedance}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", nil).WithContext(ctx)
	_, err := svc.ForwardSeedance(ctx, c, account, service.SeedanceEndpointCreate, "",
		[]byte(`{"model":"doubao-seedance-1-0-pro-250528","content":[{"type":"text","text":"waves"}],"duration":6,"resolution":"720p"}`))
	return err
}

func TestVideoSubmissionTransportTLSFingerprintUnsent(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(srv.Close)
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	upstream := NewHTTPUpstream(cfg).(*httpUpstreamService)
	t.Cleanup(func() {
		for _, entry := range upstream.clients {
			entry.client.CloseIdleConnections()
		}
	})
	ctx, writes := service.WithVideoSubmissionWrites(context.Background())
	err := forwardTransportSeedance(t, cfg, fingerprintSubmissionUpstream{upstream}, ctx, srv.URL)
	// Exercise the real custom DialTLSContext and certificate verification,
	// rather than replacing the transport or synthesizing a TLS error string.
	require.ErrorContains(t, err, "certificate")
	require.True(t, writes.DefinitelyNotSent())
	require.False(t, writes.ReceivedResponse())
	require.Zero(t, calls.Load())
	requireNoUpstreamInFlight(t, upstream)
}

func TestVideoSubmissionTransportUnknownRoundTripperIsUncertain(t *testing.T) {
	cfg := &config.Config{}
	upstream := NewHTTPUpstream(cfg).(*httpUpstreamService)
	entry, err := upstream.getClientEntry("", 1, 1, service.HTTPUpstreamProfileDefault, false, false)
	require.NoError(t, err)
	entry.client.CloseIdleConnections()
	entry.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		// A plugin can fail after sending a request without exposing trace hooks.
		return nil, errors.New("custom transport failed")
	})
	ctx, writes := service.WithVideoSubmissionWrites(context.Background())
	err = forwardTransportSeedance(t, cfg, upstream, ctx, "https://video.example.test")
	require.ErrorContains(t, err, "custom transport failed")
	require.False(t, writes.DefinitelyNotSent(), "absence of hooks from an unknown transport is not proof")
	require.False(t, writes.ReceivedResponse())
	requireNoUpstreamInFlight(t, upstream)
}

func TestVideoSubmissionTransportHTTP2PreservesWriteEvidence(t *testing.T) {
	for _, fault := range []string{"success", "reset-after-read"} {
		t.Run(fault, func(t *testing.T) {
			var proto atomic.Int32
			var calls atomic.Int32
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				proto.Store(int32(r.ProtoMajor))
				calls.Add(1)
				if fault == "reset-after-read" {
					panic(http.ErrAbortHandler)
				}
				_, _ = io.WriteString(w, `{"id":"http2-video-task"}`)
			}))
			srv.EnableHTTP2 = true
			srv.StartTLS()
			t.Cleanup(srv.Close)
			cfg := &config.Config{}
			cfg.Security.URLAllowlist.AllowPrivateHosts = true
			upstream := NewHTTPUpstream(cfg).(*httpUpstreamService)
			entry, err := upstream.getClientEntry("", 1, 1, service.HTTPUpstreamProfileLongStream, false, false)
			require.NoError(t, err)
			tr := entry.client.Transport.(*http.Transport)
			t.Cleanup(tr.CloseIdleConnections)
			roots := x509.NewCertPool()
			roots.AddCert(srv.Certificate())
			if tr.TLSClientConfig == nil {
				tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
			}
			tr.TLSClientConfig.RootCAs = roots
			ctx := service.WithHTTPUpstreamProfile(context.Background(), service.HTTPUpstreamProfileLongStream)
			ctx, writes := service.WithVideoSubmissionWrites(ctx)
			err = forwardTransportSeedance(t, cfg, upstream, ctx, srv.URL)
			if fault == "success" {
				require.NoError(t, err)
				require.True(t, writes.ReceivedResponse())
			} else {
				require.Error(t, err)
				require.True(t, strings.Contains(err.Error(), "stream") || strings.Contains(err.Error(), "INTERNAL_ERROR"), err.Error())
				require.False(t, writes.ReceivedResponse())
			}
			require.Equal(t, int32(2), proto.Load(), "must negotiate actual HTTP/2")
			require.Equal(t, int32(1), calls.Load(), "POST must not be replayed")
			require.False(t, writes.DefinitelyNotSent(), "HTTP/2 write evidence must survive stream errors")
			requireNoUpstreamInFlight(t, upstream)
		})
	}
}
