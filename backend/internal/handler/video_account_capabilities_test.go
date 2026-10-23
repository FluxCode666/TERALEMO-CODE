//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func extendedMiniMaxCapabilities(account *service.Account) {
	account.Credentials["model_mapping"] = map[string]any{"public-video": "MiniMax-H3"}
	account.Credentials["video_resolutions"] = []string{"480p", "768p", "1080p", "2k"}
	account.Credentials["video_min_duration_seconds"], account.Credentials["video_max_duration_seconds"] = 4, 30
}

func TestMiniMaxAccountCapabilitiesSelectCompatibleAccountAndFreezeContract(t *testing.T) {
	for _, rejected := range []int{0, 1, 4} {
		t.Run(strings.Repeat("official-", rejected)+"configured", func(t *testing.T) {
			bases := make([]string, rejected+1)
			for i := range bases {
				bases[i] = "https://relay.example/api/minimax/v2/video_generation"
			}
			accounts := miniMaxProtocolAccounts(bases...)
			for i := range accounts {
				accounts[i].Credentials["model_mapping"] = map[string]any{"public-video": "MiniMax-H3"}
			}
			extendedMiniMaxCapabilities(&accounts[rejected])
			h, slots, bindings, up := miniMaxProtocolPool(t, accounts)
			up.call = func(req *http.Request, id int64) (*http.Response, error) {
				require.EqualValues(t, rejected+1, id)
				assertMiniMaxNormalizedReservation(t, bindings, 20, "1080p", .4, id, service.VideoReservationPreparing)
				body, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, "1080P", gjson.GetBytes(body, "resolution").String())
				require.EqualValues(t, 20, gjson.GetBytes(body, "duration").Int())
				require.Equal(t, "/api/minimax/v2/video_generation", req.URL.Path)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"task_id":"configured-h3"}`))}, nil
			}
			c, w := miniMaxProtocolContext(context.Background(), "/v2/video_generation", `{"model":"public-video","prompt":"waves","duration":20,"resolution":"1080P"}`)
			key, _ := middleware.GetAPIKeyFromContext(c)
			price := .4
			key.Group.VideoPrice1080P = &price
			h.MiniMaxVideoTasks(c)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, 1, up.calls, "capability rejections must never send upstream creates")
			assertMiniMaxNormalizedReservation(t, bindings, 20, "1080p", price, int64(rejected+1), service.VideoReservationAccepted)
			slots.assertReleased(t)
		})
	}
}

func TestMiniMaxAccountCapabilitiesOfficialAndConfiguredShareNativeRequest(t *testing.T) {
	for _, configuredOnly := range []bool{false, true} {
		accounts := miniMaxProtocolAccounts("https://official.example/v2", "https://relay.example/v2")
		for i := range accounts {
			accounts[i].Credentials["model_mapping"] = map[string]any{"public-video": "MiniMax-H3"}
		}
		extendedMiniMaxCapabilities(&accounts[1])
		wantID := int64(1)
		if configuredOnly {
			accounts = accounts[1:]
			wantID = 2
		}
		h, slots, bindings, up := miniMaxProtocolPool(t, accounts)
		up.call = func(req *http.Request, id int64) (*http.Response, error) {
			require.Equal(t, wantID, id)
			assertMiniMaxNormalizedReservation(t, bindings, 6, "768p", .25, id, service.VideoReservationPreparing)
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"task_id":"native-h3"}`))}, nil
		}
		c, w := miniMaxProtocolContext(context.Background(), "/v2/video_generation", `{"model":"public-video","prompt":"waves","duration":6,"resolution":"768P"}`)
		h.MiniMaxVideoTasks(c)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, 1, up.calls)
		slots.assertReleased(t)
	}
}

func TestMiniMaxAccountCapabilitiesRejectWithoutSubmissionState(t *testing.T) {
	for _, scenario := range []string{"official", "configured-mismatch", "missing-final-price"} {
		t.Run(scenario, func(t *testing.T) {
			accounts := miniMaxProtocolAccounts("https://relay.example/v2")
			accounts[0].Credentials["model_mapping"] = map[string]any{"public-video": "MiniMax-H3"}
			if scenario != "official" {
				extendedMiniMaxCapabilities(&accounts[0])
			}
			if scenario == "configured-mismatch" {
				accounts[0].Credentials["video_resolutions"] = []string{"480p"}
			}
			h, slots, bindings, up := miniMaxProtocolPool(t, accounts)
			c, w := miniMaxProtocolContext(context.Background(), "/v2/video_generation", `{"model":"public-video","prompt":"waves","duration":20,"resolution":"1080P"}`)
			if scenario == "missing-final-price" {
				key, _ := middleware.GetAPIKeyFromContext(c)
				clearPhase2VideoPrices(key.Group)
				wrongTierPrice := .1
				key.Group.VideoPrice720P = &wrongTierPrice
			}
			h.MiniMaxVideoTasks(c)
			require.Equal(t, 400, w.Code, w.Body.String())
			require.Zero(t, up.calls)
			require.Empty(t, bindings.reservations.rows)
			require.Empty(t, bindings.pending)
			slots.assertReleased(t)
			if scenario == "missing-final-price" {
				require.Equal(t, "video_pricing_not_configured", gjson.Get(w.Body.String(), "error.type").String())
			} else {
				require.Contains(t, w.Body.String(), "supported resolutions")
			}
		})
	}
}

