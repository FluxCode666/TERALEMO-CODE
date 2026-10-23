//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type videoAdmissionFailureRepo struct {
	service.VideoBillingReservationRepository
	reserveError, acceptError error
}

func (r videoAdmissionFailureRepo) Reserve(ctx context.Context, v *service.VideoBillingReservation) error {
	if r.reserveError != nil {
		return r.reserveError
	}
	return r.VideoBillingReservationRepository.Reserve(ctx, v)
}
func (r videoAdmissionFailureRepo) Accept(ctx context.Context, id, task string, account int64) error {
	if r.acceptError != nil {
		return r.acceptError
	}
	return r.VideoBillingReservationRepository.Accept(ctx, id, task, account)
}

func TestVideoDurableAdmissionRejectsBeforeUpstream(t *testing.T) {
	for _, failure := range []error{service.ErrInsufficientBalance, service.ErrVideoReservationQuotaExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false)
			h.gatewayService.SetVideoBillingReservationRepository(videoAdmissionFailureRepo{VideoBillingReservationRepository: bindings.reservations, reserveError: failure})
			c, w := grokMediaSlotContext(context.Background(), true)
			h.GrokVideoGeneration(c)
			require.GreaterOrEqual(t, w.Code, 400, w.Body.String())
			require.Zero(t, upstream.calls)
			require.Empty(t, bindings.reservations.rows)
			slots.assertReleased(t)
		})
	}
}

func TestVideoCreationWaitsForDurableAcceptAndRetainsUncertainHold(t *testing.T) {
	h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false)
	h.gatewayService.SetVideoBillingReservationRepository(videoAdmissionFailureRepo{VideoBillingReservationRepository: bindings.reservations, acceptError: errors.New("database unavailable")})
	c, w := grokMediaSlotContext(context.Background(), true)
	h.GrokVideoGeneration(c)
	require.GreaterOrEqual(t, w.Code, 500, w.Body.String())
	require.NotContains(t, w.Body.String(), `"request_id":"task"`)
	require.Equal(t, 1, upstream.calls)
	require.Len(t, bindings.reservations.rows, 1)
	for _, v := range bindings.reservations.rows {
		require.Equal(t, service.VideoReservationManual, v.State)
	}
	slots.assertReleased(t)
}

