package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type quotaNotifySettingsStub struct {
	SettingRepository
	values map[string]string
	reads  int
}

func (s *quotaNotifySettingsStub) GetValue(_ context.Context, key string) (string, error) {
	s.reads++
	return s.values[key], nil
}

type quotaNotifyAccountReaderStub struct {
	account *Account
	err     error
	reads   int
}

func (s *quotaNotifyAccountReaderStub) GetByID(context.Context, int64) (*Account, error) {
	s.reads++
	return s.account, s.err
}

func TestNotifyAccountQuotaRequiresPostIncrementState(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeBedrock} {
		for _, limitKey := range []string{"quota_limit", "quota_daily_limit", "quota_weekly_limit"} {
			t.Run(accountType+"/"+limitKey, func(t *testing.T) {
				p := &postUsageBillingParams{
					Cost:                  &CostBreakdown{TotalCost: 10},
					Account:               &Account{ID: 1, Type: accountType, Extra: map[string]any{limitKey: 100.0}},
					AccountRateMultiplier: 1,
				}
				require.True(t, p.shouldUpdateAccountQuota())
				settings := &quotaNotifySettingsStub{}
				reader := &quotaNotifyAccountReaderStub{account: p.Account}
				deps := &billingDeps{balanceNotifyService: NewBalanceNotifyService(&EmailService{}, settings, reader)}

				// A successful payer settlement may skip the deleted account's
				// counter. It must not trigger notification queries or emails.
				notifyAccountQuota(p, deps, &UsageBillingApplyResult{Applied: true})
				require.Zero(t, settings.reads)
				require.Zero(t, reader.reads)

				// A real increment still reaches the notification service. The
				// global toggle is off so these control cases send no email.
				notifyAccountQuota(p, deps, &UsageBillingApplyResult{Applied: true, QuotaState: &AccountQuotaState{}})
				require.Equal(t, 1, settings.reads)

				// A nil result identifies the legacy path, which has no RETURNING
				// state and must retain its fresh-account notification fallback.
				notifyAccountQuota(p, deps, nil)
				require.Equal(t, 2, settings.reads)
			})
		}
	}
}

func TestNotifyAccountQuotaSkipsAccountsWithoutLimits(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeBedrock} {
		t.Run(accountType, func(t *testing.T) {
			p := &postUsageBillingParams{
				Cost:                  &CostBreakdown{TotalCost: 10},
				Account:               &Account{ID: 1, Type: accountType},
				AccountRateMultiplier: 1,
			}
			require.False(t, p.shouldUpdateAccountQuota())
			settings := &quotaNotifySettingsStub{}
			deps := &billingDeps{balanceNotifyService: NewBalanceNotifyService(&EmailService{}, settings, nil)}
			notifyAccountQuota(p, deps, nil)
			notifyAccountQuota(p, deps, &UsageBillingApplyResult{Applied: true, QuotaState: &AccountQuotaState{}})
			require.Zero(t, settings.reads, "unlimited accounts must not query notification settings")
		})
	}
}

func TestAccountQuotaNotifyFreshAccountFallback(t *testing.T) {
	snapshot := &Account{ID: 1, Name: "snapshot"}
	fresh := &Account{ID: 1, Name: "fresh"}
	for _, tc := range []struct {
		name    string
		account *Account
		err     error
		want    *Account
	}{
		{name: "fresh account", account: fresh, want: fresh},
		{name: "deleted account", err: ErrAccountNotFound},
		{name: "wrapped not found", err: fmt.Errorf("lookup: %w", ErrAccountNotFound)},
		{name: "transient failure", err: errors.New("database unavailable"), want: snapshot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &quotaNotifyAccountReaderStub{account: tc.account, err: tc.err}
			s := &BalanceNotifyService{accountRepo: reader}
			got := s.fetchFreshAccount(context.Background(), snapshot)
			if tc.want == nil {
				require.Nil(t, got)
			} else {
				require.Same(t, tc.want, got)
			}
			require.Equal(t, 1, reader.reads)
		})
	}
	t.Run("legacy without reader", func(t *testing.T) {
		s := &BalanceNotifyService{}
		require.Same(t, snapshot, s.fetchFreshAccount(context.Background(), snapshot))
	})
}

func TestCheckAccountQuotaAfterIncrementSkipsDeletedAccount(t *testing.T) {
	settings := &quotaNotifySettingsStub{values: map[string]string{
		SettingKeyAccountQuotaNotifyEnabled: "true",
		SettingKeyAccountQuotaNotifyEmails:  `[{"email":"admin@example.com","verified":true}]`,
	}}
	reader := &quotaNotifyAccountReaderStub{err: ErrAccountNotFound}
	s := NewBalanceNotifyService(&EmailService{}, settings, reader)
	snapshot := &Account{
		ID:   1,
		Type: AccountTypeAPIKey,
		Extra: map[string]any{
			"quota_notify_total_enabled":   true,
			"quota_notify_total_threshold": 20.0,
			"quota_limit":                  100.0,
			"quota_used":                   85.0,
		},
	}
	// Reconstructing old usage as 85 - 10 would invent a crossing at 80.
	// The missing account instead suppresses the snapshot and the whole check.
	require.Nil(t, s.fetchFreshAccount(context.Background(), snapshot))
	require.NotPanics(t, func() {
		s.CheckAccountQuotaAfterIncrement(context.Background(), snapshot, 10, nil)
	})
	require.Equal(t, 2, reader.reads)
}
