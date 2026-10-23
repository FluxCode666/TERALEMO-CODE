//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestEnterpriseAsyncVideoSubmissionAllowed(t *testing.T) {
	for _, endpoint := range []string{"generate", "edit", "extend", "seedance", "gemini", "minimax-v1", "minimax-v2"} {
		t.Run(endpoint, func(t *testing.T) {
			platform, protocol, baseURL := service.PlatformGrok, "grok", "https://video.example/v1"
			if endpoint == "seedance" {
				platform, protocol = service.PlatformVideo, service.VideoProtocolSeedance
			}
			if endpoint == "gemini" {
				platform, protocol = service.PlatformVideo, service.VideoProtocolGemini
			}
			if strings.HasPrefix(endpoint, "minimax") {
				platform, protocol = service.PlatformVideo, service.VideoProtocolMiniMax
			}
			if endpoint == "minimax-v2" {
				baseURL = "https://video.example/v2"
			}
			h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false, platform, protocol, baseURL)
			c, w := grokMediaSlotContext(context.Background(), true)
			key, _ := middleware.GetAPIKeyFromContext(c)
			key.Group.Platform = platform
			key.User.Enterprise = &service.EnterpriseMemberContext{EnterpriseID: 7, MemberID: 9, EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive}
			body, response := `{"model":"grok-imagine-video","prompt":"waves","duration":6}`, `{"request_id":"enterprise-task","status":"pending"}`
			path := "/v1/videos/generations"
			switch endpoint {
			case "seedance":
				body, response, path = `{"model":"doubao-seedance-1-0-pro-250528","content":[{"type":"text","text":"waves"}],"duration":6}`, `{"id":"enterprise-task","status":"queued"}`, "/api/v3/contents/generations/tasks"
			case "gemini":
				body, response, path = `{"instances":[{"prompt":"waves"}],"parameters":{"durationSeconds":8}}`, `{"name":"models/veo/operations/enterprise-task"}`, "/v1beta/models/veo:predictLongRunning"
				c.Params = gin.Params{{Key: "modelAction", Value: "/veo:predictLongRunning"}}
			case "minimax-v1":
				body, response, path = `{"model":"MiniMax-Hailuo-2.3","prompt":"waves","duration":6}`, `{"task_id":"enterprise-task","base_resp":{"status_code":0}}`, "/v1/video_generation"
			case "minimax-v2":
				body, response, path = `{"model":"MiniMax-H3","content":[{"type":"text","text":"waves"}],"duration":6,"ratio":"16:9"}`, `{"task_id":"enterprise-task"}`, "/v2/video_generation"
			}
			upstream.call = func(*http.Request, int64) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
			}
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			switch endpoint {
			case "generate":
				h.GrokVideoGeneration(c)
			case "edit":
				h.GrokVideoEdit(c)
			case "extend":
				h.GrokVideoExtension(c)
			case "seedance":
				h.SeedanceTasks(c)
			case "gemini":
				h.GeminiVideoTasks(c)
			default:
				h.MiniMaxVideoTasks(c)
			}
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, 1, upstream.calls)
			require.Len(t, bindings.reservations.rows, 1)
			for _, reservation := range bindings.reservations.rows {
				require.Equal(t, int64(7), reservation.EnterpriseID)
				require.Equal(t, int64(9), reservation.EnterpriseMemberID)
				require.Equal(t, service.VideoReservationAccepted, reservation.State)
			}
			slots.assertReleased(t)
		})
	}
}

func TestAsyncVideoHistoricalPersonalPayerSnapshot(t *testing.T) {
	key := &service.APIKey{ID: 2, UserID: 42, User: &service.User{ID: 42, Balance: 123, Enterprise: &service.EnterpriseMemberContext{EnterpriseID: 7, MemberID: 9, EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive}}}
	frozen := personalAsyncVideoBillingKey(key)
	require.NotSame(t, key, frozen)
	require.Nil(t, frozen.User.Enterprise)
	require.Equal(t, int64(42), frozen.User.ID)
	require.Equal(t, 123.0, frozen.User.Balance)
	key.User.Balance = 999
	require.Equal(t, 123.0, frozen.User.Balance, "settlement owns its original personal snapshot")
	require.NotNil(t, key.User.Enterprise, "authorization snapshot must remain unchanged")
}

