//go:build unit

package ip

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestProxyPolicyRejectsSpoofedHeadersAndResolvesCDN(t *testing.T) {
	local, err := ParseProxyRanges([]string{"127.0.0.1", "172.19.0.1"})
	require.NoError(t, err)
	cdn, err := ParseProxyRanges(append(append([]string{}, CloudflareProxyRanges...), "103.79.184.232"))
	require.NoError(t, err)
	for _, test := range []struct{ name, peer, xff, cf, real, want string }{
		{"direct spoof", "198.65.0.92:1234", "192.0.2.1", "192.0.2.1", "192.0.2.1", "198.65.0.92"},
		{"nginx direct spoof", "127.0.0.1:1234", "192.0.2.1, 198.65.0.92", "192.0.2.1", "192.0.2.1", "198.65.0.92"},
		{"cloudflare client chain", "172.19.0.1:1234", "192.0.2.1, 203.0.113.4, 172.69.1.2", "192.0.2.1", "172.69.1.2", "203.0.113.4"},
		{"nginx cloudflare header", "172.19.0.1:1234", "172.69.1.2", "203.0.113.5", "172.69.1.2", "203.0.113.5"},
		{"custom CDN", "127.0.0.1:1234", "203.0.113.6, 103.79.184.232", "192.0.2.1", "103.79.184.232", "203.0.113.6"},
		{"custom CDN cannot vouch for CF header", "127.0.0.1:1234", "103.79.184.232", "192.0.2.1", "103.79.184.232", "103.79.184.232"},
		{"nginx real_ip normalized", "127.0.0.1:1234", "203.0.113.7", "192.0.2.1", "203.0.113.7", "203.0.113.7"},
		{"invalid chain", "127.0.0.1:1234", "192.0.2.1, unknown", "192.0.2.1", "192.0.2.1", "127.0.0.1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			require.NoError(t, router.SetTrustedProxies(nil))
			router.GET("/", func(ctx *gin.Context) {
				SetForwardedIPSettings(ctx, true, nil)
				SetProxyPolicy(ctx, ProxyPolicy{Local: local, CDN: cdn})
				ctx.String(200, GetClientIP(ctx))
			})
			request := httptest.NewRequest("GET", "/", nil)
			request.RemoteAddr = test.peer
			request.Header.Set("X-Forwarded-For", test.xff)
			request.Header.Set("CF-Connecting-IP", test.cf)
			request.Header.Set("X-Real-IP", test.real)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, test.want, response.Body.String())
		})
	}
}
