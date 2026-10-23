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

func TestNativeVideoDownloadCapturesDurableBillingAcrossProtocols(t *testing.T) {
	for _, protocol := range []string{VideoProtocolSeedance, VideoProtocolGemini, VideoProtocolMiniMax} {
		t.Run(protocol, func(t *testing.T) {
			svc, repo, input := videoTaskServiceFixture(t)
			input.Protocol = protocol
			input.Account.Platform = PlatformVideo
			input.Account.Credentials["video_protocol"] = protocol
			input.Endpoint = SeedanceEndpointCreate
			id, status := "seedance:task-test", `{"id":"task-test","status":"succeeded","duration":3,"content":{"video_url":"https://cdn.example/video.mp4"}}`
			wantCost := .3
			if protocol == VideoProtocolGemini {
				input.Endpoint, input.Account.Credentials["base_url"] = GeminiVideoEndpointCreate, "https://google.example/v1beta"
				id, status, wantCost = "gemini:models/veo/operations/task-test", `{"name":"models/veo/operations/task-test","done":true,"response":{"generatedVideos":[{"video":{"uri":"https://google.example/video.mp4"}}]}}`, .6
			} else if protocol == VideoProtocolMiniMax {
				input.Endpoint, input.Account.Credentials["base_url"] = MiniMaxVideoEndpointCreate, "https://metaso.cn/api/minimax/v2"
				id, status, wantCost = "minimax:task-test", `{"task":{"id":"task-test","status":"succeeded","content":{"url":"https://files.metaso.cn/video.mp4"}}}`, .6
			}
			v, err := svc.ReserveVideoTask(context.Background(), input)
			require.NoError(t, err)
			require.NoError(t, svc.AcceptVideoTask(context.Background(), v, id))
			svc.cache = &schedulerTestGatewayCache{}
			svc.accountRepo = &videoBoundAccountFake{account: input.Account}
			svc.httpUpstream = &grokMediaContentUpstreamStub{responses: []*http.Response{
				grokMediaContentStatusResponse(status),
				{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"video/mp4"}}, Body: io.NopCloser(strings.NewReader("video")), ContentLength: 5},
			}}
			require.NoError(t, svc.BindGrokMediaVideoRequestAccount(context.Background(), input.APIKey.GroupID, id, input.APIKey.UserID, input.APIKey.ID, input.Account.ID))
			task := &AsyncTaskRecord{AsyncTask: AsyncTask{Protocol: protocol, GroupID: input.APIKey.GroupID, APIKeyID: input.APIKey.ID}, UserID: input.APIKey.UserID, RequestID: id}
			task.GroupID = i64p(999) // A post-submission group change cannot change routing.
			c, response := grokMediaContentTestContext(http.MethodGet, "/api/v1/user/async-tasks/one/content", nil)
			require.NoError(t, svc.ForwardNativeVideoContent(context.Background(), c, task, 0))
			require.Equal(t, "video", response.Body.String())
			require.Len(t, repo.settlements, 1)
			require.InDelta(t, wantCost, repo.settlements[0].Usage.ActualCost, 1e-12)
		})
	}
}

func TestNativeVideoDownloadFailedTaskReleasesDurableHold(t *testing.T) {
	svc, repo, input := videoTaskServiceFixture(t)
	v := acceptVideoFixture(t, svc, input)
	svc.cache = &schedulerTestGatewayCache{}
	svc.accountRepo = &videoBoundAccountFake{account: input.Account}
	svc.httpUpstream = &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"id":"task-test","status":"failed","error":{"message":"filtered"}}`)}
	require.NoError(t, svc.BindGrokMediaVideoRequestAccount(context.Background(), input.APIKey.GroupID, v.TaskID, v.UserID, v.APIKeyID, v.AccountID))
	task := &AsyncTaskRecord{AsyncTask: AsyncTask{Protocol: VideoProtocolSeedance, GroupID: input.APIKey.GroupID, APIKeyID: v.APIKeyID}, UserID: v.UserID, RequestID: v.TaskID}
	c, _ := grokMediaContentTestContext(http.MethodGet, "/api/v1/user/async-tasks/one/content", nil)
	require.ErrorContains(t, svc.ForwardNativeVideoContent(context.Background(), c, task, 0), "video is not available")
	require.Len(t, repo.releases, 1)
	require.Empty(t, repo.settlements)
}

func TestNativeVideoDownloadNeverServesAfterSettlementError(t *testing.T) {
	svc, repo, input := videoTaskServiceFixture(t)
	v := acceptVideoFixture(t, svc, input)
	repo.settleFailures = 1
	svc.cache = &schedulerTestGatewayCache{}
	svc.accountRepo = &videoBoundAccountFake{account: input.Account}
	upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"id":"task-test","status":"succeeded","duration":3,"content":{"video_url":"https://cdn.example/video.mp4"}}`)}
	svc.httpUpstream = upstream
	require.NoError(t, svc.BindGrokMediaVideoRequestAccount(context.Background(), input.APIKey.GroupID, v.TaskID, v.UserID, v.APIKeyID, v.AccountID))
	task := &AsyncTaskRecord{AsyncTask: AsyncTask{Protocol: VideoProtocolSeedance, GroupID: input.APIKey.GroupID, APIKeyID: v.APIKeyID}, UserID: v.UserID, RequestID: v.TaskID}
	c, _ := grokMediaContentTestContext(http.MethodGet, "/api/v1/user/async-tasks/one/content", nil)
	require.ErrorContains(t, svc.ForwardNativeVideoContent(context.Background(), c, task, 0), "injected transaction failure")
	require.Len(t, upstream.requests, 1, "settlement retry must precede serving downloadable bytes")
	require.Empty(t, repo.settlements)
}

