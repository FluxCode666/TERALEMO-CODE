//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVideoResolutionAsyncBillingAcrossProtocols(t *testing.T) {
	testVideoAsyncBillingAcrossProtocols(t, false)
}

func TestVideoPerRequestAsyncBillingAcrossProtocols(t *testing.T) {
	testVideoAsyncBillingAcrossProtocols(t, true)
}

func testVideoAsyncBillingAcrossProtocols(t *testing.T, perRequest bool) {
	for _, protocol := range []string{"seedance", "gemini", "minimax", "grok"} {
		if perRequest && protocol == "grok" {
			continue
		}
		resolutions := []string{"768p", "2K"}
		if protocol == "seedance" {
			resolutions = []string{"480p", "1080p", "768p", "2K"}
		} else if protocol == "gemini" {
			resolutions = []string{"720p", "1080p"}
		}
		for _, resolution := range resolutions {
			t.Run(protocol+"/"+resolution, func(t *testing.T) {
				platform := service.PlatformVideo
				if protocol == "grok" {
					platform = service.PlatformGrok
				}
				platforms := []string{platform, protocol, "https://video.example/v2"}
				if protocol == "grok" {
					platforms = nil
				}
				h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, platforms...)
				c, w := grokMediaSlotContext(context.Background(), true)
				key, _ := middleware.GetAPIKeyFromContext(c)
				key.Group.Platform = platform
				price480, price720, price768, price1080, price2K := 0.04, 0.07, 0.12, 0.2, 0.3
				key.Group.VideoPrice480P, key.Group.VideoPrice1080P = &price480, &price1080
				key.Group.VideoPrice720P, key.Group.VideoPrice768P, key.Group.VideoPrice2K = &price720, &price768, &price2K
				if perRequest {
					key.Group.VideoBillingMode = string(service.BillingModePerRequest)
					key.Group.VideoPricePerRequest = &price768
					key.Group.VideoPrice720P, key.Group.VideoPrice768P, key.Group.VideoPrice2K = nil, nil, nil
					key.Group.VideoPrice480P, key.Group.VideoPrice1080P = nil, nil
				}
				body := fmt.Sprintf(`{"model":"custom-video","prompt":"waves","duration":6,"resolution":%q}`, resolution)
				response, taskID := `{"request_id":"task"}`, "task"
				duration := 6
				switch protocol {
				case "grok":
					body = strings.Replace(body, "custom-video", "grok-imagine-video", 1)
				case "seedance":
					body = fmt.Sprintf(`{"model":"doubao-seedance-1-0-pro-250528","content":[{"type":"text","text":"waves"}],"duration":6,"resolution":%q}`, resolution)
					response, taskID = `{"id":"task"}`, "seedance:task"
				case "gemini":
					duration = 8
					body = fmt.Sprintf(`{"model":"veo-3.0-generate-001","instances":[{"prompt":"waves"}],"parameters":{"durationSeconds":8,"resolution":%q}}`, resolution)
					response, taskID = `{"name":"models/veo-3.0-generate-001/operations/task"}`, "gemini:models/veo-3.0-generate-001/operations/task"
				case "minimax":
					response, taskID = `{"task_id":"task"}`, "minimax:task"
				}
				upstream.call = func(req *http.Request, _ int64) (*http.Response, error) {
					payload, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.Contains(t, strings.ToLower(string(payload)), strings.ToLower(resolution))
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
				}
				c.Request = httptest.NewRequest(http.MethodPost, "/videos", strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				switch protocol {
				case "seedance":
					h.handleGrokMedia(c, service.SeedanceEndpointCreate, "")
				case "gemini":
					h.handleGrokMedia(c, service.GeminiVideoEndpointCreate, "")
				case "minimax":
					h.handleGrokMedia(c, service.MiniMaxVideoEndpointCreate, "")
				case "grok":
					h.GrokVideoGeneration(c)
				}
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				require.Len(t, bindings.reservations.rows, 1)
				slots.assertReleased(t)
				reservation, err := bindings.reservations.GetByTask(context.Background(), protocol, taskID)
				require.NoError(t, err)
				var contract service.VideoPricingContract
				require.NoError(t, json.Unmarshal(reservation.Contract, &contract))
				require.Equal(t, strings.ToLower(resolution), contract.Resolution)
				expectedPrice := price768
				switch resolution {
				case "480p":
					expectedPrice = price480
				case "720p":
					expectedPrice = price720
				case "1080p":
					expectedPrice = price1080
				case "2K":
					expectedPrice = price2K
				}
				mode := service.BillingModeVideo
				if perRequest {
					mode, expectedPrice = service.BillingModePerRequest, price768
				}
				require.Equal(t, mode, contract.Mode)
				require.InDelta(t, expectedPrice, contract.UnitPrice, 1e-12)
				for poll := 0; poll < 3; poll++ {
					found, err := h.gatewayService.ObserveVideoTaskBilling(context.Background(), protocol, taskID, &service.OpenAIForwardResult{ResponseID: taskID, VideoCount: 1, AsyncTaskStatus: "completed"}, false)
					require.True(t, found)
					require.NoError(t, err)
					require.Len(t, bindings.reservations.settlements, 1)
				}
				usage := bindings.reservations.settlements[reservation.ID]
				require.Equal(t, strings.ToLower(resolution), *usage.VideoResolution)
				require.Equal(t, duration, *usage.VideoDurationSeconds)
				require.Equal(t, string(mode), *usage.BillingMode)

			})
		}
	}
}
