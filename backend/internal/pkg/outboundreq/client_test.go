package outboundreq

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/outbound"
	"github.com/stretchr/testify/require"
)

func TestReqClientDestinationPolicy(t *testing.T) {
	require.NoError(t, outbound.Configure(nil))
	t.Cleanup(func() { require.NoError(t, outbound.Configure(nil)) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := New()
	_, err := client.R().Get(server.URL)
	require.ErrorContains(t, err, "not allowed")
	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	require.NoError(t, outbound.Configure([]string{parsed.Host}))
	resp, err := client.R().Get(server.URL)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
}
