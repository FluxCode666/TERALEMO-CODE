// Package outboundreq applies the outbound destination policy to req clients.
package outboundreq

import (
	"net"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/outbound"
	"github.com/imroc/req/v3"
)

func New() *req.Client { return Guard(req.C()) }

func Guard(client *req.Client) *req.Client {
	client.SetDial(outbound.DialContext(&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}))
	client.GetTransport().WrapRoundTripFunc(func(rt http.RoundTripper) req.HttpRoundTripFunc {
		return outbound.WrapTransport(rt).RoundTrip
	})
	return client
}
