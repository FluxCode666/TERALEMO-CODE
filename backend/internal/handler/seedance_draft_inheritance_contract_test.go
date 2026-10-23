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

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const draftFinalBody = `{"model":"public-video","content":[{"type":"draft_task","draft_task":{"id":"draft-1"}}],"resolution":"1080p"}`

func draftInheritanceContext() (*gin.Context, *httptest.ResponseRecorder) {
	c, w := miniMaxProtocolContext(context.Background(), "/api/v3/contents/generations/tasks", draftFinalBody)
	key, _ := middleware.GetAPIKeyFromContext(c)
	low, high := .1, .2
	key.Group.VideoPrice480P, key.Group.VideoPrice1080P = &low, &high
	return c, w
}

func draftInheritanceFixture(t *testing.T) (*OpenAIGatewayHandler, *grokMediaSlotsCache, *grokMediaSlotBindings, *grokMediaSlotUpstream, *service.VideoBillingReservation) {
	t.Helper()
	accounts := miniMaxProtocolAccounts("https://ark.example/api/v3", "https://ark.example/api/v3")
	for i := range accounts {
		accounts[i].Credentials["video_protocol"] = service.VideoProtocolSeedance
		accounts[i].Credentials["model_mapping"] = map[string]any{"public-video": "doubao-seedance-2-5-pro"}
	}
	h, slots, bindings, upstream := miniMaxProtocolPool(t, accounts)
	c, _ := draftInheritanceContext()
	key, _ := middleware.GetAPIKeyFromContext(c)
	// Use account 2 as the source even though the scheduler prefers account 1.
	// The inheritance path must bind every source GET and new POST to account 2.
	source, err := h.gatewayService.ReserveVideoTask(context.Background(), service.ReserveVideoTaskInput{
		APIKey: key, Account: &accounts[1], Protocol: service.VideoProtocolSeedance, Endpoint: service.SeedanceEndpointCreate,
		Model: "public-video", Resolution: "480p", DurationSeconds: -1, MaxDurationSeconds: 30, VideoCount: 1,
		InboundEndpoint: EndpointSeedanceTasks, UpstreamEndpoint: EndpointSeedanceTasks,
	})
	require.NoError(t, err)
	require.NoError(t, h.gatewayService.AcceptVideoTask(context.Background(), source, "seedance:draft-1"))
	return h, slots, bindings, upstream, source
}

func TestSeedanceDraftInheritanceFreezesFinal1080PAndActualDuration(t *testing.T) {
	h, slots, bindings, upstream, source := draftInheritanceFixture(t)
	gets, posts := 0, 0
	upstream.call = func(req *http.Request, accountID int64) (*http.Response, error) {
		require.EqualValues(t, 2, accountID, "both operations must use the creating account")
		body := `{"id":"draft-1","model":"doubao-seedance-2-5-pro","status":"succeeded","draft":true,"resolution":"480p","duration":7}`
		if req.Method == http.MethodGet {
			gets++
			require.Equal(t, "/api/v3/contents/generations/tasks/draft-1", req.URL.Path)
		} else {
			posts++
			payload, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Equal(t, "doubao-seedance-2-5-pro", gjson.GetBytes(payload, "model").String())
			require.Equal(t, "draft-1", gjson.GetBytes(payload, "content.0.draft_task.id").String())
			require.Equal(t, "1080p", gjson.GetBytes(payload, "resolution").String())
			for _, inherited := range []string{"duration", "frames", "ratio", "seed", "generate_audio", "omni_reference_task_type"} {
				require.False(t, gjson.GetBytes(payload, inherited).Exists(), "Ark forbids resubmitting inherited %s", inherited)
			}
			require.Len(t, bindings.reservations.rows, 2, "new contract must exist before the paid create")
			for id, reservation := range bindings.reservations.rows {
				if id == source.ID {
					continue
				}
				var contract service.VideoPricingContract
				require.NoError(t, json.Unmarshal(reservation.Contract, &contract))
				require.EqualValues(t, 2, reservation.AccountID)
				require.Equal(t, "public-video", contract.Model)
				require.Equal(t, "1080p", contract.Resolution)
				require.Equal(t, 7, contract.MaxDurationSeconds)
				require.Equal(t, 7.0, contract.RequestedDurationSeconds)
				require.Equal(t, 1, contract.MaxVideoCount)
				require.InDelta(t, 1.4, reservation.HoldAmount, 1e-9)
			}
			body = `{"id":"final-1","status":"queued"}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	c, w := draftInheritanceContext()
	h.SeedanceTasks(c)
	require.Equal(t, 1, gets, w.Body.String())
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 1, posts)
	final, err := bindings.reservations.GetByTask(context.Background(), service.VideoProtocolSeedance, "seedance:final-1")
	require.NoError(t, err)
	require.Equal(t, service.VideoReservationAccepted, final.State)
	unchanged, err := bindings.reservations.Get(context.Background(), source.ID)
	require.NoError(t, err)
	require.Equal(t, source.Contract, unchanged.Contract, "source tariff is immutable")
	slots.assertReleased(t)
}

func TestSeedanceDraftInheritanceHidesUnknownAndOtherOwnersBeforeUpstream(t *testing.T) {
	var commonResponse string
	for _, scenario := range []string{"unknown", "user", "key", "group"} {
		t.Run(scenario, func(t *testing.T) {
			h, slots, bindings, upstream, _ := draftInheritanceFixture(t)
			c, w := draftInheritanceContext()
			key, _ := middleware.GetAPIKeyFromContext(c)
			switch scenario {
			case "unknown":
				c.Request = httptest.NewRequest(http.MethodPost, c.Request.URL.Path, strings.NewReader(strings.Replace(draftFinalBody, "draft-1", "unknown-draft", 1)))
				c.Request.Header.Set("Content-Type", "application/json")
			case "user":
				key.UserID, key.User.ID = 11, 11
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 11, Concurrency: 5})
			case "key":
				key.ID = 21
			case "group":
				groupID := int64(25)
				key.GroupID, key.Group.ID = &groupID, groupID
			}
			h.SeedanceTasks(c)
			require.Equal(t, 400, w.Code, w.Body.String())
			if commonResponse == "" {
				commonResponse = w.Body.String()
			}
			require.Equal(t, commonResponse, w.Body.String(), "task existence must not leak across identities")
			require.Zero(t, upstream.calls)
			require.Len(t, bindings.reservations.rows, 1, "only the untouched source row may remain")
			require.Empty(t, bindings.pending)
			slots.assertReleased(t)
		})
	}
}