func TestEnterpriseMemberCanPollHistoricalPersonalVideo(t *testing.T) {
	h, slots, _, upstream := newGrokMediaSlotHandler(t, false, false)
	c, w := grokMediaSlotContext(context.Background(), false)
	key, _ := middleware.GetAPIKeyFromContext(c)
	key.User.Enterprise = &service.EnterpriseMemberContext{EnterpriseID: 7, MemberID: 9, EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive}
	h.GrokVideoStatus(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 1, upstream.calls)
	require.NotNil(t, key.User.Enterprise)
	slots.assertReleased(t)
}

type historicalVideoBillingRepo struct {
	service.UsageBillingRepository
	commands chan *service.UsageBillingCommand
}

func (r historicalVideoBillingRepo) Apply(_ context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	r.commands <- cmd
	return &service.UsageBillingApplyResult{Applied: true}, nil
}

func TestHistoricalAsyncVideoCompletionChargesPersonalWalletAfterJoiningEnterprise(t *testing.T) {
	for _, provider := range []string{"grok", "seedance"} {
		t.Run(provider, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Default.RateMultiplier = 1
			logs := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 1)}
			billing := historicalVideoBillingRepo{commands: make(chan *service.UsageBillingCommand, 1)}
			gateway := service.NewOpenAIGatewayService(nil, logs, billing, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, nil, service.NewDeferredService(nil, nil, time.Second), nil, nil, nil, nil, nil, nil, nil)
			h := &OpenAIGatewayHandler{gatewayService: gateway, cfg: cfg}
			c, _ := grokMediaSlotContext(context.Background(), false)
			key, _ := middleware.GetAPIKeyFromContext(c)
			key.Group.RateMultiplier = 1
			price := 0.1
			key.Group.VideoPrice480P = &price
			key.User.Enterprise = &service.EnterpriseMemberContext{EnterpriseID: 7, MemberID: 9, EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive}
			result := &service.OpenAIForwardResult{Model: "grok-imagine-video", ResponseID: "task", VideoCount: 1, VideoDurationSeconds: 6, VideoResolution: "480p"}
			if provider == "seedance" {
				key.Group.VideoPrice480P, key.Group.VideoPrice720P, key.Group.VideoPrice768P, key.Group.VideoPrice1080P, key.Group.VideoPrice2K = nil, nil, nil, nil, nil
				result = &service.OpenAIForwardResult{Model: "gpt-5.1", ResponseID: "seedance:task", Usage: service.OpenAIUsage{OutputTokens: 1000}}
			}
			recordGrokMediaUsage(c, h, zap.NewNop(), personalAsyncVideoBillingKey(key), middleware.AuthSubject{UserID: key.UserID}, nil, &service.Account{ID: 3, Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey}, result, result.Model, nil, result.ResponseID)
			select {
			case cmd := <-billing.commands:
				require.Positive(t, cmd.BalanceCost)
				require.Zero(t, cmd.EnterpriseBalanceCost)
				require.Zero(t, cmd.EnterpriseID)
				require.Zero(t, cmd.SubscriptionCost)
			case <-time.After(3 * time.Second):
				t.Fatal("video completion did not settle")
			}
			select {
			case log := <-logs.created:
				require.Positive(t, log.ActualCost)
				require.Nil(t, log.EnterpriseID)
				require.Nil(t, log.EnterpriseDepartmentID)
			case <-time.After(3 * time.Second):
				t.Fatal("video completion log missing")
			}
			require.NotNil(t, key.User.Enterprise)
		})
	}
}

