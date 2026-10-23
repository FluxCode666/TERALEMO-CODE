//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestMiniMaxV1Legacy512PUsesItsExplicit480PBillingAlias(t *testing.T) {
	accounts := miniMaxProtocolAccounts("https://minimax.example/v1")
	h, slots, bindings, upstream := miniMaxProtocolPool(t, accounts)
	upstream.call = func(req *http.Request, accountID int64) (*http.Response, error) {
		payload, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Equal(t, "512P", gjson.GetBytes(payload, "resolution").String())
		require.EqualValues(t, 6, gjson.GetBytes(payload, "duration").Int())
		assertMiniMaxNormalizedReservation(t, bindings, 6, "480p", .5, accountID, service.VideoReservationPreparing)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"task_id":"legacy-512","base_resp":{"status_code":0}}`))}, nil
	}
	c, w := miniMaxProtocolContext(context.Background(), "/v1/video_generation", `{"model":"MiniMax-Hailuo-2.3","prompt":"waves","resolution":"512P"}`)
	h.MiniMaxVideoTasks(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 1, upstream.calls)
	slots.assertReleased(t)
}
