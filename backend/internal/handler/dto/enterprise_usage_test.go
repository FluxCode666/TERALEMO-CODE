//go:build unit

package dto

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseUsageFieldsMatchPersonalDTO(t *testing.T) {
	str := func(v string) *string { return &v }
	number := 17
	id := int64(9)
	log := &service.UsageLog{
		ID: 1, UserID: 2, APIKeyID: 3, AccountID: 999, RequestID: "req-client", Model: "billing-model", RequestedModel: "client-model",
		ServiceTier: str("priority"), ReasoningEffort: str("high"), RequestedReasoningEffort: str(" max "), InboundEndpoint: str("/v1/messages"),
		GroupID: &id, SubscriptionID: &id, EnterpriseDepartmentID: &id,
		InputTokens: 101, OutputTokens: 102, CacheCreationTokens: 103, CacheReadTokens: 104, CacheCreation5mTokens: 105, CacheCreation1hTokens: 106,
		InputCost: 1.1, OutputCost: 1.2, CacheCreationCost: 1.3, CacheReadCost: 1.4, TotalCost: 5, ActualCost: 6, RateMultiplier: 1.2, LongContextBillingApplied: true,
		BillingType: 1, RequestType: service.RequestTypeWSV2, NativeCompactionV2: true, DurationMs: &number, FirstTokenMs: &number,
		ImageCount: 2, ImageSize: str("1024x1024"), ImageInputSize: str("512x512"), ImageOutputSize: str("1024x1024"), ImageInputTokens: 201, ImageInputCost: 2.1,
		ImageOutputTokens: 202, ImageOutputCost: 2.2, ImageSizeSource: str("response"), ImageSizeBreakdown: map[string]int{"1024x1024": 2}, MediaType: str("image"),
		UserAgent: str("client"), IPAddress: str("127.0.0.1"), CacheTTLOverridden: true, BillingMode: str("image"), CreatedAt: time.Now(),
		UpstreamModel: str("private-model"), UpstreamEndpoint: str("/private"), SessionID: str("private-session"),
		User: &service.User{ID: 2, Balance: 999}, APIKey: &service.APIKey{ID: 3, Key: "sk-private"}, Account: &service.Account{ID: 999},
	}
	personalJSON, err := json.Marshal(UsageLogFromService(log))
	require.NoError(t, err)
	enterprise := service.EnterpriseUsageLogFromUsageLog(log)
	enterprise.APIKey = &service.EnterpriseUsageNamedResource{ID: 3, Name: "display"}
	enterprise.Group = &service.EnterpriseUsageGroup{ID: 9, Name: "group", Platform: "openai"}
	enterpriseJSON, err := json.Marshal(enterprise)
	require.NoError(t, err)
	var personal, actual map[string]any
	require.NoError(t, json.Unmarshal(personalJSON, &personal))
	require.NoError(t, json.Unmarshal(enterpriseJSON, &actual))
	excluded := map[string]bool{"account_id": true, "upstream_endpoint": true, "subscription_id": true, "session_id": true, "subscription": true}
	associations := map[string]bool{"user": true, "api_key": true, "group": true}
	typ := reflect.TypeFor[UsageLog]()
	for i := 0; i < typ.NumField(); i++ {
		key := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if excluded[key] || associations[key] {
			continue
		}
		require.Contains(t, actual, key)
		require.Equal(t, personal[key], actual[key], key)
	}
	for _, key := range []string{"account_id", "account", "account_rate_multiplier", "account_stats_cost", "account_cost", "upstream_model", "upstream_response_model", "upstream_endpoint", "upstream_request_id", "subscription_id", "subscription", "session_id", "balance", "model_mapping_chain", "channel_id"} {
		require.NotContains(t, actual, key)
	}
	require.Equal(t, map[string]any{"id": float64(2), "username": "", "email": ""}, actual["user"])
	require.Equal(t, map[string]any{"id": float64(3), "name": "display"}, actual["api_key"])
	require.Equal(t, map[string]any{"id": float64(9), "name": "group", "platform": "openai"}, actual["group"])
	require.Contains(t, actual, "department_id")
	require.Contains(t, actual, "department_name")
	require.NotContains(t, string(enterpriseJSON), "sk-private")
}

func TestEnterpriseUsageLegacyDisplayMatchesPersonalDTO(t *testing.T) {
	blank, effective := "  ", "high"
	for _, log := range []*service.UsageLog{
		{Model: "legacy", Stream: true},
		{Model: "legacy", OpenAIWSMode: true, ReasoningEffort: &effective, RequestedReasoningEffort: &blank},
		{Model: "legacy", RequestType: service.RequestTypeSync, Stream: true, OpenAIWSMode: true},
	} {
		personal := UsageLogFromService(log)
		actual := service.EnterpriseUsageLogFromUsageLog(log)
		require.Equal(t, personal.Model, actual.Model)
		require.Equal(t, personal.ReasoningEffort, actual.ReasoningEffort)
		require.Equal(t, personal.RequestType, actual.RequestType)
		require.Equal(t, personal.Stream, actual.Stream)
		require.Equal(t, personal.OpenAIWSMode, actual.OpenAIWSMode)
	}
}
