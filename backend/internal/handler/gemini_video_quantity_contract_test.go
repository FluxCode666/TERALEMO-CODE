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
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func geminiQuantityContext(parameters string) (*gin.Context, *httptest.ResponseRecorder) {
	body := `{"instances":[{"prompt":"offline waves"}]`
	if parameters != "" {
		body += `,"parameters":` + parameters
	}
	body += `}`
	c, w := miniMaxProtocolContext(context.Background(), "/v1beta/models/veo-public:predictLongRunning", body)
	c.Params = gin.Params{{Key: "modelAction", Value: "/veo-public:predictLongRunning"}}
	key, _ := middleware.GetAPIKeyFromContext(c)
	price := .02
	key.Group.VideoPrice720P = &price
	key.Group.VideoRateIndependent, key.Group.VideoRateMultiplier = true, 2
	return c, w
}

func geminiQuantityAccounts(model string) []service.Account {
	accounts := miniMaxProtocolAccounts("https://google.example/v1beta")
	accounts[0].Credentials["video_protocol"] = service.VideoProtocolGemini
	accounts[0].Credentials["model_mapping"] = map[string]any{"veo-public": model, model: "must-not-map-twice"}
	return accounts
}

func TestGeminiSampleCountFreezesAndSettlesEveryOutput(t *testing.T) {
	for count := 1; count <= 2; count++ {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			h, slots, bindings, up := miniMaxProtocolPool(t, geminiQuantityAccounts("veo-2.0-generate-001"))
			up.call = func(req *http.Request, accountID int64) (*http.Response, error) {
				require.Equal(t, int64(1), accountID)
				response := `{"name":"models/veo-2.0-generate-001/operations/multi"}`
				if req.Method == http.MethodPost {
					payload, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.Equal(t, int64(count), gjson.GetBytes(payload, "parameters.sampleCount").Int())
					require.Equal(t, int64(8), gjson.GetBytes(payload, "parameters.durationSeconds").Int())
					require.Equal(t, "720p", gjson.GetBytes(payload, "parameters.resolution").String())
					require.Equal(t, "/v1beta/models/veo-2.0-generate-001:predictLongRunning", req.URL.Path)
					require.Len(t, bindings.reservations.rows, 1)
					for _, reservation := range bindings.reservations.rows {
						var contract service.VideoPricingContract
						var snapshot service.VideoBillingSnapshot
						require.NoError(t, json.Unmarshal(reservation.Contract, &contract))
						require.NoError(t, json.Unmarshal(reservation.Snapshot, &snapshot))
						require.Equal(t, service.VideoReservationPreparing, reservation.State)
						require.Equal(t, count, contract.MaxVideoCount)
						require.Equal(t, count, snapshot.VideoCount)
						require.Equal(t, 8, contract.MaxDurationSeconds)
						require.Equal(t, 8.0, contract.RequestedDurationSeconds)
						require.Equal(t, "720p", contract.Resolution)
						require.InDelta(t, .32*float64(count), reservation.HoldAmount, 1e-8)
					}
				} else {
					outputs := make([]any, count)
					for i := range outputs {
						outputs[i] = map[string]any{"video": map[string]any{"uri": fmt.Sprintf("https://google.example/v1beta/files/result-%d:download", i)}}
					}
					encoded, err := json.Marshal(map[string]any{"name": "models/veo-2.0-generate-001/operations/multi", "done": true, "response": map[string]any{"generateVideoResponse": map[string]any{"generatedSamples": outputs}}})
					require.NoError(t, err)
					response = string(encoded)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
			}
			c, w := geminiQuantityContext(fmt.Sprintf(`{"sampleCount":%d}`, count))
			h.GeminiVideoTasks(c)
			require.Equal(t, 200, w.Code, w.Body.String())
			for i := 0; i < 2; i++ {
				c, w = geminiQuantityContext("")
				c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models/veo-2.0-generate-001/operations/multi", nil)
				c.Params = gin.Params{{Key: "model", Value: "veo-2.0-generate-001"}, {Key: "operation", Value: "multi"}}
				h.GeminiVideoTasks(c)
				require.Equal(t, 200, w.Code, w.Body.String())
			}
			require.Len(t, bindings.reservations.settlements, 1)
			for _, usage := range bindings.reservations.settlements {
				require.InDelta(t, .32*float64(count), usage.ActualCost, 1e-8)
				require.Equal(t, count, usage.VideoCount)
			}
			slots.assertReleased(t)
		})
	}
}

type geminiQuantityBalanceRepo struct {
	service.VideoBillingReservationRepository
	balance, attemptedHold float64
}

func (r *geminiQuantityBalanceRepo) Reserve(ctx context.Context, reservation *service.VideoBillingReservation) error {
	r.attemptedHold = reservation.HoldAmount
	if reservation.HoldAmount > r.balance {
		return service.ErrInsufficientBalance
	}
	return r.VideoBillingReservationRepository.Reserve(ctx, reservation)
}

