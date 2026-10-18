package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func reserveUsageBillingBatchImageEnterpriseBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
        UPDATE enterprises SET balance = balance - $1,
            frozen_balance = frozen_balance + $1, updated_at = NOW()
        WHERE id = $2 AND deleted_at IS NULL AND status = 'active' AND balance >= $1
        RETURNING balance, frozen_balance
    `, cmd.HoldAmount, cmd.EnterpriseID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM enterprises WHERE id = $1 AND deleted_at IS NULL`, cmd.EnterpriseID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrEnterpriseNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != service.EnterpriseStatusActive {
		return nil, service.ErrEnterpriseGatewayDisabled
	}
	return nil, service.ErrEnterpriseBillingBalanceInsufficient
}

func captureUsageBillingBatchImageEnterpriseBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	// Never consult live membership, status, department or soft-delete scopes.
	// Lock order matches Apply: dedup -> enterprises -> sorted quotas -> usage.
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
        UPDATE enterprises SET balance = balance + $1 - $2,
            frozen_balance = frozen_balance - $1, updated_at = NOW()
        WHERE id = $3 AND frozen_balance >= $1
        RETURNING balance, frozen_balance
    `, cmd.HoldAmount, cmd.ActualAmount, cmd.EnterpriseID).Scan(&balance, &frozen)
	if err != nil {
		return nil, err
	}
	_, err = incrementUsageBillingEnterpriseQuotas(ctx, tx, &service.UsageBillingCommand{
		RequestID: cmd.RequestID, APIKeyID: cmd.APIKeyID, EnterpriseID: cmd.EnterpriseID,
		EnterpriseDepartmentID: cmd.EnterpriseDepartmentID, EnterpriseMemberID: cmd.EnterpriseMemberID, EnterpriseDepartmentPathIDs: cmd.EnterpriseDepartmentPathIDs,
		EnterpriseBalanceCost: cmd.ActualAmount,
	})
	if err != nil {
		return nil, err
	}
	if cmd.UsageLog != nil {
		log := cmd.UsageLog
		log.UserID, log.APIKeyID = cmd.UserID, cmd.APIKeyID
		log.ActualCost, log.TotalCost, log.ImageOutputCost = cmd.ActualAmount, cmd.ActualAmount, cmd.ActualAmount
		service.ApplyUsageLogPayerIdentity(log, cmd.EnterpriseID, cmd.EnterpriseDepartmentID)
		if _, err := (&usageLogRepository{}).createSingle(ctx, tx, log); err != nil {
			return nil, err
		}
	}
	return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
}

func releaseUsageBillingBatchImageEnterpriseBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	// The transaction wrapper verifies the hold claim and serializes capture /
	// release through their shared terminal claim before acquiring this lock.
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
        UPDATE enterprises SET balance = balance + $1,
            frozen_balance = frozen_balance - $1, updated_at = NOW()
        WHERE id = $2 AND frozen_balance >= $1
        RETURNING balance, frozen_balance
    `, cmd.HoldAmount, cmd.EnterpriseID).Scan(&balance, &frozen)
	if err != nil {
		return nil, err
	}
	return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
}

func (r *usageBillingRepository) invalidateBatchImageEnterpriseCache(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand, capture bool) {
	if r.enterpriseCache == nil {
		return
	}
	// Also invalidate on replay: a worker may have died just after DB commit.
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := r.enterpriseCache.InvalidateEnterpriseBalance(cacheCtx, cmd.EnterpriseID); err != nil {
		logger.LegacyPrintf("repository.usage_billing", "batch enterprise balance invalidation failed: enterprise=%d error=%v", cmd.EnterpriseID, err)
	}
	if !capture {
		return
	}
	scopes := []service.EnterpriseQuotaScope{
		{ScopeType: service.EnterpriseScopeEnterprise, ScopeID: cmd.EnterpriseID},
		{ScopeType: service.EnterpriseScopeMember, ScopeID: cmd.EnterpriseMemberID},
	}
	for _, id := range service.FrozenEnterpriseDepartmentIDs(cmd.EnterpriseDepartmentPathIDs, cmd.EnterpriseDepartmentID) {
		scopes = append(scopes, service.EnterpriseQuotaScope{ScopeType: service.EnterpriseScopeDepartment, ScopeID: id})
	}
	for _, scope := range scopes {
		if err := r.enterpriseCache.InvalidateEnterpriseQuota(cacheCtx, scope.ScopeType, scope.ScopeID); err != nil {
			logger.LegacyPrintf("repository.usage_billing", "batch enterprise quota invalidation failed: scope=%s/%d error=%v", scope.ScopeType, scope.ScopeID, err)
		}
	}
}
