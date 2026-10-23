//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type videoBalanceBoundaryCache struct {
	service.BillingCache
	balance float64
}

func (c videoBalanceBoundaryCache) GetUserBalance(context.Context, int64) (float64, error) {
	return c.balance, nil
}

func TestVideoBalanceBoundaryPreservesGatewayStatusAndProtocol(t *testing.T) {
	for _, protocol := range []string{service.VideoProtocolSeedance, service.VideoProtocolGemini, "minimax-v1", "minimax-v2"} {
		for _, boundary := range []struct {
			name             string
			balance, reserve float64
			status           int
		}{
			{"zero", 0, 0, 403},
			{"negative", -.01, 0, 403},
			{"below-minimum-reserve", .01, .02, 403},
			{"positive-insufficient-hold", .1, 0, 402},
		} {
			t.Run(protocol+"/"+boundary.name, func(t *testing.T) {
				accountProtocol, base := protocol, "https://video.example"
				if strings.HasPrefix(protocol, "minimax-") {
					accountProtocol = service.VideoProtocolMiniMax
					if protocol == "minimax-v2" {
						base += "/v2"
					}
				}
				h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformVideo, accountProtocol, base)
				h.cfg.RunMode = "standard"
				h.cfg.Billing.MinimumBalanceReserve = boundary.reserve
				h.billingCacheService = service.NewBillingCacheService(videoBalanceBoundaryCache{balance: boundary.balance}, nil, nil, nil, nil, nil, h.cfg, nil)
				t.Cleanup(h.billingCacheService.Stop)
				h.gatewayService.SetVideoBillingReservationRepository(videoAdmissionFailureRepo{VideoBillingReservationRepository: bindings.reservations, reserveError: service.ErrInsufficientBalance})
				c, w := grokMediaSlotContext(context.Background(), true)
				key, _ := middleware.GetAPIKeyFromContext(c)
				key.Group.Platform = service.PlatformVideo
				path, body := EndpointSeedanceTasks, `{"model":"doubao-seedance-1-0-pro-250528","content":[{"type":"text","text":"waves"}],"duration":6}`
				switch protocol {
				case service.VideoProtocolGemini:
					path, body = "/v1beta/models/veo:predictLongRunning", `{"instances":[{"prompt":"waves"}],"parameters":{"durationSeconds":8,"resolution":"720p"}}`
					c.Params = gin.Params{{Key: "modelAction", Value: "/veo:predictLongRunning"}}
				case "minimax-v1":
					path, body = "/v1/video_generation", `{"model":"MiniMax-Hailuo-2.3","prompt":"waves","duration":6,"resolution":"768P"}`
				case "minimax-v2":
					path, body = "/v2/video_generation", `{"model":"MiniMax-H3","content":[{"type":"text","text":"waves"}],"duration":6,"resolution":"768P","ratio":"16:9"}`
				}
				c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				switch protocol {
				case service.VideoProtocolSeedance:
					h.SeedanceTasks(c)
				case service.VideoProtocolGemini:
					h.GeminiVideoTasks(c)
				default:
					h.MiniMaxVideoTasks(c)
				}
				require.Equal(t, boundary.status, w.Code, w.Body.String())
				if protocol == service.VideoProtocolGemini {
					requireGeminiVideoError(t, w, boundary.status)
				} else {
					require.NotEmpty(t, gjson.Get(w.Body.String(), "error.type").String())
					require.False(t, gjson.Get(w.Body.String(), "error.status").Exists())
				}
				require.Zero(t, upstream.calls)
				require.Empty(t, bindings.reservations.rows)
				slots.assertReleased(t)
			})
		}
	}
}
