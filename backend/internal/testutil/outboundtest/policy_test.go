package outboundtest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/outbound"
	"github.com/stretchr/testify/require"
)

func TestAllowLoopbackRequiresExplicitConfiguration(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, outbound.Configure(nil)) })
	for _, target := range []string{"http://127.0.0.1", "http://[::1]"} {
		require.ErrorContains(t, outbound.ValidateLiteralURL(target), "not allowed")
	}
	AllowLoopback()
	for _, target := range []string{"http://127.0.0.1", "http://[::1]"} {
		require.NoError(t, outbound.ValidateLiteralURL(target))
	}
	for _, target := range []string{"http://10.0.0.1", "http://169.254.169.254", "http://[fc00::1]"} {
		require.ErrorContains(t, outbound.ValidateLiteralURL(target), "not allowed")
	}
}

func TestAllowLoopbackSupportsHTTPFixtures(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, outbound.Configure(nil)) })
	AllowLoopback()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := outbound.NewClient(time.Second)
	defer client.CloseIdleConnections()
	response, err := client.Get(server.URL)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusNoContent, response.StatusCode)
}
