//go:build unit

package handler

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Use the real repository HTTP client, with actual sockets/TLS/DNS. Only the
// billing SQL ledger is the existing handler state-transition test double.
func transportVideoHandler(t *testing.T, protocol, base string, mutate func(*service.Account)) (*OpenAIGatewayHandler, *grokMediaSlotsCache, *videoSubmissionReservationRepo) {
	t.Helper()
	platform, accountProtocol := service.PlatformVideo, protocol
	if protocol == "grok" {
		platform = service.PlatformGrok
	}
	if strings.HasPrefix(protocol, "minimax-") {
		accountProtocol = service.VideoProtocolMiniMax
	}
	h, slots, bindings, _ := newGrokMediaSlotHandler(t, false, false, platform, accountProtocol, base)
	h.cfg.RunMode = "standard"
	h.cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	h.cfg.Security.URLAllowlist.AllowPrivateHosts = true
	h.cfg.Gateway.ResponseHeaderTimeout = 1
	h.cfg.Gateway.GrokResponseHeaderTimeout = 1
	h.billingCacheService = service.NewBillingCacheService(videoBalanceBoundaryCache{balance: 1}, nil, nil, nil, nil, nil, h.cfg, nil)
	t.Cleanup(h.billingCacheService.Stop)
	account := service.Account{ID: 1, Platform: platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive,
		Schedulable: true, Concurrency: 10, GroupIDs: []int64{24}, Credentials: map[string]any{
			"api_key": "local-transport-test-key", "base_url": base, "video_protocol": accountProtocol,
		}}
	if mutate != nil {
		mutate(&account)
	}
	accountRepo := grokMediaSlotRepo{openAIImagesFailoverAccountRepo: openAIImagesFailoverAccountRepo{accounts: []service.Account{account}}}
	concurrency := service.NewConcurrencyService(slots)
	gateway := service.NewOpenAIGatewayService(accountRepo, nil, nil, nil, nil, nil, bindings, h.cfg, nil, concurrency,
		service.NewBillingService(h.cfg, nil), nil, nil, repository.NewHTTPUpstream(h.cfg), service.NewDeferredService(nil, nil, time.Second),
		nil, service.NewGrokTokenProvider(accountRepo, nil), nil, nil, nil, nil, nil)
	gateway.SetVideoBillingReservationRepository(bindings.reservations)
	h.gatewayService = gateway
	return h, slots, bindings.reservations
}

func submitTransportVideo(t *testing.T, h *OpenAIGatewayHandler, protocol string, contexts ...context.Context) *httptest.ResponseRecorder {
	t.Helper()
	return submitTransportVideoAtPrice(t, h, protocol, .2, contexts...)
}

func submitTransportVideoAtPrice(t *testing.T, h *OpenAIGatewayHandler, protocol string, price float64, contexts ...context.Context) *httptest.ResponseRecorder {
	t.Helper()
	ctx := context.Background()
	if len(contexts) > 0 {
		ctx = contexts[0]
	}
	c, w := grokMediaSlotContext(ctx, true)
	key, _ := middleware.GetAPIKeyFromContext(c)
	key.User.Balance = 1
	key.Group.VideoBillingMode = "per_request"
	key.Group.VideoPricePerRequest = &price
	path, body := "/v1/videos/generations", `{"model":"grok-imagine-video","prompt":"waves","duration":6}`
	key.Group.Platform = service.PlatformVideo
	switch protocol {
	case "grok":
		key.Group.Platform = service.PlatformGrok
		key.Group.ModelPricing = []service.ChannelModelPricing{{Models: []string{"grok-imagine-video"}, BillingMode: service.BillingModePerRequest, PerRequestPrice: &price}}
	case service.VideoProtocolSeedance:
		path, body = EndpointSeedanceTasks, `{"model":"doubao-seedance-1-0-pro-250528","content":[{"type":"text","text":"waves"}],"duration":6}`
	case service.VideoProtocolGemini:
		path, body = "/v1beta/models/veo:predictLongRunning", `{"instances":[{"prompt":"waves"}],"parameters":{"durationSeconds":8,"resolution":"720p"}}`
		c.Params = gin.Params{{Key: "modelAction", Value: "/veo:predictLongRunning"}}
	case "minimax-v1":
		path, body = "/v1/video_generation", `{"model":"MiniMax-Hailuo-2.3","prompt":"waves","duration":6,"resolution":"768P"}`
	case "minimax-v2":
		path, body = "/v2/video_generation", `{"model":"MiniMax-H3","content":[{"type":"text","text":"waves"}],"duration":6,"resolution":"768P","ratio":"16:9"}`
	}
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
	c.Request.Header.Set("Content-Type", "application/json")
	switch protocol {
	case "grok":
		h.GrokVideoGeneration(c)
	case service.VideoProtocolSeedance:
		h.SeedanceTasks(c)
	case service.VideoProtocolGemini:
		h.GeminiVideoTasks(c)
	default:
		h.MiniMaxVideoTasks(c)
	}
	return w
}

