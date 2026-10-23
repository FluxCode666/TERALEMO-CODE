//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func miniMaxAcceptedBillingFixture(t *testing.T) (*OpenAIGatewayService, *videoTaskBillingFake, *VideoBillingReservation, *Account) {
	t.Helper()
	svc, repo, input := videoTaskServiceFixture(t)
	input.Protocol, input.Endpoint = VideoProtocolMiniMax, MiniMaxVideoEndpointCreate
	input.Account = miniMaxVideoTestAccount()
	input.Account.Credentials["base_url"] = "https://api.minimax.io/v1"
	input.InboundEndpoint, input.UpstreamEndpoint = "/v1/video_generation", "/v1/video_generation"
	v, err := svc.ReserveVideoTask(context.Background(), input)
	require.NoError(t, err)
	require.NoError(t, svc.AcceptVideoTask(context.Background(), v, "minimax:task-test"))
	return svc, repo, v, input.Account
}

func TestMiniMaxVideoSuccessSettlesBeforeFileLookupFailure(t *testing.T) {
	for _, background := range []bool{false, true} {
		t.Run(map[bool]string{false: "native", true: "background"}[background], func(t *testing.T) {
			svc, repo, v, account := miniMaxAcceptedBillingFixture(t)
			status := `{"task_id":"task-test","status":"Success","file_id":"file-test","base_resp":{"status_code":0}}`
			upstream := &grokMediaContentUpstreamStub{responses: []*http.Response{
				grokMediaContentStatusResponse(status),
				{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": {"23"}}, Body: io.NopCloser(strings.NewReader("private diagnostic"))},
				grokMediaContentStatusResponse(status),
				grokMediaContentStatusResponse(`{"file":{"file_id":"file-test","download_url":"https://cdn.example/video.mp4"},"base_resp":{"status_code":0}}`),
			}}
			svc.httpUpstream, svc.accountRepo = upstream, &videoBoundAccountFake{account: account}
			c, response := grokMediaContentTestContext(http.MethodGet, "/v1/query/video_generation?task_id=task-test", nil)
			if background {
				svc.reconcileVideoTask(context.Background(), "test-worker", v)
			} else {
				result, err := svc.ForwardMiniMaxVideo(context.Background(), c, account, MiniMaxVideoEndpointStatus, v.TaskID, nil)
				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, http.StatusServiceUnavailable, response.Code)
				require.Equal(t, "23", response.Header().Get("Retry-After"))
				require.JSONEq(t, `{"error":{"type":"upstream_error","message":"MiniMax upstream request failed"}}`, response.Body.String())
			}
			require.Len(t, repo.settlements, 1)
			require.InDelta(t, .6, repo.settlements[0].Usage.ActualCost, 1e-12)
			require.Equal(t, "/v1/video_generation", *repo.settlements[0].Usage.InboundEndpoint)
			require.Equal(t, "/v1/video_generation", *repo.settlements[0].Usage.UpstreamEndpoint)
			stored, err := repo.Get(context.Background(), v.ID)
			require.NoError(t, err)
			require.Equal(t, VideoReservationSettled, stored.State)
			require.Equal(t, "file-test", stored.FileID)

			c, response = grokMediaContentTestContext(http.MethodGet, "/v1/query/video_generation?task_id=task-test", nil)
			result, err := svc.ForwardMiniMaxVideo(context.Background(), c, account, MiniMaxVideoEndpointStatus, v.TaskID, nil)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, "https://cdn.example/video.mp4", result.AsyncTaskURL)
			require.Len(t, repo.settlements, 1, "a successful file retry must not capture a second payment")
			require.Len(t, upstream.requests, 4)
		})
	}
}

func TestMiniMaxVideoSuccessSettlementFailureRetriesBeforeFileLookup(t *testing.T) {
	svc, repo, v, account := miniMaxAcceptedBillingFixture(t)
	repo.settleFailures = 1
	status := `{"task_id":"task-test","status":"Success","file_id":"file-test","base_resp":{"status_code":0}}`
	upstream := &grokMediaContentUpstreamStub{responses: []*http.Response{
		grokMediaContentStatusResponse(status), grokMediaContentStatusResponse(status),
		grokMediaContentStatusResponse(`{"file":{"file_id":"file-test","download_url":"https://cdn.example/video.mp4"},"base_resp":{"status_code":0}}`),
	}}
	svc.httpUpstream = upstream
	c, _ := grokMediaContentTestContext(http.MethodGet, "/v1/query/video_generation?task_id=task-test", nil)
	_, err := svc.ForwardMiniMaxVideo(context.Background(), c, account, MiniMaxVideoEndpointStatus, v.TaskID, nil)
	require.ErrorContains(t, err, "injected transaction failure")
	require.Empty(t, repo.settlements)
	require.Len(t, upstream.requests, 1, "failed payment must remain retryable before fetching file metadata")
	stored, err := repo.Get(context.Background(), v.ID)
	require.NoError(t, err)
	require.Equal(t, VideoReservationAccepted, stored.State)

	c, _ = grokMediaContentTestContext(http.MethodGet, "/v1/query/video_generation?task_id=task-test", nil)
	result, err := svc.ForwardMiniMaxVideo(context.Background(), c, account, MiniMaxVideoEndpointStatus, v.TaskID, nil)
	require.NoError(t, err)
	require.Equal(t, "completed", result.AsyncTaskStatus)
	require.Len(t, repo.settlements, 1)
	require.InDelta(t, .6, repo.settlements[0].Usage.ActualCost, 1e-12)
	require.Len(t, upstream.requests, 3)
}
