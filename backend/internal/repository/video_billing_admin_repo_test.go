//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func videoAdminContract(t *testing.T) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(service.VideoPricingContract{Version: service.VideoPricingContractVersion, Model: "frozen-video", Resolution: "720p", Mode: service.BillingModeVideo,
		UnitPrice: .1, EffectiveMultiplier: 1, ChannelTimeMultiplier: 1, ReasoningMultiplier: 1, MaxDurationSeconds: 6, MaxVideoCount: 1, ReservedAmount: .6})
	require.NoError(t, err)
	return encoded
}

func TestVideoBillingAdminPostgresManualSettlementAtomicAndStableIdentity(t *testing.T) {
	db := videoTestDB(t)
	ctx := context.Background()
	v := videoFixture(t, db, 1, .6)
	v.Contract = videoAdminContract(t)
	repo := NewVideoBillingReservationRepository(db)
	svc := &service.OpenAIGatewayService{}
	svc.SetVideoBillingReservationRepository(repo)
	require.NoError(t, repo.Reserve(ctx, v))
	duration, count := 4.0, 1
	input := service.VideoBillingManualUsage{Reason: "verified provider output", DurationSeconds: &duration, VideoCount: &count}
	_, err := svc.AdminSettleVideoReservation(ctx, v.ID, 7, input)
	require.ErrorIs(t, err, service.ErrVideoManualOnly)
	_, err = repo.Settle(ctx, &service.VideoReservationSettlement{ID: v.ID, Usage: videoUsage(v, .4), ManualActorID: 7, ManualReason: input.Reason})
	require.ErrorIs(t, err, service.ErrVideoManualOnly, "manual-only constraint also applies under row lock")
	require.NoError(t, repo.MarkManual(ctx, v.ID, "provider unknown"))
	bad := videoUsage(v, .4)
	bad.RequestID = service.StableGrokVideoBillingRequestID("reservation:" + v.ID)
	bad.Model = strings.Repeat("x", 101)
	_, err = repo.Settle(ctx, &service.VideoReservationSettlement{ID: v.ID, Usage: bad, ManualActorID: 7, ManualReason: "must rollback with money"})
	require.Error(t, err)
	videoWallet(t, db, v, .4, .6)
	stored, err := repo.Get(ctx, v.ID)
	require.NoError(t, err)
	require.Equal(t, service.VideoReservationManual, stored.State)
	require.Equal(t, "provider unknown", stored.LastError)
	var claims int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, bad.RequestID, v.APIKeyID).Scan(&claims))
	require.Zero(t, claims)
	// Simultaneous administrator retries all share the same reservation money event.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.AdminSettleVideoReservation(ctx, v.ID, 7, input)
			if err != nil {
				t.Errorf("manual settlement: %v", err)
			}
		}()
	}
	wg.Wait()
	videoWallet(t, db, v, .6, 0)
	stored, err = repo.Get(ctx, v.ID)
	require.NoError(t, err)
	require.Equal(t, service.VideoReservationSettled, stored.State)
	require.Empty(t, stored.TaskID, "do not invent an upstream ID to settle a lost create response")
	require.Equal(t, "manual settlement by administrator 7: verified provider output", stored.LastError)
	require.InDelta(t, .4, stored.ActualAmount, 1e-12)
	var logged int
	var payer int64
	var amount float64
	require.NoError(t, db.QueryRow(`SELECT COUNT(*),MAX(user_id),SUM(actual_cost) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, bad.RequestID, v.APIKeyID).Scan(&logged, &payer, &amount))
	require.Equal(t, 1, logged)
	require.Equal(t, v.UserID, payer)
	require.InDelta(t, .4, amount, 1e-12)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, bad.RequestID, v.APIKeyID).Scan(&claims))
	require.Equal(t, 1, claims)
	t.Log("manual usage-insert failure rolls back money/dedup/reason; 8 concurrent retries capture $0.40 once; empty upstream ID stays empty")
}

func TestVideoBillingAdminPostgresListReleaseAndRealTaskKey(t *testing.T) {
	db := videoTestDB(t)
	ctx := context.Background()
	repo := NewVideoBillingReservationRepository(db)
	svc := &service.OpenAIGatewayService{}
	svc.SetVideoBillingReservationRepository(repo)
	v := videoFixture(t, db, 1, .6)
	v.Contract = videoAdminContract(t)
	require.NoError(t, repo.Reserve(ctx, v))
	require.NoError(t, repo.Accept(ctx, v.ID, "real-task-"+v.ID, v.AccountID))
	require.NoError(t, repo.MarkManual(ctx, v.ID, "retention exceeded"))
	page, err := svc.ListVideoBillingReservations(ctx, "manual", 1, 1)
	require.NoError(t, err)
	require.GreaterOrEqual(t, page.Total, 1)
	require.Len(t, page.Items, 1)
	require.Equal(t, v.ID, page.Items[0].ID)
	for i := 0; i < 2; i++ {
		item, err := svc.AdminReleaseVideoReservation(ctx, v.ID, 7, "confirmed task failed")
		require.NoError(t, err)
		require.Equal(t, service.VideoReservationReleased, item.State)
		require.Equal(t, "manual release by administrator 7: confirmed task failed", item.ReviewNote)
	}
	videoWallet(t, db, v, 1, 0)
	_, err = svc.AdminSettleVideoReservation(ctx, v.ID, 7, service.VideoBillingManualUsage{Reason: "cannot charge a released task"})
	require.ErrorIs(t, err, service.ErrVideoManualOnly)
	v = videoFixture(t, db, 1, .6)
	v.Contract = videoAdminContract(t)
	require.NoError(t, repo.Reserve(ctx, v))
	upstreamID := "real-task-" + v.ID
	require.NoError(t, repo.Accept(ctx, v.ID, upstreamID, v.AccountID))
	require.NoError(t, repo.MarkManual(ctx, v.ID, "retention exceeded"))
	duration, count := 60.0, 2
	item, err := svc.AdminSettleVideoReservation(ctx, v.ID, 7, service.VideoBillingManualUsage{Reason: "verified actual output", DurationSeconds: &duration, VideoCount: &count})
	require.NoError(t, err)
	require.InDelta(t, .6, item.ActualAmount, 1e-12)
	videoWallet(t, db, v, .4, 0)
	var logged, originalTaskID string
	require.NoError(t, db.QueryRow(`SELECT request_id FROM usage_logs WHERE api_key_id=$1`, v.APIKeyID).Scan(&logged))
	require.Equal(t, service.StableGrokVideoBillingRequestID(upstreamID), logged)
	require.NoError(t, db.QueryRow(`SELECT task_id FROM video_billing_reservations WHERE id=$1`, v.ID).Scan(&originalTaskID))
	require.Equal(t, upstreamID, originalTaskID)
	_, err = svc.AdminReleaseVideoReservation(ctx, v.ID, 7, "already settled")
	require.True(t, errors.Is(err, service.ErrVideoManualOnly))
	t.Log("manual-only list paginates; release retries refund once and preserve administrator reason; real task keeps its existing dedup key; $12 actual capped to $0.60")
}
