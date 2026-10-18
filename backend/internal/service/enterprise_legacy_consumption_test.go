//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type legacyConsumptionMembershipReader struct {
	member *EnterpriseMemberContext
	err    error
	calls  int
}

func (r *legacyConsumptionMembershipReader) GetMemberContextByUserID(context.Context, int64) (*EnterpriseMemberContext, error) {
	r.calls++
	return r.member, r.err
}

type legacyConsumptionUserRepo struct {
	UserRepository
	calls  int
	amount float64
}

func (r *legacyConsumptionUserRepo) UpdateBalance(_ context.Context, _ int64, amount float64) error {
	r.calls++
	r.amount = amount
	return nil
}

func TestEnterpriseLegacyUserBalanceConsumptionRejectsMembershipAndLookupFailure(t *testing.T) {
	lookupErr := errors.New("membership unavailable")
	for _, tc := range []struct {
		name   string
		reader *legacyConsumptionMembershipReader
		want   error
	}{
		{name: "active member", reader: &legacyConsumptionMembershipReader{member: &EnterpriseMemberContext{EnterpriseID: 1, MemberStatus: EnterpriseMemberStatusActive}}, want: ErrEnterpriseBillingLegacyUnsupported},
		{name: "disabled member", reader: &legacyConsumptionMembershipReader{member: &EnterpriseMemberContext{EnterpriseID: 1, MemberStatus: EnterpriseMemberStatusDisabled}}, want: ErrEnterpriseBillingLegacyUnsupported},
		{name: "lookup failed", reader: &legacyConsumptionMembershipReader{err: lookupErr}, want: lookupErr},
		{name: "unconfigured lookup", want: ErrEnterpriseBillingLegacyUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &legacyConsumptionUserRepo{}
			svc := NewUserService(repo, nil, nil, nil)
			if tc.reader != nil {
				svc.SetEnterpriseMembershipReader(tc.reader)
			}
			require.ErrorIs(t, svc.UpdateBalance(context.Background(), 1, -2), tc.want)
			require.Zero(t, repo.calls, "enterprise money and personal money must remain unchanged")
		})
	}
}

func TestEnterpriseLegacyUserBalanceConsumptionAllowsConfirmedPersonalUser(t *testing.T) {
	repo := &legacyConsumptionUserRepo{}
	reader := &legacyConsumptionMembershipReader{}
	svc := NewUserService(repo, nil, nil, nil)
	svc.SetEnterpriseMembershipReader(reader)
	require.NoError(t, svc.UpdateBalance(context.Background(), 1, -2))
	require.Equal(t, 1, reader.calls)
	require.Equal(t, 1, repo.calls)
	require.Equal(t, -2.0, repo.amount)
}

func TestEnterpriseLegacyUserBalancePositiveAdjustmentKeepsExistingBehavior(t *testing.T) {
	repo := &legacyConsumptionUserRepo{}
	reader := &legacyConsumptionMembershipReader{member: &EnterpriseMemberContext{EnterpriseID: 1}}
	svc := NewUserService(repo, nil, nil, nil)
	svc.SetEnterpriseMembershipReader(reader)
	require.NoError(t, svc.UpdateBalance(context.Background(), 1, 2))
	require.NoError(t, svc.UpdateBalance(context.Background(), 1, 0))
	require.Zero(t, reader.calls, "positive administrative adjustment does not use the consumption guard")
	require.Equal(t, 2, repo.calls)
}
