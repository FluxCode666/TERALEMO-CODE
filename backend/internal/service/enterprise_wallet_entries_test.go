//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

type enterpriseAffiliateTransferStub struct {
	AffiliateRepository
	called bool
}

func (s *enterpriseAffiliateTransferStub) TransferQuotaToBalance(context.Context, int64) (float64, float64, error) {
	s.called = true
	return 12, 30, nil
}

func TestEnterpriseAffiliateTransferRejectedBeforeQuotaMutation(t *testing.T) {
	for _, status := range []string{StatusActive, StatusDisabled} {
		t.Run(status, func(t *testing.T) {
			repo := &enterpriseAffiliateTransferStub{}
			s := NewAffiliateService(repo, nil, nil, nil)
			s.SetEnterpriseMembershipReader(walletMembershipStub{member: &EnterpriseMemberContext{MemberStatus: status}})
			transferred, balance, err := s.TransferAffiliateQuota(context.Background(), 42)
			require.ErrorIs(t, err, ErrEnterpriseMemberWalletDisabled)
			require.Zero(t, transferred)
			require.Zero(t, balance)
			require.False(t, repo.called)
		})
	}
}

func TestEnterpriseHistoricalPaymentFulfillmentCreditsPersonalWallet(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	ensurePaymentAuditOrderActionUniqueIndex(t, ctx, client)
	order := createPaymentFulfillmentSubscriptionOrder(t, ctx, client, OrderStatusPaid, time.Now())
	order, err := client.PaymentOrder.UpdateOneID(order.ID).SetOrderType(payment.OrderTypeBalance).ClearPlanID().ClearSubscriptionGroupID().ClearSubscriptionDays().Save(ctx)
	require.NoError(t, err)
	repo := &paymentFulfillmentRedeemRepo{}
	credited := 0.0
	userRepo := &mockUserRepo{getByIDUser: &User{ID: order.UserID}}
	userRepo.updateBalanceFn = func(_ context.Context, id int64, amount float64) error {
		require.Equal(t, order.UserID, id)
		credited += amount
		return nil
	}
	redeem := NewRedeemService(repo, userRepo, nil, nil, nil, client, nil, nil)
	redeem.SetEnterpriseMembershipReader(walletMembershipStub{member: &EnterpriseMemberContext{}})
	s := &PaymentService{entClient: client, redeemService: redeem, userRepo: userRepo}
	require.NoError(t, s.ExecuteBalanceFulfillment(ctx, order.ID))
	require.InDelta(t, order.Amount, credited, 1e-8)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.NoError(t, s.ExecuteBalanceFulfillment(ctx, order.ID))
	require.InDelta(t, order.Amount, credited, 1e-8, "callback retry cannot credit twice")
}

func TestEnterpriseTrustedRedeemDoesNotApplyPublicWalletGuard(t *testing.T) {
	for _, admin := range []bool{false, true} {
		repo := &redeemRejectRepo{code: RedeemCode{ID: 1, Code: "old-payment", Type: RedeemTypeBalance, Status: StatusUnused}}
		reachedUser := errors.New("reached trusted user validation")
		s := NewRedeemService(repo, walletUserFailureStub{err: reachedUser}, nil, nil, nil, nil, nil, nil)
		s.SetEnterpriseMembershipReader(walletMembershipStub{member: &EnterpriseMemberContext{}})
		var err error
		if admin {
			_, err = s.RedeemForAdminFulfillment(context.Background(), 42, repo.code.Code)
		} else {
			_, err = s.redeemForPaymentFulfillment(context.Background(), 42, repo.code.Code)
		}
		require.ErrorIs(t, err, reachedUser)
		require.NotErrorIs(t, err, ErrEnterpriseMemberWalletDisabled)
	}
}

func TestEnterpriseHistoricalRefundCompensationStaysPersonal(t *testing.T) {
	ctx := context.Background()
	var credited float64
	userRepo := &mockUserRepo{updateBalanceFn: func(_ context.Context, userID int64, amount float64) error {
		require.EqualValues(t, 42, userID)
		credited += amount
		return nil
	}}
	// A failing membership reader proves neither compensation path consults
	// current membership or redirects the historical personal payment.
	s := &PaymentService{userRepo: userRepo, enterpriseMemberships: walletMembershipStub{err: errors.New("must not read membership")}}
	plan := &RefundPlan{Order: &dbent.PaymentOrder{UserID: 42}, DeductionType: payment.DeductionTypeBalance, BalanceToDeduct: 7}
	require.NoError(t, s.returnHeldRefundDeduction(ctx, plan))
	require.Equal(t, 7.0, credited)
	require.True(t, s.RollbackRefund(ctx, plan, errors.New("gateway failed")))
	require.Equal(t, 14.0, credited)
}