func TestGeminiSampleCountRejectsInsufficientBalanceBeforeCreate(t *testing.T) {
	h, slots, bindings, up := miniMaxProtocolPool(t, geminiQuantityAccounts("veo-2.0-generate-001"))
	repo := &geminiQuantityBalanceRepo{VideoBillingReservationRepository: bindings.reservations, balance: .40}
	h.gatewayService.SetVideoBillingReservationRepository(repo)
	c, w := geminiQuantityContext(`{"sampleCount":2,"durationSeconds":8,"resolution":"720p"}`)
	key, _ := middleware.GetAPIKeyFromContext(c)
	key.User.Balance = .40
	h.GeminiVideoTasks(c)
	requireGeminiVideoError(t, w, 402)
	require.InDelta(t, .64, repo.attemptedHold, 1e-8)
	require.Zero(t, up.calls)
	require.Empty(t, bindings.reservations.rows)
	require.Empty(t, bindings.pending)
	slots.assertReleased(t)
}

func TestGeminiSampleCountInvalidRequestsNeverReachUpstream(t *testing.T) {
	for _, tc := range []struct{ name, model, parameters string }{
		{"veo2-three-conservative-limit", "veo-2.0-generate-001", `{"sampleCount":3}`},
		{"veo2-four-conservative-limit", "veo-2.0-generate-001", `{"sampleCount":4}`},
		{"veo2-five", "veo-2.0-generate-001", `{"sampleCount":5}`},
		{"veo3-two-W6", "veo-3.0-generate-001", `{"sampleCount":2,"durationSeconds":8,"resolution":"720p"}`},
		{"veo31-two", "veo-3.1-generate-preview", `{"sampleCount":2}`},
		{"fraction", "veo-2.0-generate-001", `{"sampleCount":1.5}`},
		{"zero", "veo-2.0-generate-001", `{"sampleCount":0}`},
		{"string", "veo-2.0-generate-001", `{"sampleCount":"2"}`},
		{"null", "veo-2.0-generate-001", `{"sampleCount":null}`},
		{"fraction-duration", "veo-2.0-generate-001", `{"durationSeconds":7.9}`},
		{"veo2-too-long", "veo-2.0-generate-001", `{"durationSeconds":9}`},
		{"veo2-1080p", "veo-2.0-generate-001", `{"resolution":"1080p"}`},
		{"4k-unpriced", "veo-3.1-generate-preview", `{"resolution":"4k"}`},
		{"sdk-count-alias", "veo-2.0-generate-001", `{"numberOfVideos":2}`},
		{"duplicate-key", "veo-2.0-generate-001", `{"sampleCount":1,"sampleCount":4}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, slots, bindings, up := miniMaxProtocolPool(t, geminiQuantityAccounts(tc.model))
			c, w := geminiQuantityContext(tc.parameters)
			h.GeminiVideoTasks(c)
			requireGeminiVideoError(t, w, 400)
			require.Zero(t, up.calls)
			require.Empty(t, bindings.reservations.rows)
			require.Empty(t, bindings.pending)
			slots.assertReleased(t)
		})
	}
	t.Run("multiple-instances", func(t *testing.T) {
		h, slots, bindings, up := miniMaxProtocolPool(t, geminiQuantityAccounts("veo-2.0-generate-001"))
		c, w := geminiQuantityContext("")
		c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/veo-public:predictLongRunning", strings.NewReader(`{"instances":[{"prompt":"one"},{"prompt":"two"}],"parameters":{"sampleCount":1}}`))
		h.GeminiVideoTasks(c)
		requireGeminiVideoError(t, w, 400)
		require.Zero(t, up.calls)
		require.Empty(t, bindings.reservations.rows)
		slots.assertReleased(t)
	})
}

func TestGeminiSubmissionWritesExplicitDefaultsBeforeReserve(t *testing.T) {
	for _, model := range []string{"veo-2.0-generate-001", "veo-3.0-generate-001", "veo-3.1-generate-preview"} {
		t.Run(model, func(t *testing.T) {
			h, slots, bindings, up := miniMaxProtocolPool(t, geminiQuantityAccounts(model))
			up.call = func(req *http.Request, _ int64) (*http.Response, error) {
				payload, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, int64(1), gjson.GetBytes(payload, "parameters.sampleCount").Int())
				require.Equal(t, int64(8), gjson.GetBytes(payload, "parameters.durationSeconds").Int())
				require.Equal(t, "720p", gjson.GetBytes(payload, "parameters.resolution").String())
				for _, reservation := range bindings.reservations.rows {
					var contract service.VideoPricingContract
					require.NoError(t, json.Unmarshal(reservation.Contract, &contract))
					require.Equal(t, 1, contract.MaxVideoCount)
					require.Equal(t, 8, contract.MaxDurationSeconds)
					require.Equal(t, "720p", contract.Resolution)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"name":"models/%s/operations/defaults"}`, model)))}, nil
			}
			c, w := geminiQuantityContext("")
			h.GeminiVideoTasks(c)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, 1, up.calls)
			require.Len(t, bindings.reservations.rows, 1)
			slots.assertReleased(t)
		})
	}
}
