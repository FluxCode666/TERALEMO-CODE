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
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func miniMaxProtocolAccounts(bases ...string) []service.Account {
	out := make([]service.Account, len(bases))
	for i, base := range bases {
		out[i] = service.Account{ID: int64(i + 1), Platform: service.PlatformVideo, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 50, Priority: i, GroupIDs: []int64{24}, Credentials: map[string]any{"api_key": "mock-key", "video_protocol": service.VideoProtocolMiniMax, "base_url": base}}
	}
	return out
}

func miniMaxProtocolPool(t *testing.T, accounts []service.Account) (*OpenAIGatewayHandler, *grokMediaSlotsCache, *grokMediaSlotBindings, *grokMediaSlotUpstream) {
	t.Helper()
	slots := &grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}}
	concurrency := service.NewConcurrencyService(slots)
	bindings := &grokMediaSlotBindings{owner: 1, reservations: newVideoSubmissionReservationRepo()}
	up := &grokMediaSlotUpstream{}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 20 * time.Millisecond
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 3
	cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
	repo := grokMediaSlotRepo{openAIImagesFailoverAccountRepo: openAIImagesFailoverAccountRepo{accounts: accounts}}
	gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, bindings, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, nil, up, service.NewDeferredService(nil, nil, time.Second), nil, service.NewGrokTokenProvider(repo, nil), nil, nil, nil, nil, nil)
	gateway.SetVideoBillingReservationRepository(bindings.reservations)
	billing := service.NewBillingCacheService(newHandlerInflightCache(1000), nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	h := NewOpenAIGatewayHandler(gateway, concurrency, billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	return h, slots, bindings, up
}

func miniMaxProtocolContext(ctx context.Context, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	c, w := grokMediaSlotContext(ctx, true)
	key, _ := middleware.GetAPIKeyFromContext(c)
	key.Group.Platform = service.PlatformVideo
	key.User.Balance = 1000
	price480, price768, price2K := 0.5, 0.25, 1.0
	key.Group.VideoPrice480P = &price480
	key.Group.VideoPrice768P = &price768
	key.Group.VideoPrice2K = &price2K
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

func assertMiniMaxNormalizedReservation(t *testing.T, bindings *grokMediaSlotBindings, seconds int, resolution string, price float64, accountID int64, state string) {
	t.Helper()
	bindings.reservations.mu.Lock()
	defer bindings.reservations.mu.Unlock()
	require.Len(t, bindings.reservations.rows, 1)
	for _, v := range bindings.reservations.rows {
		require.Equal(t, state, v.State)
		require.Equal(t, accountID, v.AccountID)
		var contract service.VideoPricingContract
		var snapshot service.VideoBillingSnapshot
		require.NoError(t, json.Unmarshal(v.Contract, &contract))
		require.NoError(t, json.Unmarshal(v.Snapshot, &snapshot))
		require.Equal(t, resolution, contract.Resolution)
		require.Equal(t, seconds, contract.MaxDurationSeconds)
		require.Equal(t, float64(seconds), contract.RequestedDurationSeconds)
		require.Equal(t, resolution, snapshot.Resolution)
		require.Equal(t, float64(seconds), snapshot.RequestedDurationSeconds)
		require.Equal(t, 1, contract.MaxVideoCount)
		require.InDelta(t, price, contract.UnitPrice, 1e-8)
		require.InDelta(t, float64(seconds)*price, contract.ReservedAmount, 1e-8)
		require.InDelta(t, contract.ReservedAmount, v.HoldAmount, 1e-8)
	}
}

// Correct-behavior replacements for W1 B7: aliases are validated after mapping,
// and the contract already matches the normalized body when upstream is called.
func TestMiniMaxAliasLegacySubmissionFreezesNormalizedContract(t *testing.T) {
	for _, path := range []string{"/v2/video_generation", "/v1/video_generation"} {
		for _, tc := range []struct {
			body, resolution string
			seconds          int
			price            float64
		}{
			{`{"model":"public-video","prompt":"waves","duration":7}`, "768p", 7, 0.25},
			{`{"model":"public-video","prompt":"waves","duration":6,"resolution":"480P"}`, "480p", 6, 0.5},
		} {
			t.Run(path+"/"+tc.resolution, func(t *testing.T) {
				accounts := miniMaxProtocolAccounts("https://relay.example/api/minimax/v2")
				accounts[0].Credentials["model_mapping"] = map[string]any{"public-video": "MiniMax-H3-Max", "MiniMax-H3-Max": "must-not-map-twice"}
				h, slots, bindings, up := miniMaxProtocolPool(t, accounts)
				up.call = func(req *http.Request, id int64) (*http.Response, error) {
					assertMiniMaxNormalizedReservation(t, bindings, tc.seconds, tc.resolution, tc.price, id, service.VideoReservationPreparing)
					body, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.Equal(t, "MiniMax-H3-Max", gjson.GetBytes(body, "model").String())
					require.Equal(t, "waves", gjson.GetBytes(body, "content.0.text").String())
					require.Equal(t, strings.ToUpper(tc.resolution), gjson.GetBytes(body, "resolution").String())
					require.Equal(t, int64(tc.seconds), gjson.GetBytes(body, "duration").Int())
					require.False(t, gjson.GetBytes(body, "prompt").Exists())
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"task_id":"h3-alias"}`))}, nil
				}
				c, w := miniMaxProtocolContext(context.Background(), path, tc.body)
				h.MiniMaxVideoTasks(c)
				require.Equal(t, 200, w.Code, w.Body.String())
				require.Equal(t, 1, up.calls, "exactly one upstream creation")
				assertMiniMaxNormalizedReservation(t, bindings, tc.seconds, tc.resolution, tc.price, 1, service.VideoReservationAccepted)
				slots.assertReleased(t)
			})
		}
	}
}

func TestMiniMaxMixedVersionPoolSkipsV1BeforeCreate(t *testing.T) {
	body := `{"model":"MiniMax-H3","content":[{"type":"text","text":"waves"}],"duration":7,"resolution":"768P","ratio":"16:9"}`
	for _, scenario := range []string{"v1-first", "v2-first", "stale-v1-sticky", "four-v1-first", "alias-legacy"} {
		t.Run(scenario, func(t *testing.T) {
			bases := []string{"https://relay.example/v1", "https://relay.example/v2", "https://relay.example/v2"}
			wantID := int64(2)
			if scenario == "v2-first" {
				bases[0] = "https://relay.example/v2"
				wantID = 1
			}
			if scenario == "four-v1-first" {
				bases = []string{"https://relay.example/v1", "https://relay.example/v1", "https://relay.example/v1", "https://relay.example/v1", "https://relay.example/v2"}
				wantID = 5
			}
			accounts := miniMaxProtocolAccounts(bases...)
			payload := body
			if scenario == "alias-legacy" {
				payload = `{"model":"public-video","prompt":"waves","duration":7}`
				for i := range accounts {
					accounts[i].Credentials["model_mapping"] = map[string]any{"public-video": "MiniMax-H3-Max"}
				}
			}
			h, slots, bindings, up := miniMaxProtocolPool(t, accounts)
			perAccount := map[int64]int{}
			up.call = func(req *http.Request, id int64) (*http.Response, error) {
				perAccount[id]++
				require.Equal(t, wantID, id)
				assertMiniMaxNormalizedReservation(t, bindings, 7, "768p", 0.25, id, service.VideoReservationPreparing)
				require.Equal(t, "/v2/video_generation", req.URL.Path)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"task_id":"mixed-h3"}`))}, nil
			}
			c, w := miniMaxProtocolContext(context.Background(), "/v2/video_generation", payload)
			if scenario == "stale-v1-sticky" {
				hash := h.gatewayService.GenerateExplicitSessionHash(c, []byte(payload))
				require.NoError(t, bindings.SetSessionAccountID(context.Background(), 24, hash, 1, time.Hour))
			}
			h.MiniMaxVideoTasks(c)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, 1, up.calls)
			for i, base := range bases {
				if strings.HasSuffix(base, "/v1") {
					require.Zero(t, perAccount[int64(i+1)])
				}
			}
			assertMiniMaxNormalizedReservation(t, bindings, 7, "768p", 0.25, wantID, service.VideoReservationAccepted)
			slots.assertReleased(t)
		})
	}
}

