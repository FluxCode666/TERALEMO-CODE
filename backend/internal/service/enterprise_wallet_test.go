package service

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
)

type walletMembershipStub struct {
	member *EnterpriseMemberContext
	err    error
}

type walletUserFailureStub struct {
	UserRepository
	err error
}

func (s walletUserFailureStub) GetByID(context.Context, int64) (*User, error) { return nil, s.err }

func (s walletMembershipStub) GetMemberContextByUserID(context.Context, int64) (*EnterpriseMemberContext, error) {
	return s.member, s.err
}

func TestEnterpriseWalletChecksMembershipIncludingDisabled(t *testing.T) {
	for _, status := range []string{EnterpriseMemberStatusActive, EnterpriseMemberStatusDisabled} {
		t.Run(status, func(t *testing.T) {
			reader := walletMembershipStub{member: &EnterpriseMemberContext{MemberStatus: status}}
			payment := &PaymentService{enterpriseMemberships: reader}
			// No payment configuration/provider is installed: rejecting here also
			// proves the operation has no downstream side effects.
			_, err := payment.CreateOrder(context.Background(), CreateOrderRequest{UserID: 1, OrderType: "subscription"})
			require.ErrorIs(t, err, ErrEnterpriseMemberWalletDisabled)
			promo := &PromoService{enterpriseMemberships: reader}
			require.ErrorIs(t, promo.ApplyPromoCode(context.Background(), 1, "bonus"), ErrEnterpriseMemberWalletDisabled)
			repo := &redeemRejectRepo{code: RedeemCode{ID: 1, Code: "balance", Type: RedeemTypeBalance, Status: StatusUnused}}
			redeem := NewRedeemService(repo, nil, nil, nil, nil, nil, nil, nil)
			redeem.SetEnterpriseMembershipReader(reader)
			_, err = redeem.Redeem(context.Background(), 1, "balance")
			require.ErrorIs(t, err, ErrEnterpriseMemberWalletDisabled)
			require.False(t, repo.useCalled)
			require.Equal(t, StatusUnused, repo.code.Status)
		})
	}
	require.NoError(t, checkEnterprisePersonalWallet(context.Background(), walletMembershipStub{}, 1))
	failure := errors.New("membership lookup unavailable")
	require.ErrorIs(t, checkEnterprisePersonalWallet(context.Background(), walletMembershipStub{err: failure}, 1), failure)
}

func TestEnterpriseWalletDoesNotInterceptNonBalanceRedeem(t *testing.T) {
	for _, codeType := range []string{RedeemTypeConcurrency, RedeemTypeSubscription} {
		t.Run(codeType, func(t *testing.T) {
			groupID := int64(1)
			repo := &redeemRejectRepo{code: RedeemCode{ID: 1, Code: "non-balance", Type: codeType, Status: StatusUnused, GroupID: &groupID}}
			reachedUserValidation := errors.New("reached user validation")
			redeem := NewRedeemService(repo, walletUserFailureStub{err: reachedUserValidation}, nil, nil, nil, nil, nil, nil)
			redeem.SetEnterpriseMembershipReader(walletMembershipStub{member: &EnterpriseMemberContext{}})
			_, err := redeem.Redeem(context.Background(), 1, "non-balance")
			require.ErrorIs(t, err, reachedUserValidation)
			require.NotErrorIs(t, err, ErrEnterpriseMemberWalletDisabled)
			require.False(t, repo.useCalled)
		})
	}
}
