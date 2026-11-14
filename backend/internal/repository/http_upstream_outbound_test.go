package repository

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/outbound"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

func TestHTTPUpstreamOutboundPolicyWithoutHostAllowlist(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, outbound.Configure([]string{"127.0.0.0/8", "::1/128"})) })
	var calls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()
	parsed, err := url.Parse(redirector.URL)
	require.NoError(t, err)
	cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false, AllowPrivateHosts: true}}}
	upstream := NewHTTPUpstream(cfg)
	for _, fingerprint := range []bool{false, true} {
		t.Run(map[bool]string{false: "standard", true: "fingerprint"}[fingerprint], func(t *testing.T) {
			do := func(raw string) (*http.Response, error) {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, raw, nil)
				require.NoError(t, err)
				if fingerprint {
					return upstream.DoWithTLS(req, "", 1, 1, &tlsfingerprint.Profile{Name: "test"})
				}
				return upstream.Do(req, "", 1, 1)
			}
			require.NoError(t, outbound.Configure(nil))
			blockedURL, err := url.Parse(target.URL)
			require.NoError(t, err)
			if fingerprint {
				blockedURL.Scheme = "https"
			}
			resp, err := do(blockedURL.String())
			require.ErrorContains(t, err, "not allowed")
			require.Nil(t, resp)
			require.NoError(t, outbound.Configure([]string{parsed.Host}))
			resp, err = do(redirector.URL)
			require.ErrorContains(t, err, "not allowed")
			if resp != nil {
				require.NoError(t, resp.Body.Close())
			}
		})
	}
	require.Zero(t, calls.Load())
	parsed, err = url.Parse(target.URL)
	require.NoError(t, err)
	require.NoError(t, outbound.Configure([]string{parsed.Host}))
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target.URL, nil)
	require.NoError(t, err)
	resp, err := upstream.Do(req, "", 1, 1)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, int64(1), calls.Load())
}
