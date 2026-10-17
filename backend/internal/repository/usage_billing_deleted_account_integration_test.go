//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func deletedAccountBillingCommand(t *testing.T, enterprise bool) *service.UsageBillingCommand {
	t.Helper()
	if enterprise {
		return newEnterpriseBillingFixture(t).command(1.25)
	}
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@billing.test", PasswordHash: "hash", Balance: 100})
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, user.ID)
		require.NoError(t, err)
	})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "deleted-account"})
	t.Cleanup(func() {
		ctx := context.Background()
		for _, query := range []string{
			`DELETE FROM usage_billing_dedup WHERE api_key_id=$1`,
			`DELETE FROM api_keys WHERE id=$1`,
		} {
			_, err := integrationDB.ExecContext(ctx, query, key.ID)
			require.NoError(t, err)
		}
	})
	return &service.UsageBillingCommand{RequestID: uuid.NewString(), UserID: user.ID, APIKeyID: key.ID, BalanceCost: 1.25}
}

func deletedAccountFixture(t *testing.T) *service.Account {
	t.Helper()
	account := mustCreateAccount(t, testEntClient(t), &service.Account{
		Name: "deleted-account-" + uuid.NewString(), Type: service.AccountTypeAPIKey,
		Extra: map[string]any{"quota_limit": 100.0},
	})
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM accounts WHERE id=$1`, account.ID)
		require.NoError(t, err)
	})
	return account
}

func assertDeletedAccountPayerState(t *testing.T, cmd *service.UsageBillingCommand, charged bool) {
	t.Helper()
	ctx := context.Background()
	cost := 0.0
	if charged {
		cost = 1.25
	}
	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM users WHERE id=$1`, cmd.UserID).Scan(&balance))
	if cmd.EnterpriseID > 0 {
		require.Equal(t, 100.0, balance, "enterprise consumption preserves the personal wallet")
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM enterprises WHERE id=$1`, cmd.EnterpriseID).Scan(&balance))
		require.Equal(t, 10-cost, balance)
		rows, err := integrationDB.QueryContext(ctx, `SELECT daily_usage_usd,weekly_usage_usd,monthly_usage_usd,total_usage_usd FROM enterprise_quotas WHERE enterprise_id=$1`, cmd.EnterpriseID)
		require.NoError(t, err)
		count := 0
		for rows.Next() {
			var daily, weekly, monthly, total float64
			require.NoError(t, rows.Scan(&daily, &weekly, &monthly, &total))
			require.Equal(t, []float64{cost, cost, cost, cost}, []float64{daily, weekly, monthly, total})
			count++
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		require.Equal(t, 3, count)
	} else {
		require.Equal(t, 100-cost, balance)
	}
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, cmd.RequestID, cmd.APIKeyID).Scan(&count))
	if charged {
		require.Equal(t, 1, count)
	} else {
		require.Zero(t, count)
	}
}

func TestUsageBillingDeletedAccountAfterConsumptionRetainsPayerSettlement(t *testing.T) {
	for _, payer := range []string{"personal", "enterprise"} {
		for _, deletion := range []string{"soft", "physical"} {
			t.Run(payer+"/"+deletion, func(t *testing.T) {
				ctx := context.Background()
				cmd := deletedAccountBillingCommand(t, payer == "enterprise")
				account := deletedAccountFixture(t)
				// Freeze the consumed upstream account, then remove it before the worker settles.
				cmd.AccountID, cmd.AccountType, cmd.AccountQuotaCost = account.ID, account.Type, 0.5
				query := `UPDATE accounts SET deleted_at=NOW() WHERE id=$1`
				if deletion == "physical" {
					query = `DELETE FROM accounts WHERE id=$1`
				}
				_, err := integrationDB.ExecContext(ctx, query, account.ID)
				require.NoError(t, err)
				repo := NewUsageBillingRepository(testEntClient(t), integrationDB)
				result, err := repo.Apply(ctx, cmd)
				require.NoError(t, err)
				require.True(t, result.Applied)
				assertDeletedAccountPayerState(t, cmd, true)
				if deletion == "soft" {
					var used float64
					require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT (extra->>'quota_used')::numeric FROM accounts WHERE id=$1`, account.ID).Scan(&used))
					require.Equal(t, 0.5, used, "the frozen account ID still updates archived quota")
				} else {
					require.Nil(t, result.QuotaState)
				}
				replay, err := repo.Apply(ctx, cmd)
				require.NoError(t, err)
				require.False(t, replay.Applied)
				assertDeletedAccountPayerState(t, cmd, true)
			})
		}
	}
}

func TestUsageBillingAccountSQLFailureRollsBackAndCanRetry(t *testing.T) {
	for _, payer := range []string{"personal", "enterprise"} {
		t.Run(payer, func(t *testing.T) {
			ctx := context.Background()
			cmd := deletedAccountBillingCommand(t, payer == "enterprise")
			account := deletedAccountFixture(t)
			cmd.AccountID, cmd.AccountType, cmd.AccountQuotaCost = account.ID, account.Type, 0.5
			// PostgreSQL numeric conversion fails inside the final account update,
			// after the payer and enterprise quotas have already been touched.
			_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET extra=extra || '{"quota_used":"invalid"}'::jsonb WHERE id=$1`, account.ID)
			require.NoError(t, err)
			repo := NewUsageBillingRepository(testEntClient(t), integrationDB)
			_, err = repo.Apply(ctx, cmd)
			require.Error(t, err)
			assertDeletedAccountPayerState(t, cmd, false)
			_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET extra=extra - 'quota_used' WHERE id=$1`, account.ID)
			require.NoError(t, err)
			result, err := repo.Apply(ctx, cmd)
			require.NoError(t, err)
			require.True(t, result.Applied)
			assertDeletedAccountPayerState(t, cmd, true)
		})
	}
}
