//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNativeVideoCanonicalControlsMatchFrozenContract(t *testing.T) {
	for _, tc := range []struct {
		name, model, body, resolution string
		grok                          bool
		duration, maxDuration, frames int
		exact                         float64
	}{
		{"Seedance defaults", "doubao-seedance-1-0-pro-250528", `{"model":"public-video","content":[{"type":"text","text":"waves"}]}`, "720p", false, 5, 5, 0, 5},
		{"Seedance weak controls", "doubao-seedance-1-0-pro-250528", `{"model":"public-video","content":[{"type":"text","text":"waves --dur 7 --rs 1080p"}]}`, "1080p", false, 7, 7, 0, 7},
		{"Seedance frames override duration", "doubao-seedance-1-0-pro-250528", `{"model":"public-video","content":[{"type":"text","text":"waves"}],"duration":30,"frames":49,"resolution":"480p"}`, "480p", false, 0, 3, 49, 49.0 / 24},
		{"Seedance smart bounded", "doubao-seedance-2-5-pro", `{"model":"public-video","content":[{"type":"text","text":"waves"}],"duration":-1,"resolution":"720p"}`, "720p", false, -1, 30, 0, 0},
		{"Grok defaults", "grok-imagine-video", `{"model":"public-video","prompt":"waves"}`, "480p", true, 8, 8, 0, 8},
		{"Grok seconds alias", "grok-imagine-video", `{"model":"public-video","prompt":"waves","seconds":"13","resolution":"720p","n":1}`, "720p", true, 13, 13, 0, 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accounts := miniMaxProtocolAccounts("https://video.example")
			accounts[0].Credentials["video_protocol"] = service.VideoProtocolSeedance
			accounts[0].Credentials["model_mapping"] = map[string]any{"public-video": tc.model}
			if tc.grok {
				accounts[0].Platform = service.PlatformGrok
			}
			h, slots, bindings, upstream := miniMaxProtocolPool(t, accounts)
			upstream.call = func(req *http.Request, _ int64) (*http.Response, error) {
				payload, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, strings.ToLower(tc.resolution), strings.ToLower(gjson.GetBytes(payload, "resolution").String()))
				if tc.frames > 0 {
					require.EqualValues(t, tc.frames, gjson.GetBytes(payload, "frames").Int())
					require.False(t, gjson.GetBytes(payload, "duration").Exists())
				} else {
					require.EqualValues(t, tc.duration, gjson.GetBytes(payload, "duration").Int())
				}
				if tc.grok {
					require.False(t, gjson.GetBytes(payload, "seconds").Exists())
					require.False(t, gjson.GetBytes(payload, "n").Exists())
				}
				require.Len(t, bindings.reservations.rows, 1)
				for _, reservation := range bindings.reservations.rows {
					var contract service.VideoPricingContract
					var snapshot service.VideoBillingSnapshot
					require.NoError(t, json.Unmarshal(reservation.Contract, &contract))
					require.NoError(t, json.Unmarshal(reservation.Snapshot, &snapshot))
					require.Equal(t, service.VideoReservationPreparing, reservation.State)
					require.Equal(t, tc.maxDuration, contract.MaxDurationSeconds)
					require.InDelta(t, tc.exact, contract.RequestedDurationSeconds, 1e-9)
					require.Equal(t, tc.frames, snapshot.RequestedFrames)
					require.Equal(t, tc.resolution, contract.Resolution)
					require.Equal(t, 1, contract.MaxVideoCount)
					require.InDelta(t, float64(tc.maxDuration)*.1, reservation.HoldAmount, 1e-9)
				}
				response := `{"id":"controls"}`
				if tc.grok {
					response = `{"request_id":"controls"}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
			}
			c, w := miniMaxProtocolContext(context.Background(), "/videos", tc.body)
			key, _ := middleware.GetAPIKeyFromContext(c)
			price := .1
			key.Group.VideoPrice480P, key.Group.VideoPrice720P, key.Group.VideoPrice1080P = &price, &price, &price
			if tc.grok {
				key.Group.Platform = service.PlatformGrok
				h.GrokVideoGeneration(c)
			} else {
				h.handleGrokMedia(c, service.SeedanceEndpointCreate, "")
			}
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, 1, upstream.calls)
			slots.assertReleased(t)
		})
	}
}