func transportVideoBase(protocol, origin string) string {
	if protocol == "minimax-v2" {
		return origin + "/v2"
	}
	return origin
}

func requireTransportHold(t *testing.T, repo *videoSubmissionReservationRepo, state string, hold float64) *service.VideoBillingReservation {
	t.Helper()
	require.Len(t, repo.rows, 1)
	for _, v := range repo.rows {
		require.Equal(t, state, v.State)
		require.InDelta(t, .2, v.HoldAmount, 1e-12)
		active := 0.0
		if v.State == service.VideoReservationPreparing || v.State == service.VideoReservationAccepted || v.State == service.VideoReservationManual {
			active = v.HoldAmount
		}
		require.InDelta(t, hold, active, 1e-12)
		t.Logf("state=%s active_hold=$%.2f reason=%s", v.State, active, v.LastError)
		return v
	}
	return nil
}

func TestVideoSubmissionTransportUnsentReleases(t *testing.T) {
	protocols := []string{service.VideoProtocolSeedance, service.VideoProtocolGemini, "minimax-v1", "minimax-v2", "grok"}
	var received atomic.Int32
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { received.Add(1) }))
	t.Cleanup(tlsServer.Close)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	refused := "http://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	for _, protocol := range protocols {
		for _, fault := range []string{"untrusted-tls", "connection-refused"} {
			t.Run(protocol+"/"+fault, func(t *testing.T) {
				origin := tlsServer.URL
				if fault == "connection-refused" {
					origin = refused
				}
				h, slots, ledger := transportVideoHandler(t, protocol, transportVideoBase(protocol, origin), nil)
				w := submitTransportVideo(t, h, protocol)
				require.Equal(t, 502, w.Code, w.Body.String())
				if protocol != "grok" { // Grok may choose its generic safe gateway message.
					require.Contains(t, w.Body.String(), "reservation has been released")
				}
				require.NotContains(t, w.Body.String(), "x509")
				require.NotContains(t, w.Body.String(), "127.0.0.1")
				v := requireTransportHold(t, ledger, service.VideoReservationReleased, 0)
				require.Equal(t, "upstream request not sent", v.LastError)
				slots.assertReleased(t)
			})
		}
	}
	require.Zero(t, received.Load(), "certificate verification must prevent the HTTP request")
	t.Run("dns-invalid-domain", func(t *testing.T) {
		h, slots, ledger := transportVideoHandler(t, "minimax-v2", "https://video-unsent-test.invalid/v2", nil)
		w := submitTransportVideo(t, h, "minimax-v2")
		require.Equal(t, 502, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "reservation has been released")
		requireTransportHold(t, ledger, service.VideoReservationReleased, 0)
		slots.assertReleased(t)
	})
	t.Run("proxy-connect-rejection", func(t *testing.T) {
		var connects atomic.Int32
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodConnect, r.Method)
			connects.Add(1)
			w.WriteHeader(http.StatusProxyAuthRequired)
		}))
		t.Cleanup(proxy.Close)
		u, err := url.Parse(proxy.URL)
		require.NoError(t, err)
		port, err := strconv.Atoi(u.Port())
		require.NoError(t, err)
		h, slots, ledger := transportVideoHandler(t, "minimax-v2", transportVideoBase("minimax-v2", tlsServer.URL), func(a *service.Account) {
			id := int64(1)
			a.ProxyID, a.Proxy = &id, &service.Proxy{ID: id, Protocol: "http", Host: u.Hostname(), Port: port}
		})
		w := submitTransportVideo(t, h, "minimax-v2")
		require.Equal(t, 502, w.Code, w.Body.String())
		require.Equal(t, int32(1), connects.Load())
		requireTransportHold(t, ledger, service.VideoReservationReleased, 0)
		slots.assertReleased(t)
	})
}