func TestVideoAccountCapabilitiesNormalizeDefaultsBeforePricingAndForward(t *testing.T) {
	for _, protocol := range []string{service.VideoProtocolSeedance, service.VideoProtocolGemini, "minimax-v1", "minimax-v2"} {
		t.Run(protocol, func(t *testing.T) {
			base := "https://relay.example/v2"
			model := "MiniMax-H3"
			body := `{"model":"public-video","prompt":"waves"}`
			if protocol == "minimax-v1" {
				base = "https://relay.example/v1"
				model = "MiniMax-Hailuo-2.3"
			}
			if protocol == service.VideoProtocolSeedance {
				model = "doubao-seedance-1-0-pro"
				body = `{"model":"public-video","content":[{"type":"text","text":"waves"}]}`
			}
			if protocol == service.VideoProtocolGemini {
				model = "veo-3.0-generate-001"
				body = `{"model":"public-video","instances":[{"prompt":"waves"}]}`
			}
			accounts := miniMaxProtocolAccounts(base)
			accounts[0].Credentials["model_mapping"] = map[string]any{"public-video": model}
			if protocol == service.VideoProtocolSeedance || protocol == service.VideoProtocolGemini {
				accounts[0].Credentials["video_protocol"] = protocol
			}
			accounts[0].Credentials["video_resolutions"] = []string{"480p"}
			accounts[0].Credentials["video_min_duration_seconds"], accounts[0].Credentials["video_max_duration_seconds"] = 20, 30
			h, slots, bindings, up := miniMaxProtocolPool(t, accounts)
			up.call = func(req *http.Request, id int64) (*http.Response, error) {
				assertMiniMaxNormalizedReservation(t, bindings, 20, "480p", .5, id, service.VideoReservationPreparing)
				payload, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				path := ""
				response := `{"task_id":"default-capability"}`
				if protocol == service.VideoProtocolGemini {
					path = "parameters."
					response = `{"name":"models/veo-3.0-generate-001/operations/default-capability"}`
				}
				durationField := "duration"
				if path != "" {
					durationField = "durationSeconds"
				}
				require.EqualValues(t, 20, gjson.GetBytes(payload, path+durationField).Int())
				require.Equal(t, "480p", strings.ToLower(gjson.GetBytes(payload, path+"resolution").String()))
				if protocol == service.VideoProtocolSeedance {
					response = `{"id":"default-capability"}`
				}
				if protocol == "minimax-v1" {
					response = `{"task_id":"default-capability","base_resp":{"status_code":0}}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
			}
			c, w := miniMaxProtocolContext(context.Background(), "/videos", body)
			key, _ := middleware.GetAPIKeyFromContext(c)
			key.Group.VideoPrice720P = nil
			key.Group.VideoPrice768P = nil
			key.Group.VideoPrice2K = nil
			switch protocol {
			case service.VideoProtocolSeedance:
				h.SeedanceTasks(c)
			case service.VideoProtocolGemini:
				c.Params = gin.Params{{Key: "modelAction", Value: "public-video:predictLongRunning"}}
				h.GeminiVideoTasks(c)
			default:
				h.MiniMaxVideoTasks(c)
			}
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, 1, up.calls)
			slots.assertReleased(t)
		})
	}
}

func TestVideoAccountCapabilitiesSeedanceAndGeminiPoolsSkipOfficialLimits(t *testing.T) {
	for _, protocol := range []string{service.VideoProtocolSeedance, service.VideoProtocolGemini} {
		t.Run(protocol, func(t *testing.T) {
			accounts := miniMaxProtocolAccounts("https://official.example", "https://relay.example")
			model := "doubao-seedance-1-0-pro"
			body := `{"model":"public-video","content":[{"type":"text","text":"waves"}],"resolution":"1080p","duration":20}`
			if protocol == service.VideoProtocolGemini {
				model = "veo-3.0-generate-001"
				body = `{"instances":[{"prompt":"waves"}],"parameters":{"resolution":"1080p","durationSeconds":20}}`
			}
			for i := range accounts {
				accounts[i].Credentials["video_protocol"] = protocol
				accounts[i].Credentials["model_mapping"] = map[string]any{"public-video": model}
			}
			accounts[1].Credentials["video_resolutions"] = []string{"1080p"}
			accounts[1].Credentials["video_min_duration_seconds"], accounts[1].Credentials["video_max_duration_seconds"] = 4, 30
			h, slots, bindings, up := miniMaxProtocolPool(t, accounts)
			up.call = func(req *http.Request, id int64) (*http.Response, error) {
				require.EqualValues(t, 2, id)
				assertMiniMaxNormalizedReservation(t, bindings, 20, "1080p", .4, id, service.VideoReservationPreparing)
				response := `{"id":"capability-pool"}`
				if protocol == service.VideoProtocolGemini {
					response = `{"name":"models/veo-3.0-generate-001/operations/capability-pool"}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
			}
			c, w := miniMaxProtocolContext(context.Background(), "/videos", body)
			key, _ := middleware.GetAPIKeyFromContext(c)
			price := .4
			key.Group.VideoPrice1080P = &price
			if protocol == service.VideoProtocolGemini {
				c.Params = gin.Params{{Key: "modelAction", Value: "public-video:predictLongRunning"}}
				h.GeminiVideoTasks(c)
			} else {
				h.SeedanceTasks(c)
			}
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, 1, up.calls)
			slots.assertReleased(t)
		})
	}
}
