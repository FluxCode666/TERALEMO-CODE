//go:build unit

package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestEnterpriseAsyncVideoSubmissionRejectedBeforeAnyUpstreamWork(t *testing.T) {
	for _, endpoint := range []string{"generate", "edit", "extend", "seedance"} {
		t.Run(endpoint, func(t *testing.T) {
			c, w := grokMediaSlotContext(context.Background(), true)
			key, _ := middleware.GetAPIKeyFromContext(c)
			key.User.Enterprise = &service.EnterpriseMemberContext{EnterpriseID: 7, MemberID: 9}
			h := &OpenAIGatewayHandler{} // Any concurrency, billing or upstream call would panic.
			switch endpoint {
			case "generate":
				h.GrokVideoGeneration(c)
			case "edit":
				h.GrokVideoEdit(c)
			case "extend":
				h.GrokVideoExtension(c)
			case "seedance":
				key.Group.Platform = service.PlatformOpenAI
				h.SeedanceTasks(c)
			}
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), "ENTERPRISE_FEATURE_UNSUPPORTED")
		})
	}
}

func TestAsyncVideoHistoricalPersonalPayerSnapshot(t *testing.T) {
	key := &service.APIKey{ID: 2, UserID: 42, User: &service.User{ID: 42, Balance: 123, Enterprise: &service.EnterpriseMemberContext{EnterpriseID: 7, MemberID: 9}}}
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
	key.User.Enterprise = &service.EnterpriseMemberContext{EnterpriseID: 7, MemberID: 9}
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
			key.User.Enterprise = &service.EnterpriseMemberContext{EnterpriseID: 7, MemberID: 9}
			result := &service.OpenAIForwardResult{Model: "grok-imagine-video", ResponseID: "task", VideoCount: 1, VideoDurationSeconds: 6, VideoResolution: "480p"}
			if provider == "seedance" {
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
