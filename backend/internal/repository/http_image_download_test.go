//go:build unit

package repository

import (
	"context"
	"errors"
	"net"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestImagePlatformDownloadPinsPublicIP(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "::1", "fc00::1", "0.0.0.0", "8.8.8.8"} {
		t.Run(address, func(t *testing.T) {
			lookups, dials := 0, 0
			lookup := func(context.Context, string, string) ([]net.IP, error) {
				lookups++
				return []net.IP{net.ParseIP(address)}, nil
			}
			dial := func(_ context.Context, _, target string) (net.Conn, error) {
				dials++
				require.Equal(t, "8.8.8.8:443", target, "the verified IP must be dialed without another DNS lookup")
				return nil, errors.New("fixture dial")
			}
			_, err := publicImageDialContext(lookup, dial)(context.Background(), "tcp", "rebind.example:443")
			require.Error(t, err)
			require.Equal(t, 1, lookups)
			if address == "8.8.8.8" {
				require.Equal(t, 1, dials)
			} else {
				require.Zero(t, dials)
			}
		})
	}
}

func TestImagePlatformDownloadRejectsProxyAndLiteralPrivate(t *testing.T) {
	svc := &httpUpstreamService{}
	req := httptest.NewRequest("GET", "https://127.0.0.1/private", nil)
	req.RequestURI = ""
	req = req.WithContext(service.WithHTTPUpstreamImageDownload(req.Context()))
	_, err := svc.Do(req, "http://proxy.example:8080", 1, 1)
	require.ErrorContains(t, err, "proxies")
	_, err = svc.Do(req, "", 1, 1)
	require.ErrorContains(t, err, "not public")
	require.False(t, service.HTTPUpstreamImageDownload(service.WithHTTPUpstreamPublicHostsOnly(context.Background())), "old public-only downloads retain their transport and proxy behavior")
}