func TestMiniMaxProtocolRejectsBeforeReservationOrCreation(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		onlyV1     bool
		status     int
	}{
		{"mapped-max-duration", `{"model":"public-video","prompt":"waves","duration":4}`, false, 400},
		{"mapped-max-2k", `{"model":"public-video","prompt":"waves","resolution":"2K"}`, false, 400},
		{"malformed", `{`, false, 400},
		{"fraction", `{"model":"public-video","prompt":"waves","duration":7.5}`, false, 400},
		{"zero", `{"model":"public-video","prompt":"waves","duration":0}`, false, 400},
		{"invalid-ratio", `{"model":"public-video","content":[{"type":"text","text":"waves"}],"ratio":"2:1"}`, false, 400},
		{"all-v1", `{"model":"MiniMax-H3","prompt":"waves","duration":7}`, true, 503},
		{"v2-route-even-for-legacy", `{"model":"public-video","prompt":"waves","duration":6}`, true, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := "https://relay.example/v2"
			if tc.onlyV1 {
				base = "https://relay.example/v1"
			}
			accounts := miniMaxProtocolAccounts(base, base)
			for i := range accounts {
				accounts[i].Credentials["model_mapping"] = map[string]any{"public-video": "MiniMax-H3-Max", "MiniMax-H3": "MiniMax-H3"}
			}
			h, slots, bindings, up := miniMaxProtocolPool(t, accounts)
			up.call = func(*http.Request, int64) (*http.Response, error) { return nil, fmt.Errorf("must not create") }
			c, w := miniMaxProtocolContext(context.Background(), "/v2/video_generation", tc.body)
			h.MiniMaxVideoTasks(c)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Zero(t, up.calls)
			require.Empty(t, bindings.reservations.rows, "validation must precede monetary reservation")
			slots.assertReleased(t)
		})
	}
}