func TestDirectVideoDownloadCapturesDurableBillingBeforeServing(t *testing.T) {
	for _, kind := range []string{"gemini", "minimax-v1", "minimax-v2", "minimax-file"} {
		t.Run(kind, func(t *testing.T) {
			svc, repo, input := videoTaskServiceFixture(t)
			input.Protocol, input.Endpoint = VideoProtocolMiniMax, MiniMaxVideoEndpointCreate
			input.Account.Platform = PlatformVideo
			input.Account.Credentials["video_protocol"] = VideoProtocolMiniMax
			input.Account.Credentials["base_url"] = "https://api.minimax.io/v1"
			taskID := "minimax:task-test"
			status := `{"task_id":"task-test","status":"Success","file_id":"file-test","base_resp":{"status_code":0}}`
			responses := []*http.Response{grokMediaContentStatusResponse(status), grokMediaContentStatusResponse(`{"file":{"file_id":"file-test","download_url":"https://cdn.example/video.mp4"},"base_resp":{"status_code":0}}`)}
			if kind == "minimax-v2" {
				input.Account.Credentials["base_url"] = "https://metaso.cn/api/minimax/v2"
				responses = []*http.Response{grokMediaContentStatusResponse(`{"task":{"id":"task-test","status":"succeeded","content":{"url":"https://files.metaso.cn/video.mp4"}}}`)}
			} else if kind == "gemini" {
				input.Protocol, input.Endpoint = VideoProtocolGemini, GeminiVideoEndpointCreate
				input.Account.Credentials["video_protocol"] = VideoProtocolGemini
				input.Account.Credentials["base_url"] = "https://google.example/v1beta"
				taskID = "gemini:models/veo/operations/task-test"
				responses = []*http.Response{grokMediaContentStatusResponse(`{"name":"models/veo/operations/task-test","done":true,"response":{"generatedVideos":[{"video":{"uri":"https://google.example/video.mp4"}}]}}`)}
			}
			v, err := svc.ReserveVideoTask(context.Background(), input)
			require.NoError(t, err)
			require.NoError(t, svc.AcceptVideoTask(context.Background(), v, taskID))
			if kind == "minimax-file" {
				require.NoError(t, repo.BindFile(context.Background(), v.ID, "file-test"))
				taskID = "minimax-file:file-test"
				// The reconciliation observation also refreshes v1 file metadata;
				// the file-only download performs its own fresh signed-URL lookup.
				responses = append(responses, grokMediaContentStatusResponse(`{"file":{"file_id":"file-test","download_url":"https://cdn.example/video.mp4"},"base_resp":{"status_code":0}}`))
			}
			responses = append(responses, &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"video/mp4"}}, Body: io.NopCloser(strings.NewReader("video")), ContentLength: 5})
			upstream := &grokMediaContentUpstreamStub{responses: responses}
			svc.cache, svc.accountRepo, svc.httpUpstream = &schedulerTestGatewayCache{}, &videoBoundAccountFake{account: input.Account}, upstream
			require.NoError(t, svc.BindGrokMediaVideoRequestAccount(context.Background(), input.APIKey.GroupID, taskID, v.UserID, v.APIKeyID, v.AccountID))
			task := &AsyncTaskRecord{AsyncTask: AsyncTask{Protocol: input.Protocol, GroupID: input.APIKey.GroupID, APIKeyID: v.APIKeyID, ResultURL: "https://google.example/video.mp4"}, UserID: v.UserID, RequestID: taskID}
			task.GroupID = i64p(999) // Direct downloads also use frozen account identity.
			c, response := grokMediaContentTestContext(http.MethodGet, "/api/v1/user/async-tasks/one/content", nil)
			switch kind {
			case "gemini":
				err = svc.ForwardGeminiVideoContent(context.Background(), c, task)
			case "minimax-file":
				err = svc.ForwardNativeVideoContent(context.Background(), c, task, 0)
			default:
				err = svc.ForwardMiniMaxVideoContent(context.Background(), c, task)
			}
			require.NoError(t, err)
			require.Equal(t, "video", response.Body.String())
			require.Len(t, repo.settlements, 1)
			require.InDelta(t, .6, repo.settlements[0].Usage.ActualCost, 1e-12)
		})
	}
}

func TestDurableVideoDownloadRejectsAnotherUserOrKeyBeforeUpstream(t *testing.T) {
	for _, identity := range []string{"user", "key"} {
		t.Run(identity, func(t *testing.T) {
			svc, _, input := videoTaskServiceFixture(t)
			v := acceptVideoFixture(t, svc, input)
			svc.accountRepo = &videoBoundAccountFake{account: input.Account}
			upstream := &grokMediaContentUpstreamStub{}
			svc.httpUpstream = upstream
			task := &AsyncTaskRecord{AsyncTask: AsyncTask{Protocol: v.Protocol, APIKeyID: v.APIKeyID}, UserID: v.UserID, RequestID: v.TaskID}
			if identity == "user" {
				task.UserID++
			} else {
				task.APIKeyID++
			}
			c, _ := grokMediaContentTestContext(http.MethodGet, "/api/v1/user/async-tasks/one/content", nil)
			require.ErrorContains(t, svc.ForwardNativeVideoContent(context.Background(), c, task, 0), "task account is unavailable")
			require.Empty(t, upstream.requests)
		})
	}
}
