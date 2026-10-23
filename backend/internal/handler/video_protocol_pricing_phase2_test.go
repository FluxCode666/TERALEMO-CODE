//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func clearPhase2VideoPrices(group *service.Group) {
	group.VideoPrice480P, group.VideoPrice720P, group.VideoPrice768P, group.VideoPrice1080P, group.VideoPrice2K = nil, nil, nil, nil, nil
	group.VideoPricePerRequest = nil
}

func TestNativeVideoMissingPricingDoesNotLeaveSubmissionState(t *testing.T) {
	for _, protocol := range []string{"minimax-v1", "minimax-v2", "gemini"} {
		t.Run(protocol, func(t *testing.T) {
			version := strings.TrimPrefix(protocol, "minimax-")
			accounts := miniMaxProtocolAccounts("https://relay.example/" + version)
			body := `{"model":"public-video","prompt":"waves"}`
			path := "/" + version + "/video_generation"
			if protocol == "gemini" {
				accounts[0].Credentials["video_protocol"] = service.VideoProtocolGemini
				body = `{"instances":[{"prompt":"waves"}],"parameters":{"durationSeconds":8,"resolution":"720p"}}`
				path = "/v1beta/models/veo:predictLongRunning"
			}
			h, slots, bindings, upstream := miniMaxProtocolPool(t, accounts)
			mr := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			t.Cleanup(func() { _ = rdb.Close() })
			h.asyncTasks = service.NewImageTaskService(repository.NewImageTaskStore(rdb))
			c, w := miniMaxProtocolContext(context.Background(), path, body)
			key, _ := middleware.GetAPIKeyFromContext(c)
			clearPhase2VideoPrices(key.Group)
			if protocol == "gemini" {
				c.Params = gin.Params{{Key: "modelAction", Value: "/veo:predictLongRunning"}}
				h.GeminiVideoTasks(c)
				requireGeminiVideoError(t, w, 400)
				errType, _ := service.OpsLocalErrorType(c)
				require.Equal(t, "video_pricing_not_configured", errType)
			} else {
				h.MiniMaxVideoTasks(c)
				require.Equal(t, 400, w.Code, w.Body.String())
				require.Equal(t, "video_pricing_not_configured", gjson.Get(w.Body.String(), "error.type").String())
				require.Positive(t, slots.acquired, "MiniMax pricing must use the selected account's normalized request")
				require.Positive(t, slots.userAcquired)
			}
			require.Zero(t, upstream.calls)
			require.Empty(t, bindings.reservations.rows)
			require.Empty(t, bindings.pending)
			require.Empty(t, bindings.reservations.settlements)
			page, err := h.asyncTasks.ListUserTasks(context.Background(), key.UserID, 1, 20, "video", "", "")
			require.NoError(t, err)
			require.Zero(t, page.Total)
			require.Empty(t, mr.Keys(), "a rejected creation must not write the task catalog")
			slots.assertReleased(t)
		})
	}
}

func TestMiniMaxAliasPricingUsesFinalNormalizedTier(t *testing.T) {
	for _, resolution := range []string{"480P", "768P"} {
		for _, priced := range []bool{true, false} {
			name := resolution + "/wrong-tier-only"
			if priced {
				name = resolution + "/final-tier-priced"
			}
			t.Run(name, func(t *testing.T) {
				accounts := miniMaxProtocolAccounts("https://relay.example/v2")
				accounts[0].Credentials["model_mapping"] = map[string]any{"public-video": "MiniMax-H3-Max"}
				h, slots, bindings, upstream := miniMaxProtocolPool(t, accounts)
				body := `{"model":"public-video","prompt":"waves","duration":7}`
				if resolution == "480P" {
					body = `{"model":"public-video","prompt":"waves","duration":7,"resolution":"480P"}`
				}
				c, w := miniMaxProtocolContext(context.Background(), "/v2/video_generation", body)
				key, _ := middleware.GetAPIKeyFromContext(c)
				clearPhase2VideoPrices(key.Group)
				price := 0.5
				if (resolution == "480P") == priced {
					key.Group.VideoPrice480P = &price
				} else {
					key.Group.VideoPrice768P = &price
				}
				upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
					payload, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.Equal(t, resolution, gjson.GetBytes(payload, "resolution").String())
					assertMiniMaxNormalizedReservation(t, bindings, 7, strings.ToLower(resolution), price, id, service.VideoReservationPreparing)
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"task_id":"priced-task"}`))}, nil
				}
				h.MiniMaxVideoTasks(c)
				if priced {
					require.Equal(t, 200, w.Code, w.Body.String())
					require.Equal(t, 1, upstream.calls)
				} else {
					require.Equal(t, 400, w.Code, w.Body.String())
					require.Equal(t, "video_pricing_not_configured", gjson.Get(w.Body.String(), "error.type").String())
					require.Zero(t, upstream.calls)
					require.Empty(t, bindings.reservations.rows)
				}
				slots.assertReleased(t)
			})
		}
	}
}

func TestLegacyVideoTokenOverrideAccepts2KBeforeForward(t *testing.T) {
	for _, protocol := range []string{"seedance", "grok"} {
		t.Run(protocol, func(t *testing.T) {
			accounts := miniMaxProtocolAccounts("https://relay.example/v1")
			platform, model := service.PlatformGrok, "grok-imagine-video"
			body, response := `{"model":"grok-imagine-video","prompt":"waves","duration":6,"resolution":"2K"}`, `{"request_id":"legacy-task"}`
			if protocol == "seedance" {
				platform, model = service.PlatformOpenAI, "doubao-seedance-2-0"
				body = `{"model":"doubao-seedance-2-0","content":[{"type":"text","text":"waves"}],"duration":6,"resolution":"2K"}`
				response = `{"id":"legacy-task"}`
				accounts[0].Credentials["openai_capabilities"] = []string{"seedance"}
			}
			accounts[0].Platform = platform
			accounts[0].Credentials["video_protocol"] = protocol
			h, slots, bindings, upstream := miniMaxProtocolPool(t, accounts)
			c, w := miniMaxProtocolContext(context.Background(), "/v1/videos/generations", body)
			key, _ := middleware.GetAPIKeyFromContext(c)
			key.Group.Platform = platform
			clearPhase2VideoPrices(key.Group)
			price := 0.000002
			key.Group.ModelPricing = []service.ChannelModelPricing{{Models: []string{model}, BillingMode: service.BillingModeToken, OutputPrice: &price}}
			upstream.call = func(req *http.Request, _ int64) (*http.Response, error) {
				payload, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, "2k", strings.ToLower(gjson.GetBytes(payload, "resolution").String()))
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
			}
			if protocol == "seedance" {
				c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				h.SeedanceTasks(c)
			} else {
				h.GrokVideoGeneration(c)
			}
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, 1, upstream.calls)
			require.Len(t, bindings.reservations.rows, 1)
			for _, reservation := range bindings.reservations.rows {
				var contract service.VideoPricingContract
				require.NoError(t, json.Unmarshal(reservation.Contract, &contract))
				require.Equal(t, service.BillingModeToken, contract.Mode)
				require.Equal(t, "2k", contract.Resolution)
				require.Positive(t, reservation.HoldAmount)
			}
			slots.assertReleased(t)
		})
	}
}
