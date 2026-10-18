//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/stretchr/testify/require"
)

// Golden hashes are captured from the pre-enterprise raw fingerprint format,
// including the unquantized ninth-decimal half boundary and payload suffix.
func TestUsageBillingFingerprintPreservesPersonalGoldenHashes(t *testing.T) {
	for _, tc := range []struct{ payload, want string }{
		{"", "c8d0a3ac818d57bc88b6ae9430c600e33241f488b8f6bb3a10a90c312144771f"},
		{"payload", "893aec069eaa41c2cf139e5b0e0d3a9c71c1c9836cf418712a153bc8cf2bf0a3"},
	} {
		cmd := &UsageBillingCommand{
			UserID: 11, AccountID: 22, APIKeyID: 33, AccountType: " apikey ",
			Model: "model", ServiceTier: "priority", ReasoningEffort: "high",
			InputTokens: 101, OutputTokens: 202, CacheCreationTokens: 303, CacheReadTokens: 404,
			ImageCount: 5, MediaType: "image", BalanceCost: 0.000078125,
			APIKeyQuotaCost: 0.000078125, APIKeyRateLimitCost: 0.125, AccountQuotaCost: 2.5,
			RequestPayloadHash: tc.payload,
			// Fields outside enterprise mode must never alter a legacy request hash.
			EnterpriseMemberID: 99, EnterpriseDepartmentID: 98, EnterpriseBalanceCost: 100,
		}
		cmd.Normalize()
		require.Equal(t, tc.want, cmd.RequestFingerprint)
	}
}

func TestUsageBillingFingerprintIncludesEnterpriseSnapshot(t *testing.T) {
	base := UsageBillingCommand{EnterpriseID: 1, EnterpriseDepartmentID: 2, EnterpriseMemberID: 3, EnterpriseBalanceCost: 0.000078125}
	want := buildUsageBillingFingerprint(&base)
	for _, mutate := range []func(*UsageBillingCommand){
		func(c *UsageBillingCommand) { c.EnterpriseID++ },
		func(c *UsageBillingCommand) { c.EnterpriseDepartmentID++ },
		func(c *UsageBillingCommand) { c.EnterpriseMemberID++ },
		func(c *UsageBillingCommand) { c.EnterpriseBalanceCost++ },
	} {
		cmd := base
		mutate(&cmd)
		require.NotEqual(t, want, buildUsageBillingFingerprint(&cmd))
	}
	base.Normalize()
	require.Equal(t, want, base.RequestFingerprint)
	require.Equal(t, 0.00007813, base.EnterpriseBalanceCost)
}

func TestBuildUsageBillingCommandEnterpriseWalletAndSubscription(t *testing.T) {
	departmentID := int64(23)
	for _, subscription := range []bool{false, true} {
		p := &postUsageBillingParams{
			User:   &User{ID: 1, Enterprise: &EnterpriseMemberContext{EnterpriseID: 22, MemberID: 24, DepartmentID: &departmentID}},
			APIKey: &APIKey{ID: 2}, Account: &Account{ID: 3},
			Cost:               &CostBreakdown{TotalCost: 1, ActualCost: 1.25},
			IsSubscriptionBill: subscription, Subscription: &UserSubscription{ID: 4},
		}
		cmd := buildUsageBillingCommand("enterprise-usage", nil, p)
		require.Equal(t, int64(22), cmd.EnterpriseID)
		require.Equal(t, departmentID, cmd.EnterpriseDepartmentID)
		require.Equal(t, int64(24), cmd.EnterpriseMemberID)
		require.Zero(t, cmd.BalanceCost)
		if subscription {
			require.Zero(t, cmd.EnterpriseBalanceCost)
			require.Equal(t, 1.25, cmd.SubscriptionCost)
		} else {
			require.Equal(t, 1.25, cmd.EnterpriseBalanceCost)
			require.Zero(t, cmd.SubscriptionCost)
		}
	}
}

func TestEnterpriseLegacyBillingFailsBeforePersonalMutation(t *testing.T) {
	p := &postUsageBillingParams{User: &User{Enterprise: &EnterpriseMemberContext{EnterpriseID: 1}}}
	// Nil dependencies deliberately make any attempt to touch the personal
	// wallet, key, or account fail this test before an error can be returned.
	require.ErrorIs(t, postUsageBilling(context.Background(), p, nil), ErrEnterpriseBillingLegacyUnsupported)
	p.Cost, p.APIKey, p.Account = &CostBreakdown{ActualCost: 1}, &APIKey{}, &Account{}
	applied, err := applyUsageBilling(context.Background(), "request", nil, p, &billingDeps{}, nil)
	require.False(t, applied)
	require.ErrorIs(t, err, ErrEnterpriseBillingLegacyUnsupported)
}