func TestVideoCreationExplicitRejectionReleasesAndNeverRetries(t *testing.T) {
	h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformVideo, service.VideoProtocolSeedance)
	upstream.call = func(*http.Request, int64) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"overloaded"}}`))}, nil
	}
	c, w := grokMediaSlotContext(context.Background(), true)
	key, _ := middleware.GetAPIKeyFromContext(c)
	key.Group.Platform = service.PlatformVideo
	c.Request = httptest.NewRequest(http.MethodPost, EndpointSeedanceTasks, strings.NewReader(`{"model":"doubao-seedance-1-0-pro-250528","content":[{"type":"text","text":"waves"}]}`))
	h.handleGrokMedia(c, service.SeedanceEndpointCreate, "")
	require.Equal(t, 429, w.Code, w.Body.String())
	require.Equal(t, 1, upstream.calls)
	require.Len(t, bindings.reservations.rows, 1)
	for _, v := range bindings.reservations.rows {
		require.Equal(t, service.VideoReservationReleased, v.State)
	}
	slots.assertReleased(t)
}

func TestVideoFrozenEndpointsAcrossHTTPObservers(t *testing.T) {
	for _, protocol := range []string{service.VideoProtocolSeedance, service.VideoProtocolGemini, service.VideoProtocolMiniMax, "grok"} {
		for _, source := range []string{"native", "user-refresh", "admin-refresh", "content"} {
			t.Run(protocol+"/"+source, func(t *testing.T) {
				platforms := []string{service.PlatformVideo, protocol, "https://video.example/v2"}
				if protocol == "grok" {
					platforms = nil
				}
				h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, platforms...)
				createEndpoint, statusEndpoint := service.SeedanceEndpointCreate, service.SeedanceEndpointStatus
				path, body, created, completed, id := EndpointSeedanceTasks, `{"model":"doubao-seedance-1-0-pro-250528","content":[{"type":"text","text":"waves"}],"duration":6}`, `{"id":"task-frozen"}`, `{"id":"task-frozen","status":"succeeded","duration":6,"content":{"video_url":"https://cdn.example/a.mp4"}}`, "seedance:task-frozen"
				wantUpstream := EndpointSeedanceTasks
				switch protocol {
				case service.VideoProtocolGemini:
					createEndpoint, statusEndpoint = service.GeminiVideoEndpointCreate, service.GeminiVideoEndpointStatus
					path, body, created, completed, id = "/v1beta/models/veo:predictLongRunning", `{"model":"veo","instances":[{"prompt":"waves"}],"parameters":{"durationSeconds":8}}`, `{"name":"models/veo/operations/task-frozen"}`, `{"name":"models/veo/operations/task-frozen","done":true,"response":{"generatedVideos":[{"video":{"uri":"https://video.example/a.mp4"}}]}}`, "gemini:models/veo/operations/task-frozen"
					wantUpstream = EndpointGeminiVideoCreate
				case service.VideoProtocolMiniMax:
					createEndpoint, statusEndpoint = service.MiniMaxVideoEndpointCreate, service.MiniMaxVideoEndpointStatus
					path, body, created, completed, id = EndpointMiniMaxV2VideoCreate, `{"model":"MiniMax-H3","prompt":"waves","duration":6,"ratio":"16:9"}`, `{"task_id":"task-frozen"}`, `{"task":{"id":"task-frozen","status":"succeeded","content":{"url":"https://cdn.example/a.mp4"}}}`, "minimax:task-frozen"
					wantUpstream = EndpointMiniMaxV2VideoCreate
				case "grok":
					createEndpoint, statusEndpoint = service.GrokMediaEndpointVideosGenerations, service.GrokMediaEndpointVideoStatus
					path, body, created, completed, id = EndpointVideosGenerations, `{"model":"grok-imagine-video","prompt":"waves","duration":6}`, `{"request_id":"task-frozen"}`, `{"status":"done","video":{"url":"https://cdn.example/a.mp4","duration":6}}`, "task-frozen"
					wantUpstream = EndpointVideosGenerations
				}
				upstream.call = func(req *http.Request, _ int64) (*http.Response, error) {
					payload := completed
					if req.Method == http.MethodPost {
						payload = created
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
				}
				c, w := grokMediaSlotContext(context.Background(), true)
				key, _ := middleware.GetAPIKeyFromContext(c)
				if protocol != "grok" {
					key.Group.Platform = service.PlatformVideo
				}
				c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				h.handleGrokMedia(c, createEndpoint, "")
				require.Equal(t, 200, w.Code, w.Body.String())
				frozen, err := bindings.reservations.GetByTask(context.Background(), protocol, id)
				require.NoError(t, err)
				var contract service.VideoPricingContract
				require.NoError(t, json.Unmarshal(frozen.Contract, &contract))
				require.Equal(t, NormalizeInboundEndpoint(path), contract.InboundEndpoint)
				require.Equal(t, wantUpstream, contract.UpstreamEndpoint)
				pollPath := path + "/task-frozen"
				switch source {
				case "user-refresh":
					pollPath = "/api/v1/user/async-tasks/vidtask_abcdef/refresh"
				case "admin-refresh":
					pollPath = "/api/v1/admin/async-tasks/10/vidtask_abcdef/refresh"
				case "content":
					pollPath = "/api/v1/user/async-tasks/vidtask_abcdef/content"
				}
				for retry := 0; retry < 2; retry++ {
					c, w = grokMediaSlotContext(context.Background(), false)
					key, _ = middleware.GetAPIKeyFromContext(c)
					if protocol != "grok" {
						key.Group.Platform = service.PlatformVideo
					}
					c.Request = httptest.NewRequest(http.MethodGet, pollPath, nil)
					h.handleGrokMedia(c, statusEndpoint, id)
					require.Equal(t, 200, w.Code, w.Body.String())
				}
				require.Len(t, bindings.reservations.settlements, 1)
				for _, usage := range bindings.reservations.settlements {
					require.NotNil(t, usage.InboundEndpoint)
					require.NotNil(t, usage.UpstreamEndpoint)
					require.Equal(t, contract.InboundEndpoint, *usage.InboundEndpoint)
					require.Equal(t, wantUpstream, *usage.UpstreamEndpoint)
				}
				slots.assertReleased(t)
			})
		}
	}
}

func TestVideoCreationUncertainOutcomesRetainHold(t *testing.T) {
	for _, scenario := range []string{"transport", "malformed-2xx", "missing-id", "panic"} {
		t.Run(scenario, func(t *testing.T) {
			h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformVideo, service.VideoProtocolSeedance)
			upstream.call = func(*http.Request, int64) (*http.Response, error) {
				if scenario == "transport" {
					return nil, errors.New("connection closed after request upload")
				}
				if scenario == "panic" {
					panic("injected forwarding panic")
				}
				body := `{}`
				if scenario == "malformed-2xx" {
					body = `<html>unknown</html>`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			}
			c, w := grokMediaSlotContext(context.Background(), true)
			key, _ := middleware.GetAPIKeyFromContext(c)
			key.Group.Platform = service.PlatformVideo
			c.Request = httptest.NewRequest(http.MethodPost, EndpointSeedanceTasks, strings.NewReader(`{"model":"doubao-seedance-1-0-pro-250528","content":[{"type":"text","text":"waves"}]}`))
			h.handleGrokMedia(c, service.SeedanceEndpointCreate, "")
			require.GreaterOrEqual(t, w.Code, 500, w.Body.String())
			require.Equal(t, 1, upstream.calls)
			require.Len(t, bindings.reservations.rows, 1)
			for _, v := range bindings.reservations.rows {
				require.Equal(t, service.VideoReservationManual, v.State)
			}
			slots.assertReleased(t)
		})
	}
}

func TestVideoCreationComplete503ReleasesButPreservesHTTP(t *testing.T) {
	h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformVideo, service.VideoProtocolSeedance)
	upstream.call = func(*http.Request, int64) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Header: http.Header{"Content-Type": {"application/json"}, "Retry-After": {"17"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"temporarily unavailable"}}`))}, nil
	}
	c, w := grokMediaSlotContext(context.Background(), true)
	key, _ := middleware.GetAPIKeyFromContext(c)
	key.Group.Platform = service.PlatformVideo
	c.Request = httptest.NewRequest(http.MethodPost, EndpointSeedanceTasks, strings.NewReader(`{"model":"doubao-seedance-1-0-pro-250528","content":[{"type":"text","text":"waves"}]}`))
	h.handleGrokMedia(c, service.SeedanceEndpointCreate, "")
	require.Equal(t, 503, w.Code, w.Body.String())
	require.Equal(t, "17", w.Header().Get("Retry-After"))
	require.Equal(t, 1, upstream.calls)
	for _, v := range bindings.reservations.rows {
		require.Equal(t, service.VideoReservationReleased, v.State)
	}
	slots.assertReleased(t)
}
