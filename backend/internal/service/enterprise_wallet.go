package service

import "context"

// EnterpriseMembershipReader keeps personal-wallet checks independent of the
// administration and billing services. Disabled memberships still own a wallet.
type EnterpriseMembershipReader interface {
	GetMemberContextByUserID(context.Context, int64) (*EnterpriseMemberContext, error)
}

func checkEnterprisePersonalWallet(ctx context.Context, reader EnterpriseMembershipReader, userID int64) error {
	if reader == nil {
		return nil
	}
	member, err := reader.GetMemberContextByUserID(ctx, userID)
	if err != nil {
		return err
	}
	if member != nil {
		return ErrEnterpriseMemberWalletDisabled
	}
	return nil
}

func (s *PaymentService) SetEnterpriseMembershipReader(reader EnterpriseMembershipReader) {
	s.enterpriseMemberships = reader
}

func (s *RedeemService) SetEnterpriseMembershipReader(reader EnterpriseMembershipReader) {
	s.enterpriseMemberships = reader
}

func (s *PromoService) SetEnterpriseMembershipReader(reader EnterpriseMembershipReader) {
	s.enterpriseMemberships = reader
}

func (s *AffiliateService) SetEnterpriseMembershipReader(reader EnterpriseMembershipReader) {
	s.enterpriseMemberships = reader
}
