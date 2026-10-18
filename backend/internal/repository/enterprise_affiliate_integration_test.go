//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseAffiliateTransferPreservesQuotaAndLedger(t *testing.T) {
	for _, status := range []string{service.StatusActive, service.StatusDisabled} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			tx := testEntTx(t)
			ctx = dbent.NewTxContext(ctx, tx)
			client := tx.Client()
			e := enterpriseFixture(t, ctx)
			m := enterpriseMemberFixture(t, ctx, e.ID, nil)
			m.Status = status
			require.NoError(t, NewEnterpriseMemberRepository(client).Update(ctx, m))
			_, err := client.User.UpdateOneID(m.UserID).SetBalance(5).Save(ctx)
			require.NoError(t, err)
			_, err = client.ExecContext(ctx, `INSERT INTO user_affiliates (user_id, aff_code, aff_quota, aff_frozen_quota, aff_history_quota, created_at, updated_at)
VALUES ($1, $2, 12, 3, 15, NOW(), NOW())`, m.UserID, fmt.Sprintf("EA%d", time.Now().UnixNano()))
			require.NoError(t, err)
			repo := NewAffiliateRepository(client, integrationDB)
			transferred, balance, err := repo.TransferQuotaToBalance(ctx, m.UserID)
			require.ErrorIs(t, err, service.ErrEnterpriseMemberWalletDisabled)
			require.Zero(t, transferred)
			require.Zero(t, balance)
			require.Equal(t, 12.0, querySingleFloat(t, ctx, client, `SELECT aff_quota::double precision FROM user_affiliates WHERE user_id = $1`, m.UserID))
			require.Equal(t, 3.0, querySingleFloat(t, ctx, client, `SELECT aff_frozen_quota::double precision FROM user_affiliates WHERE user_id = $1`, m.UserID))
			require.Equal(t, 5.0, querySingleFloat(t, ctx, client, `SELECT balance::double precision FROM users WHERE id = $1`, m.UserID))
			require.Zero(t, querySingleInt(t, ctx, client, `SELECT COUNT(*) FROM user_affiliate_ledger WHERE user_id = $1`, m.UserID))
		})
	}
}
