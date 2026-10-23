//go:build unit

package service

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func (f *videoTaskBillingFake) ListVideoBillingReservations(_ context.Context, state string, page, pageSize int) ([]*VideoBillingReservation, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []*VideoBillingReservation
	for _, v := range f.tasks {
		if v.State == state {
			items = append(items, cloneVideoTask(v))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	total := len(items)
	start := (page - 1) * pageSize
	if start >= total {
		return nil, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return items[start:end], total, nil
}

func manualVideoFixture(t *testing.T, accepted bool) (*OpenAIGatewayService, *videoTaskBillingFake, *VideoBillingReservation, ReserveVideoTaskInput) {
	t.Helper()
	svc, repo, input := videoTaskServiceFixture(t)
	v, err := svc.ReserveVideoTask(context.Background(), input)
	require.NoError(t, err)
	if accepted {
		require.NoError(t, svc.AcceptVideoTask(context.Background(), v, "upstream-real-task"))
	}
	require.NoError(t, repo.MarkManual(context.Background(), v.ID, "terminal state unavailable"))
	return svc, repo, v, input
}

func TestVideoBillingAdminListIsSafeAndValidated(t *testing.T) {
	svc, repo, v, _ := manualVideoFixture(t, true)
	repo.tasks[v.ID].Snapshot = json.RawMessage(`{"password":"snapshot-secret","credentials":{"api_key":"routing-secret"}}`)
	var contract map[string]any
	require.NoError(t, json.Unmarshal(v.Contract, &contract))
	contract["token_pricing"] = map[string]any{"unused_secret": "contract-secret"}
	repo.tasks[v.ID].Contract, _ = json.Marshal(contract)
	page, err := svc.ListVideoBillingReservations(context.Background(), "", 1, 20)
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, "720p", page.Items[0].Resolution)
	require.Equal(t, "video", page.Items[0].BillingMode)
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	for _, secret := range []string{"snapshot-secret", "routing-secret", "contract-secret", "token_pricing", "credentials", "contract", "snapshot"} {
		require.NotContains(t, string(encoded), secret)
	}
	for _, tc := range []struct {
		state      string
		page, size int
	}{{"unknown", 1, 20}, {"manual", 0, 20}, {"manual", 1, 101}, {"manual", 1000001, 20}} {
		_, err := svc.ListVideoBillingReservations(context.Background(), tc.state, tc.page, tc.size)
		require.ErrorIs(t, err, ErrVideoAdminInput)
	}
}

func TestVideoBillingAdminResolutionRequiresManualAndReason(t *testing.T) {
	svc, repo, v, _ := manualVideoFixture(t, true)
	for _, reason := range []string{"", "   ", strings.Repeat("字", 1001)} {
		_, err := svc.AdminReleaseVideoReservation(context.Background(), v.ID, 7, reason)
		require.ErrorIs(t, err, ErrVideoAdminInput)
	}
	_, err := svc.AdminReleaseVideoReservation(context.Background(), v.ID, 0, "verified")
	require.ErrorIs(t, err, ErrVideoAdminInput)
	for _, state := range []string{VideoReservationPreparing, VideoReservationAccepted} {
		repo.tasks[v.ID].State = state
		_, err := svc.AdminReleaseVideoReservation(context.Background(), v.ID, 7, "verified")
		require.ErrorIs(t, err, ErrVideoManualOnly)
		_, err = svc.AdminSettleVideoReservation(context.Background(), v.ID, 7, VideoBillingManualUsage{Reason: "verified"})
		require.ErrorIs(t, err, ErrVideoManualOnly)
	}
	require.Empty(t, repo.releases)
	require.Empty(t, repo.settlements)
	repo.tasks[v.ID].State = VideoReservationManual
	item, err := svc.AdminReleaseVideoReservation(context.Background(), v.ID, 7, " confirmed upstream failure ")
	require.NoError(t, err)
	require.Equal(t, VideoReservationReleased, item.State)
	_, err = svc.AdminReleaseVideoReservation(context.Background(), v.ID, 8, "repeat")
	require.NoError(t, err)
	require.Equal(t, []string{"manual release by administrator 7: confirmed upstream failure"}, repo.releases)
}

func TestVideoBillingAdminSettlementFrozenCapAndIdentity(t *testing.T) {
	for _, accepted := range []bool{true, false} {
		t.Run(map[bool]string{true: "real-upstream-id", false: "no-upstream-id"}[accepted], func(t *testing.T) {
			svc, repo, v, input := manualVideoFixture(t, accepted)
			input.APIKey.Group.VideoPrice720P = f64p(10)
			input.APIKey.Group.RateMultiplier = 20
			input.APIKey.Group.VideoBillingMode = "per_request"
			duration, count := 60.0, 2
			item, err := svc.AdminSettleVideoReservation(context.Background(), v.ID, 7, VideoBillingManualUsage{Reason: "verified upstream usage", DurationSeconds: &duration, VideoCount: &count})
			require.NoError(t, err)
			require.InDelta(t, .6, item.ActualAmount, 1e-12)
			require.Len(t, repo.settlements, 1)
			settlement := repo.settlements[0]
			require.Equal(t, int64(7), settlement.ManualActorID)
			require.Equal(t, "verified upstream usage", settlement.ManualReason)
			require.Equal(t, int64(71), settlement.Usage.UserID, "administrator is never substituted as payer")
			require.Equal(t, int64(72), settlement.Usage.APIKeyID)
			require.Equal(t, int64(73), *settlement.Usage.GroupID)
			require.InDelta(t, 12, settlement.Usage.TotalCost, 1e-12)
			require.Equal(t, 1.0, settlement.Usage.RateMultiplier)
			wantBillingID := "upstream-real-task"
			if !accepted {
				wantBillingID = "reservation:" + v.ID
				require.Empty(t, repo.tasks[v.ID].TaskID, "manual settlement must not invent an upstream binding")
			}
			require.Equal(t, StableGrokVideoBillingRequestID(wantBillingID), settlement.Usage.RequestID)
			_, err = svc.AdminSettleVideoReservation(context.Background(), v.ID, 8, VideoBillingManualUsage{Reason: "duplicate retry"})
			require.NoError(t, err)
			require.Len(t, repo.settlements, 1)
		})
	}
}

func TestVideoBillingAdminActualUsageValidationAndModes(t *testing.T) {
	svc, repo, v, _ := manualVideoFixture(t, true)
	count, zero, negative := 1, 0, -1
	duration, negativeDuration, nan := 4.0, -1.0, math.NaN()
	for _, input := range []VideoBillingManualUsage{
		{Reason: "verified"}, {Reason: "verified", DurationSeconds: &duration},
		{Reason: "verified", DurationSeconds: &negativeDuration, VideoCount: &count},
		{Reason: "verified", DurationSeconds: &nan, VideoCount: &count},
		{Reason: "verified", DurationSeconds: &duration, VideoCount: &zero},
		{Reason: "verified", DurationSeconds: &duration, VideoCount: &count, CompletionTokens: &negative},
	} {
		_, err := svc.AdminSettleVideoReservation(context.Background(), v.ID, 7, input)
		require.ErrorIs(t, err, ErrVideoAdminInput)
	}
	var contract VideoPricingContract
	require.NoError(t, json.Unmarshal(v.Contract, &contract))
	contract.Mode, contract.TokenPricing = BillingModeToken, &ModelPricing{OutputPricePerToken: .0001}
	repo.tasks[v.ID].Contract, _ = json.Marshal(contract)
	_, err := svc.AdminSettleVideoReservation(context.Background(), v.ID, 7, VideoBillingManualUsage{Reason: "verified"})
	require.ErrorIs(t, err, ErrVideoAdminInput)
	tokens := 2400
	_, err = svc.AdminSettleVideoReservation(context.Background(), v.ID, 7, VideoBillingManualUsage{Reason: "verified", CompletionTokens: &tokens})
	require.NoError(t, err)
	require.InDelta(t, .24, repo.settlements[0].Usage.ActualCost, 1e-12)
	svc, repo, v, _ = manualVideoFixture(t, false)
	require.NoError(t, json.Unmarshal(v.Contract, &contract))
	contract.Mode, contract.UnitPrice = BillingModePerRequest, .2
	repo.tasks[v.ID].Contract, _ = json.Marshal(contract)
	_, err = svc.AdminSettleVideoReservation(context.Background(), v.ID, 7, VideoBillingManualUsage{Reason: "verified"})
	require.NoError(t, err)
	require.InDelta(t, .2, repo.settlements[0].Usage.ActualCost, 1e-12)
}