func TestMiniMaxV1ProtocolDefaultsAndV2OwnedLookup(t *testing.T) {
	for _, version := range []string{"v1", "v2"} {
		t.Run(version, func(t *testing.T) {
			accounts := miniMaxProtocolAccounts("https://relay.example/" + version)
			model := "MiniMax-Hailuo-2.3"
			if version == "v2" {
				model = "MiniMax-H3-Max"
			}
			accounts[0].Credentials["model_mapping"] = map[string]any{"public-video": model}
			h, slots, bindings, up := miniMaxProtocolPool(t, accounts)
			up.call = func(req *http.Request, id int64) (*http.Response, error) {
				require.Equal(t, int64(1), id)
				response := `{"task_id":"owned-h3","base_resp":{"status_code":0}}`
				if req.Method == http.MethodPost {
					body, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.Equal(t, model, gjson.GetBytes(body, "model").String())
					require.Equal(t, int64(6), gjson.GetBytes(body, "duration").Int())
					require.Equal(t, "768P", gjson.GetBytes(body, "resolution").String())
					if version == "v1" {
						require.Equal(t, "waves", gjson.GetBytes(body, "prompt").String())
						require.False(t, gjson.GetBytes(body, "content").Exists())
					}
					assertMiniMaxNormalizedReservation(t, bindings, 6, "768p", 0.25, id, service.VideoReservationPreparing)
				} else {
					if version == "v2" {
						require.Equal(t, "/v2/query/video_generation/owned-h3", req.URL.Path)
						response = `{"task":{"id":"owned-h3","status":"running"}}`
					} else {
						require.Equal(t, "owned-h3", req.URL.Query().Get("task_id"))
						response = `{"task_id":"owned-h3","status":"Processing","base_resp":{"status_code":0}}`
					}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
			}
			c, w := miniMaxProtocolContext(context.Background(), "/"+version+"/video_generation", `{"model":"public-video","prompt":"waves"}`)
			h.MiniMaxVideoTasks(c)
			require.Equal(t, 200, w.Code, w.Body.String())
			for _, otherKey := range []bool{false, true} {
				c, w = miniMaxProtocolContext(context.Background(), "/"+version+"/query/video_generation?task_id=owned-h3", "")
				c.Request.Method = http.MethodGet
				if version == "v2" {
					c.Params = gin.Params{{Key: "task_id", Value: "owned-h3"}}
				}
				before := up.calls
				if otherKey {
					key, _ := middleware.GetAPIKeyFromContext(c)
					key.ID++
				}
				h.MiniMaxVideoTasks(c)
				if otherKey {
					require.Equal(t, 404, w.Code)
					require.Equal(t, before, up.calls)
				} else {
					require.Equal(t, 200, w.Code, w.Body.String())
				}
			}
			require.Equal(t, 2, up.calls)
			slots.assertReleased(t)
		})
	}
}

func TestMiniMaxKnownPublicModelsUseMappedConstraintsBeforeReserve(t *testing.T) {
	for _, tc := range []struct {
		name, body, upstreamModel, resolution string
		seconds                               int
		price                                 float64
	}{
		{"max-to-h3-legacy", `{"model":"MiniMax-H3-Max","prompt":"waves","duration":4,"resolution":"2K"}`, "MiniMax-H3", "2k", 4, 1},
		{"max-to-h3-native", `{"model":"MiniMax-H3-Max","content":[{"type":"text","text":"waves"}],"ratio":"16:9","duration":4,"resolution":"2K"}`, "MiniMax-H3", "2k", 4, 1},
		{"h3-to-max", `{"model":"MiniMax-H3","prompt":"waves","duration":5,"resolution":"480P"}`, "MiniMax-H3-Max", "480p", 5, 0.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accounts := miniMaxProtocolAccounts("https://relay.example/v2")
			publicModel := gjson.Get(tc.body, "model").String()
			accounts[0].Credentials["model_mapping"] = map[string]any{publicModel: tc.upstreamModel, tc.upstreamModel: "must-not-map-twice"}
			h, slots, bindings, upstream := miniMaxProtocolPool(t, accounts)
			upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
				payload, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, tc.upstreamModel, gjson.GetBytes(payload, "model").String())
				require.Equal(t, int64(tc.seconds), gjson.GetBytes(payload, "duration").Int())
				assertMiniMaxNormalizedReservation(t, bindings, tc.seconds, tc.resolution, tc.price, id, service.VideoReservationPreparing)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"task_id":"known-model-mapped"}`))}, nil
			}
			c, w := miniMaxProtocolContext(context.Background(), "/v2/video_generation", tc.body)
			h.MiniMaxVideoTasks(c)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, 1, upstream.calls)
			slots.assertReleased(t)
		})
	}
}