type cancelAfterVideoReserve struct {
	service.VideoBillingReservationRepository
	cancel context.CancelFunc
}

func (r cancelAfterVideoReserve) Reserve(ctx context.Context, v *service.VideoBillingReservation) error {
	if err := r.VideoBillingReservationRepository.Reserve(ctx, v); err != nil {
		return err
	}
	r.cancel()
	return nil
}

func TestVideoSubmissionTransportLocalFailuresRelease(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { received.Add(1) }))
	t.Cleanup(srv.Close)
	for _, fault := range []string{"missing-credential", "url-policy", "canceled-before-send"} {
		t.Run(fault, func(t *testing.T) {
			h, slots, ledger := transportVideoHandler(t, service.VideoProtocolSeedance, srv.URL, func(a *service.Account) {
				if fault == "missing-credential" {
					a.Credentials["api_key"] = ""
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if fault == "canceled-before-send" {
				h.gatewayService.SetVideoBillingReservationRepository(cancelAfterVideoReserve{ledger, cancel})
			}
			if fault == "url-policy" {
				h.cfg.Security.URLAllowlist.Enabled = true
				h.cfg.Security.URLAllowlist.UpstreamHosts = []string{"allowed.example.test"}
			}
			w := submitTransportVideo(t, h, service.VideoProtocolSeedance, ctx)
			if fault != "canceled-before-send" {
				require.Equal(t, 502, w.Code, w.Body.String())
			}
			v := requireTransportHold(t, ledger, service.VideoReservationReleased, 0)
			require.Equal(t, "upstream request not sent", v.LastError)
			slots.assertReleased(t)
		})
	}
	require.Zero(t, received.Load(), "local failures must not send a creation request")
}

func TestVideoSubmissionTransportAfterWriteRetainsManualHold(t *testing.T) {
	for _, protocol := range []string{service.VideoProtocolSeedance, service.VideoProtocolGemini, "minimax-v1", "minimax-v2", "grok"} {
		for _, fault := range []string{"timeout-after-read", "disconnect-after-read", "malformed-2xx", "missing-task-id-2xx"} {
			t.Run(protocol+"/"+fault, func(t *testing.T) {
				var reads atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, err := io.Copy(io.Discard, r.Body)
					if err != nil {
						return
					}
					reads.Add(1)
					switch fault {
					case "timeout-after-read":
						<-r.Context().Done()
					case "disconnect-after-read":
						conn, _, err := w.(http.Hijacker).Hijack()
						if err == nil {
							_ = conn.Close()
						}
					case "missing-task-id-2xx":
						_, _ = io.WriteString(w, `{}`)
					default:
						_, _ = io.WriteString(w, "{")
					}
				}))
				t.Cleanup(srv.Close)
				h, slots, ledger := transportVideoHandler(t, protocol, transportVideoBase(protocol, srv.URL), nil)
				w := submitTransportVideo(t, h, protocol)
				require.Equal(t, 502, w.Code, w.Body.String())
				require.Equal(t, int32(1), reads.Load(), "no automatic create retries")
				requireTransportHold(t, ledger, service.VideoReservationManual, .2)
				slots.assertReleased(t)
			})
		}
	}
}

func TestVideoSubmissionTransportNormalH3KeepsFrozenContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2/video_generation", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"task_id":"normal-h3"}`)
	}))
	t.Cleanup(srv.Close)
	h, slots, ledger := transportVideoHandler(t, "minimax-v2", srv.URL+"/v2", nil)
	w := submitTransportVideo(t, h, "minimax-v2")
	require.Equal(t, 200, w.Code, w.Body.String())
	v := requireTransportHold(t, ledger, service.VideoReservationAccepted, .2)
	handled, err := h.gatewayService.ObserveVideoTaskBilling(context.Background(), v.Protocol, v.TaskID,
		&service.OpenAIForwardResult{AsyncTaskStatus: "completed", VideoCount: 1, VideoDurationSeconds: 6}, false)
	require.NoError(t, err)
	require.True(t, handled)
	requireTransportHold(t, ledger, service.VideoReservationSettled, 0)
	usage := ledger.settlements[v.ID]
	require.InDelta(t, .2, usage.ActualCost, 1e-12)
	require.Equal(t, "/v2/video_generation", *usage.InboundEndpoint)
	require.Equal(t, "/v2/video_generation", *usage.UpstreamEndpoint)
	slots.assertReleased(t)
}
