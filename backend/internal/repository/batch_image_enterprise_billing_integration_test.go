//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func enterpriseBatchJobFixture(t *testing.T, f enterpriseBillingFixture, hold float64, noDepartment ...bool) *service.BatchImageJob {
	t.Helper()
	ctx := context.Background()
	account := mustCreateAccount(t, testEntClient(t), &service.Account{Name: "batch-enterprise-" + uuid.NewString(), Type: service.AccountTypeAPIKey})
	batchID, idErr := service.NewBatchImageID()
	require.NoError(t, idErr)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, err := integrationDB.ExecContext(cleanupCtx, `DELETE FROM usage_logs WHERE account_id=$1`, account.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(cleanupCtx, `DELETE FROM batch_image_jobs WHERE batch_id=$1`, batchID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(cleanupCtx, `DELETE FROM accounts WHERE id=$1`, account.ID)
		require.NoError(t, err)
	})
	departmentID := &f.department.ID
	if len(noDepartment) > 0 && noDepartment[0] {
		departmentID = nil
	}
	membership, err := NewEnterpriseRepository(testEntClient(t)).GetMemberContextByUserID(ctx, f.member.UserID)
	require.NoError(t, err)
	path := membership.DepartmentPathIDs
	if departmentID == nil {
		path = nil
	}
	job, err := NewBatchImageRepository(integrationDB).CreateBatchImageJob(ctx, service.CreateBatchImageJobParams{
		BatchID: batchID, UserID: f.member.UserID, APIKeyID: &f.key.ID, AccountID: &account.ID,
		EnterpriseID: &f.enterprise.ID, EnterpriseDepartmentID: departmentID, EnterpriseMemberID: &f.member.ID, EnterpriseDepartmentPathIDs: path,
		Provider: service.BatchImageProviderGeminiAPI, Model: "gemini-2.5-flash-image", Status: service.BatchImageJobStatusSettling,
		ItemCount: 2, SuccessCount: 1, FailCount: 1, HoldAmount: &hold, EstimatedCost: hold,
		BillableUnitPrice: 2.5, PricingSnapshotVersion: 1, GroupRateMultiplier: 1, AccountRateMultiplier: 1, BatchDiscountMultiplier: 1,
	})
	require.NoError(t, err)
	return job
}

func enterpriseBatchHoldCommand(job *service.BatchImageJob, operation string, actual float64) *service.BatchImageBalanceHoldCommand {
	requestID := service.BatchImageHoldRequestID(job.BatchID)
	if operation == "capture" {
		requestID = service.BatchImageCaptureRequestID(job.BatchID)
	}
	if operation == "release" {
		requestID = service.BatchImageReleaseRequestID(job.BatchID)
	}
	var departmentID int64
	if job.EnterpriseDepartmentID != nil {
		departmentID = *job.EnterpriseDepartmentID
	}
	return &service.BatchImageBalanceHoldCommand{RequestID: requestID, UserID: job.UserID, APIKeyID: *job.APIKeyID,
		BatchID: job.BatchID, HoldAmount: *job.HoldAmount, ActualAmount: actual,
		EnterpriseID: *job.EnterpriseID, EnterpriseDepartmentID: departmentID, EnterpriseMemberID: *job.EnterpriseMemberID, EnterpriseDepartmentPathIDs: job.EnterpriseDepartmentPathIDs}
}

func assertEnterpriseBatchWallet(t *testing.T, f enterpriseBillingFixture, balance, frozen float64) {
	t.Helper()
	var available, held, personal, personalFrozen float64
	require.NoError(t, integrationDB.QueryRow(`SELECT e.balance,e.frozen_balance,u.balance,u.frozen_balance FROM enterprises e CROSS JOIN users u WHERE e.id=$1 AND u.id=$2`, f.enterprise.ID, f.member.UserID).Scan(&available, &held, &personal, &personalFrozen))
	require.Equal(t, balance, available)
	require.Equal(t, frozen, held)
	require.Equal(t, 100.0, personal)
	require.Zero(t, personalFrozen)
}