type enterpriseLegacyUsageUserRepo struct{ UserRepository }

func (*enterpriseLegacyUsageUserRepo) GetByID(context.Context, int64) (*User, error) {
	return &User{ID: 1}, nil
}

func TestEnterpriseLegacyUsageServiceRejectsMembershipAndLookupFailure(t *testing.T) {
	for _, lookupFails := range []bool{false, true} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
		mock.ExpectBegin()
		query := mock.ExpectQuery(`SELECT .*FROM "enterprise_members"`).WithArgs(int64(1))
		if lookupFails {
			query.WillReturnError(errors.New("enterprise membership unavailable"))
		} else {
			query.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(2))
		}
		mock.ExpectRollback()
		svc := NewUsageService(nil, &enterpriseLegacyUsageUserRepo{}, client, nil)
		_, err = svc.Create(context.Background(), CreateUsageLogRequest{UserID: 1, ActualCost: 2})
		if lookupFails {
			require.ErrorContains(t, err, "check enterprise membership")
		} else {
			require.ErrorIs(t, err, ErrEnterpriseBillingLegacyUnsupported)
		}
		mock.ExpectClose()
		require.NoError(t, client.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestEnterpriseGatewayUsageLogSnapshot(t *testing.T) {
	departmentID := int64(8)
	user := &User{ID: 1, Enterprise: &EnterpriseMemberContext{EnterpriseID: 7, DepartmentID: &departmentID}}
	log := (&GatewayService{}).buildRecordUsageLog(context.Background(), &recordUsageCoreInput{}, &ForwardResult{}, &APIKey{}, user, &Account{}, nil, "model", 1, 1, 1, BillingTypeBalance, false, nil)
	require.Equal(t, int64(7), *log.EnterpriseID)
	require.Equal(t, int64(8), *log.EnterpriseDepartmentID)
	departmentID = 9
	require.Equal(t, int64(8), *log.EnterpriseDepartmentID, "log owns an immutable department snapshot")
}

func TestUsageLogPayerIdentityCapturesFrozenScopeAndClearsPersonalScope(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		enterprise, department int64
	}{
		{"personal", 0, 0},
		{"personal_ignores_current_department", 0, 8},
		{"enterprise_direct_member", 7, 0},
		{"enterprise_department_member", 7, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldEnterprise, oldDepartment := int64(100), int64(200)
			log := &UsageLog{EnterpriseID: &oldEnterprise, EnterpriseDepartmentID: &oldDepartment}
			ApplyUsageLogPayerIdentity(log, tc.enterprise, tc.department)
			if tc.enterprise == 0 {
				require.Nil(t, log.EnterpriseID)
				require.Nil(t, log.EnterpriseDepartmentID)
				return
			}
			require.Equal(t, tc.enterprise, *log.EnterpriseID)
			if tc.department == 0 {
				require.Nil(t, log.EnterpriseDepartmentID)
			} else {
				require.Equal(t, tc.department, *log.EnterpriseDepartmentID)
			}
		})
	}
	require.NotPanics(t, func() { ApplyUsageLogPayerIdentity(nil, 7, 8) })
}

func TestEnterpriseOpenAIUsageLogSnapshot(t *testing.T) {
	departmentID := int64(8)
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	billing := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	users := &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, billing, users, &openAIRecordUsageSubRepoStub{}, nil)
	// Use a model with built-in test pricing; unknown models deliberately produce
	// a zero-cost log, which would not exercise enterprise wallet selection.
	usage := OpenAIUsage{InputTokens: 1000}
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{RequestID: "enterprise-openai", Model: "gpt-5.1", Usage: usage},
		APIKey: &APIKey{ID: 2}, User: &User{ID: 1, Enterprise: &EnterpriseMemberContext{EnterpriseID: 7, MemberID: 9, DepartmentID: &departmentID}}, Account: &Account{ID: 3},
	})
	require.NoError(t, err)
	require.Equal(t, int64(7), *logs.lastLog.EnterpriseID)
	require.Equal(t, departmentID, *logs.lastLog.EnterpriseDepartmentID)
	require.Zero(t, billing.lastCmd.BalanceCost)
	require.Positive(t, billing.lastCmd.EnterpriseBalanceCost)
	expected := expectedOpenAICost(t, svc, "gpt-5.1", usage, 1.1)
	require.Equal(t, QuantizeUsageBillingAmount(expected.ActualCost), billing.lastCmd.EnterpriseBalanceCost)
	require.Zero(t, users.deductCalls)
}