func TestHistoricalSubscriptionVideoCompletionAfterJoiningEnterprise(t *testing.T) {
	for _, tc := range []struct {
		name              string
		legacyPending     bool
		createSubscribed  bool
		currentSubscribed bool
	}{
		{name: "legacy subscription remains active", legacyPending: true, createSubscribed: true, currentSubscribed: true},
		{name: "frozen subscription after group mode changes", createSubscribed: true},
		{name: "frozen balance after subscription starts", currentSubscribed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false)
			cfg := &config.Config{}
			cfg.Default.RateMultiplier = 1
			logs := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 1)}
			billing := historicalVideoBillingRepo{commands: make(chan *service.UsageBillingCommand, 1)}
			account := service.Account{ID: 1, Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 50, GroupIDs: []int64{24}, Credentials: map[string]any{"api_key": "test-key"}}
			repo := grokMediaSlotRepo{openAIImagesFailoverAccountRepo: openAIImagesFailoverAccountRepo{accounts: []service.Account{account}}}
			// Subscription post-billing also updates its cache before writing the
			// usage log, so retain the fixture's real billing-cache dependency.
			h.gatewayService = service.NewOpenAIGatewayService(repo, logs, billing, nil, nil, nil, bindings, cfg, nil, service.NewConcurrencyService(slots), service.NewBillingService(cfg, nil), nil, h.billingCacheService, upstream, service.NewDeferredService(nil, nil, time.Second), nil, nil, nil, nil, nil, nil, nil)
			subscription := &service.UserSubscription{ID: 77, UserID: 10, GroupID: 24, Status: service.SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)}
			upstream.call = func(req *http.Request, _ int64) (*http.Response, error) {
				body := `{"request_id":"task","status":"pending"}`
				if req.Method == http.MethodGet {
					body = `{"request_id":"task","status":"done","model":"grok-imagine-video","video":{"url":"https://example.com/video.mp4","duration":6}}`
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			}

			// Build a legacy task accepted before durable reservations existed.
			create, _ := grokMediaSlotContext(context.Background(), true)
			createKey, _ := middleware.GetAPIKeyFromContext(create)
			createKey.Group.RateMultiplier = 1
			price := 0.1
			createKey.Group.VideoPrice480P = &price
			var acceptedSubscription *service.UserSubscription
			if tc.createSubscribed {
				createKey.Group.SubscriptionType = service.SubscriptionTypeSubscription
				acceptedSubscription = subscription
			}
			identity := newGrokVideoBillingIdentity(createKey, acceptedSubscription)
			if tc.legacyPending {
				identity = nil
			}
			require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(context.Background(), "task", 10, 20, service.GrokVideoPendingBilling{Model: "grok-imagine-video", VideoDurationSeconds: 6, VideoResolution: "480p", BillingIdentity: identity}))

			// Join an enterprise before the first terminal poll; current billing mode
			// may also differ from the mode that accepted the consumed task.
			poll, response := grokMediaSlotContext(context.Background(), false)
			key, _ := middleware.GetAPIKeyFromContext(poll)
			key.Group.RateMultiplier = 1
			key.Group.VideoPrice480P = &price
			key.User.Enterprise = &service.EnterpriseMemberContext{EnterpriseID: 7, MemberID: 9, EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive}
			if tc.currentSubscribed {
				key.Group.SubscriptionType = service.SubscriptionTypeSubscription
				poll.Set(string(middleware.ContextKeySubscription), subscription)
			}
			h.GrokVideoStatus(poll)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			select {
			case cmd := <-billing.commands:
				if tc.createSubscribed {
					require.Positive(t, cmd.SubscriptionCost)
					require.Equal(t, subscription.ID, *cmd.SubscriptionID)
					require.Zero(t, cmd.BalanceCost)
				} else {
					require.Positive(t, cmd.BalanceCost)
					require.Zero(t, cmd.SubscriptionCost)
				}
				require.Zero(t, cmd.EnterpriseBalanceCost)
				require.Zero(t, cmd.EnterpriseID)
			case <-time.After(3 * time.Second):
				t.Fatal("terminal historical video was not settled")
			}
			select {
			case log := <-logs.created:
				require.Positive(t, log.ActualCost)
				require.Nil(t, log.EnterpriseID)
				if tc.createSubscribed {
					require.Equal(t, service.BillingTypeSubscription, log.BillingType)
					require.Equal(t, subscription.ID, *log.SubscriptionID)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("historical video usage log missing")
			}
			slots.assertReleased(t)
			require.NotNil(t, key.User.Enterprise)
		})
	}
}