func TestBatchImageEnterpriseReserveCaptureSnapshotUsageAndCache(t *testing.T) {
	for _, change := range []string{"unchanged", "removed", "department changed", "enterprise disabled"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			f := newEnterpriseBillingFixture(t)
			job := enterpriseBatchJobFixture(t, f, 4)
			_, cache := newEnterpriseCacheTest(t)
			require.NoError(t, cache.SetEnterpriseBalance(ctx, f.enterprise.ID, 10))
			generation, err := cache.GetEnterpriseBalanceGeneration(ctx, f.enterprise.ID)
			require.NoError(t, err)
			billing := ProvideUsageBillingRepository(testEntClient(t), integrationDB, cache)
			hold, err := billing.ReserveBatchImageBalance(ctx, enterpriseBatchHoldCommand(job, "reserve", 0))
			require.NoError(t, err)
			require.True(t, hold.Applied)
			assertEnterpriseBatchWallet(t, f, 6, 4)
			// A loader started before the hold cannot repopulate the old balance.
			stored, err := cache.SetEnterpriseBalanceIfGeneration(ctx, f.enterprise.ID, 10, generation)
			require.NoError(t, err)
			require.False(t, stored)
			// Prior natural windows must reset, while cumulative usage survives.
			old := timezone.StartOfMonth(timezone.Now()).AddDate(0, -1, 0)
			_, err = integrationDB.ExecContext(ctx, `UPDATE enterprise_quotas SET daily_usage_usd=8,weekly_usage_usd=8,monthly_usage_usd=8,total_usage_usd=9,daily_window_start=$2,weekly_window_start=$2,monthly_window_start=$2 WHERE enterprise_id=$1`, f.enterprise.ID, old)
			require.NoError(t, err)
			scopes := []service.EnterpriseQuotaScope{{ScopeType: service.EnterpriseScopeEnterprise, ScopeID: f.enterprise.ID}, {ScopeType: service.EnterpriseScopeDepartment, ScopeID: f.department.ID}, {ScopeType: service.EnterpriseScopeMember, ScopeID: f.member.ID}}
			quotas, err := NewEnterpriseQuotaRepository(testEntClient(t)).GetByScopes(ctx, f.enterprise.ID, scopes)
			require.NoError(t, err)
			generations := make([]int64, len(quotas))
			for i := range quotas {
				require.NoError(t, cache.SetEnterpriseQuota(ctx, &quotas[i]))
				generations[i], err = cache.GetEnterpriseQuotaGeneration(ctx, quotas[i].ScopeType, quotas[i].ScopeID)
				require.NoError(t, err)
			}
			e, err := NewEnterpriseRepository(testEntClient(t)).GetByID(ctx, f.enterprise.ID)
			require.NoError(t, err)
			require.Equal(t, 4.0, e.FrozenBalance)
			switch change {
			case "removed":
				require.NoError(t, NewEnterpriseMemberRepository(testEntClient(t)).Delete(ctx, f.enterprise.ID, f.member.ID))
			case "department changed":
				_, err = integrationDB.ExecContext(ctx, `UPDATE enterprise_members SET department_id=NULL WHERE id=$1`, f.member.ID)
				require.NoError(t, err)
			case "enterprise disabled":
				_, err = integrationDB.ExecContext(ctx, `UPDATE enterprises SET status='disabled' WHERE id=$1`, f.enterprise.ID)
				require.NoError(t, err)
			}
			// Test both archived quota scopes without changing payment identity.
			if change == "removed" {
				_, err = integrationDB.ExecContext(ctx, `UPDATE enterprise_quotas SET deleted_at=NOW() WHERE scope_type='department' AND scope_id=$1`, f.department.ID)
				require.NoError(t, err)
			}
			jobs := NewBatchImageRepository(integrationDB)
			reloaded, err := jobs.GetBatchImageJobByBatchID(ctx, job.BatchID)
			require.NoError(t, err)
			require.Equal(t, job.EnterpriseID, reloaded.EnterpriseID)
			require.Equal(t, job.EnterpriseDepartmentID, reloaded.EnterpriseDepartmentID)
			require.Equal(t, job.EnterpriseMemberID, reloaded.EnterpriseMemberID)
			settle := &service.BatchImageSettlementService{Repo: jobs, BillingRepo: billing, Pricing: &service.BatchImageModelPricingResolver{}}
			_, err = settle.Settle(ctx, job.BatchID)
			require.NoError(t, err)
			assertEnterpriseBatchWallet(t, f, 7.5, 0)
			for i := range quotas {
				stored, err := cache.SetEnterpriseQuotaIfGeneration(ctx, &quotas[i], generations[i])
				require.NoError(t, err)
				require.False(t, stored, "capture fences stale quota reloads")
			}
			cmd := enterpriseBatchHoldCommand(job, "capture", 2.5)
			cmd.RequestPayloadHash = service.BuildBatchImageSettlementManifestHash(job)
			replay, err := billing.CaptureBatchImageBalance(ctx, cmd)
			require.NoError(t, err)
			require.False(t, replay.Applied)
			// A late release must not return funds already captured.
			_, err = billing.ReleaseBatchImageBalance(ctx, enterpriseBatchHoldCommand(job, "release", 0))
			require.NoError(t, err)
			assertEnterpriseBatchWallet(t, f, 7.5, 0)
			var count int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM enterprise_quotas WHERE enterprise_id=$1 AND daily_usage_usd=2.5 AND weekly_usage_usd=2.5 AND monthly_usage_usd=2.5 AND total_usage_usd=11.5`, f.enterprise.ID).Scan(&count))
			require.Equal(t, 3, count)
			var enterpriseID, departmentID int64
			var cost float64
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT enterprise_id, enterprise_department_id, actual_cost FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, cmd.RequestID, f.key.ID).Scan(&enterpriseID, &departmentID, &cost))
			require.Equal(t, f.enterprise.ID, enterpriseID)
			require.Equal(t, f.department.ID, departmentID)
			require.Equal(t, 2.5, cost)
		})
	}
}

func TestBatchImageEnterpriseReleaseInsufficientAndPhantom(t *testing.T) {
	ctx := context.Background()
	f := newEnterpriseBillingFixture(t)
	job := enterpriseBatchJobFixture(t, f, 4)
	_, cache := newEnterpriseCacheTest(t)
	repo := ProvideUsageBillingRepository(testEntClient(t), integrationDB, cache)
	_, err := repo.ReserveBatchImageBalance(ctx, enterpriseBatchHoldCommand(job, "reserve", 0))
	require.NoError(t, err)
	_, err = repo.CaptureBatchImageBalance(ctx, enterpriseBatchHoldCommand(job, "capture", 5))
	require.ErrorIs(t, err, service.ErrBatchImageSettlementCostExceedsHold)
	assertEnterpriseBatchWallet(t, f, 6, 4)
	other := *job
	other.BatchID = "insufficient-" + uuid.NewString()
	tooMuch := 7.0
	other.HoldAmount = &tooMuch
	_, err = repo.ReserveBatchImageBalance(ctx, enterpriseBatchHoldCommand(&other, "reserve", 0))
	require.ErrorIs(t, err, service.ErrEnterpriseBillingBalanceInsufficient)
	// No hold claim exists for this job: another job's funds remain frozen.
	other.HoldAmount = job.HoldAmount
	_, err = repo.ReleaseBatchImageBalance(ctx, enterpriseBatchHoldCommand(&other, "release", 0))
	require.NoError(t, err)
	assertEnterpriseBatchWallet(t, f, 6, 4)
	_, err = integrationDB.ExecContext(ctx, `UPDATE enterprise_members SET deleted_at=NOW(),department_id=NULL WHERE id=$1`, f.member.ID)
	require.NoError(t, err)
	require.NoError(t, cache.SetEnterpriseBalance(ctx, f.enterprise.ID, 6))
	generation, err := cache.GetEnterpriseBalanceGeneration(ctx, f.enterprise.ID)
	require.NoError(t, err)
	_, err = repo.ReleaseBatchImageBalance(ctx, enterpriseBatchHoldCommand(job, "release", 0))
	require.NoError(t, err)
	replay, err := repo.ReleaseBatchImageBalance(ctx, enterpriseBatchHoldCommand(job, "release", 0))
	require.NoError(t, err)
	require.False(t, replay.Applied)
	assertEnterpriseBatchWallet(t, f, 10, 0)
	stored, err := cache.SetEnterpriseBalanceIfGeneration(ctx, f.enterprise.ID, 6, generation)
	require.NoError(t, err)
	require.False(t, stored, "release fences stale balance reloads")
	// Opposite terminal operation is a no-op even if another hold now exists.
	other.BatchID = "next-" + uuid.NewString()
	_, err = repo.ReserveBatchImageBalance(ctx, enterpriseBatchHoldCommand(&other, "reserve", 0))
	require.NoError(t, err)
	result, err := repo.CaptureBatchImageBalance(ctx, enterpriseBatchHoldCommand(job, "capture", 1))
	require.NoError(t, err)
	require.False(t, result.Applied)
	assertEnterpriseBatchWallet(t, f, 6, 4)
}

func TestBatchImageEnterpriseConcurrentHoldsAndApply(t *testing.T) {
	f := newEnterpriseBillingFixture(t)
	first := enterpriseBatchJobFixture(t, f, 2)
	second := enterpriseBatchJobFixture(t, f, 2)
	repo := NewUsageBillingRepository(testEntClient(t), integrationDB)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := make(chan struct{})
	errs := make(chan error, 3)
	var wg sync.WaitGroup
	for i := range 3 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			var err error
			if i == 2 {
				cmd := f.command(1)
				cmd.APIKeyQuotaCost = 1
				_, err = repo.Apply(ctx, cmd)
			} else {
				job := first
				if i == 1 {
					job = second
				}
				_, err = repo.ReserveBatchImageBalance(ctx, enterpriseBatchHoldCommand(job, "reserve", 0))
				if err == nil {
					_, err = repo.CaptureBatchImageBalance(ctx, enterpriseBatchHoldCommand(job, "capture", 1))
				}
			}
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	assertEnterpriseBatchWallet(t, f, 7, 0)
}

func TestBatchImageEnterpriseFrozenBalanceBlocksDissolveAndAdminSubtract(t *testing.T) {
	ctx := context.Background()
	f := newEnterpriseBillingFixture(t)
	job := enterpriseBatchJobFixture(t, f, 4)
	repo := NewUsageBillingRepository(testEntClient(t), integrationDB)
	_, err := repo.ReserveBatchImageBalance(ctx, enterpriseBatchHoldCommand(job, "reserve", 0))
	require.NoError(t, err)
	enterprises := NewEnterpriseRepository(testEntClient(t))
	require.ErrorIs(t, enterprises.(service.EnterpriseDissolveRepository).Dissolve(ctx, f.enterprise.ID), service.ErrEnterpriseFrozenBalancePending)
	_, err = enterprises.AdjustBalance(ctx, f.enterprise.ID, service.EnterpriseBalanceAdjustment{Operation: service.EnterpriseBalanceOperationSubtract, Amount: 7})
	require.ErrorIs(t, err, service.ErrEnterpriseBalanceAdjustmentInsufficient)
	assertEnterpriseBatchWallet(t, f, 6, 4)
	// Direct ordinary deletion also refuses pending funds, even after removal.
	require.NoError(t, NewEnterpriseMemberRepository(testEntClient(t)).Delete(ctx, f.enterprise.ID, f.member.ID))
	require.ErrorIs(t, enterprises.Delete(ctx, f.enterprise.ID), service.ErrEnterpriseFrozenBalancePending)
	_, err = repo.ReleaseBatchImageBalance(ctx, enterpriseBatchHoldCommand(job, "release", 0))
	require.NoError(t, err)
	require.NoError(t, enterprises.(service.EnterpriseDissolveRepository).Dissolve(ctx, f.enterprise.ID))
}

func TestBatchImageEnterpriseCaptureRollsBackWithUsageLogFailure(t *testing.T) {
	ctx := context.Background()
	f := newEnterpriseBillingFixture(t)
	job := enterpriseBatchJobFixture(t, f, 4)
	repo := NewUsageBillingRepository(testEntClient(t), integrationDB)
	_, err := repo.ReserveBatchImageBalance(ctx, enterpriseBatchHoldCommand(job, "reserve", 0))
	require.NoError(t, err)
	cmd := enterpriseBatchHoldCommand(job, "capture", 1)
	cmd.UsageLog = &service.UsageLog{UserID: job.UserID, APIKeyID: *job.APIKeyID, AccountID: *job.AccountID, RequestID: cmd.RequestID, Model: fmt.Sprintf("%0130d", 1), CreatedAt: time.Now()}
	_, err = repo.CaptureBatchImageBalance(ctx, cmd)
	require.Error(t, err)
	assertEnterpriseBatchWallet(t, f, 6, 4)
	var used float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT SUM(total_usage_usd) FROM enterprise_quotas WHERE enterprise_id=$1`, f.enterprise.ID).Scan(&used))
	require.Zero(t, used)
	// A rolled-back terminal claim must not block a later valid capture.
	cmd.UsageLog.Model = "valid-model"
	_, err = repo.CaptureBatchImageBalance(ctx, cmd)
	require.NoError(t, err)
	assertEnterpriseBatchWallet(t, f, 9, 0)
}

func TestBatchImageEnterpriseAmountsQuantizeWithoutFrozenResidue(t *testing.T) {
	ctx := context.Background()
	f := newEnterpriseBillingFixture(t)
	job := enterpriseBatchJobFixture(t, f, .123456785)
	repo := NewUsageBillingRepository(testEntClient(t), integrationDB)
	_, err := repo.ReserveBatchImageBalance(ctx, enterpriseBatchHoldCommand(job, "reserve", 0))
	require.NoError(t, err)
	_, err = repo.CaptureBatchImageBalance(ctx, enterpriseBatchHoldCommand(job, "capture", .023456785))
	require.NoError(t, err)
	assertEnterpriseBatchWallet(t, f, 9.97654321, 0)
	var matching int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM enterprises e JOIN enterprise_quotas q ON q.enterprise_id=e.id WHERE e.id=$1 AND q.total_usage_usd=10-e.balance`, f.enterprise.ID).Scan(&matching))
	require.Equal(t, 3, matching)
}

func TestBatchImageEnterpriseWithoutDepartmentUsesTwoQuotaScopes(t *testing.T) {
	ctx := context.Background()
	f := newEnterpriseBillingFixture(t)
	job := enterpriseBatchJobFixture(t, f, 4, true)
	require.Nil(t, job.EnterpriseDepartmentID)
	repo := NewUsageBillingRepository(testEntClient(t), integrationDB)
	_, err := repo.ReserveBatchImageBalance(ctx, enterpriseBatchHoldCommand(job, "reserve", 0))
	require.NoError(t, err)
	_, err = repo.CaptureBatchImageBalance(ctx, enterpriseBatchHoldCommand(job, "capture", 1))
	require.NoError(t, err)
	assertEnterpriseBatchWallet(t, f, 9, 0)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM enterprise_quotas WHERE enterprise_id=$1 AND total_usage_usd=1`, f.enterprise.ID).Scan(&count))
	require.Equal(t, 2, count)
}