func TestHistoricalVideoRejectsForeignSubscriptionIdentity(t *testing.T) {
	c, _ := grokMediaSlotContext(context.Background(), false)
	key, _ := middleware.GetAPIKeyFromContext(c)
	key.Group.SubscriptionType = service.SubscriptionTypeSubscription
	for _, subscription := range []*service.UserSubscription{
		{ID: 77, UserID: key.UserID + 1, GroupID: key.Group.ID},
		{ID: 77, UserID: key.UserID, GroupID: key.Group.ID + 1},
	} {
		identity := newGrokVideoBillingIdentity(key, subscription)
		_, _, err := restoreGrokVideoBillingIdentity(key, identity)
		require.ErrorContains(t, err, "subscription identity does not match")
	}
}

func TestHistoricalSeedanceSubscriptionIdentitySurvivesEnterpriseJoin(t *testing.T) {
	h, _, bindings, _ := newGrokMediaSlotHandler(t, false, false, service.PlatformOpenAI)
	create, _ := grokMediaSlotContext(context.Background(), true)
	key, _ := middleware.GetAPIKeyFromContext(create)
	key.Group.Platform = service.PlatformOpenAI
	key.Group.SubscriptionType = service.SubscriptionTypeSubscription
	key.Group.RateMultiplier = 1
	subscription := &service.UserSubscription{ID: 77, UserID: key.UserID, GroupID: key.Group.ID}
	create.Set(string(middleware.ContextKeySubscription), subscription)
	key.Group.VideoPrice480P, key.Group.VideoPrice720P, key.Group.VideoPrice768P, key.Group.VideoPrice1080P, key.Group.VideoPrice2K = nil, nil, nil, nil, nil
	// This old token-fallback task predates protocol/model admission checks.
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(context.Background(), service.SeedanceTaskKey("task-ark"), key.UserID, key.ID, service.GrokVideoPendingBilling{Model: "gpt-5.1", BillingIdentity: newGrokVideoBillingIdentity(key, subscription)}))

	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	logs := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 1)}
	billing := historicalVideoBillingRepo{commands: make(chan *service.UsageBillingCommand, 1)}
	h.gatewayService = service.NewOpenAIGatewayService(nil, logs, billing, nil, nil, nil, bindings, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, h.billingCacheService, nil, service.NewDeferredService(nil, nil, time.Second), nil, nil, nil, nil, nil, nil, nil)
	key.User.Enterprise = &service.EnterpriseMemberContext{EnterpriseID: 7, MemberID: 9, EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive}
	key.Group.SubscriptionType = ""
	create.Params = gin.Params{{Key: "task_id", Value: "task-ark"}}
	taskID := service.SeedanceTaskKey("task-ark")
	subject := middleware.AuthSubject{UserID: key.UserID}
	result := prepareSeedanceCompletionBilling(context.Background(), h, key, subject, taskID, &service.OpenAIForwardResult{ResponseID: taskID, Usage: service.OpenAIUsage{OutputTokens: 1000}})
	require.NotNil(t, result)
	recordGrokMediaUsage(create, h, zap.NewNop(), personalAsyncVideoBillingKey(key), subject, nil, &service.Account{ID: 3, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}, result, result.Model, nil, taskID)
	select {
	case cmd := <-billing.commands:
		require.Positive(t, cmd.SubscriptionCost)
		require.Equal(t, subscription.ID, *cmd.SubscriptionID)
		require.Zero(t, cmd.BalanceCost)
		require.Zero(t, cmd.EnterpriseBalanceCost)
	case <-time.After(3 * time.Second):
		t.Fatal("historical Seedance subscription did not settle")
	}
	select {
	case log := <-logs.created:
		require.Equal(t, service.BillingTypeSubscription, log.BillingType)
		require.Nil(t, log.EnterpriseID)
	case <-time.After(3 * time.Second):
		t.Fatal("historical Seedance subscription log missing")
	}
}
