//go:build unit

package service

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type enterpriseUserMembersStub struct {
	EnterpriseMemberRepository
	member *EnterpriseMember
	owners int64
	gets   int
	counts int
}

func (s *enterpriseUserMembersStub) GetByUserID(context.Context, int64) (*EnterpriseMember, error) {
	s.gets++
	return s.member, nil
}

func (s *enterpriseUserMembersStub) CountActiveOwners(context.Context, int64) (int64, error) {
	s.counts++
	return s.owners, nil
}

func TestAdminUserEnterpriseConstraints(t *testing.T) {
	for _, tc := range []struct {
		name         string
		role         string
		memberStatus string
		userStatus   string
		owners       int64
		input        UpdateUserInput
		wantErr      error
	}{
		{name: "member cannot become admin", role: EnterpriseRoleMember, input: UpdateUserInput{Role: RoleAdmin}, wantErr: ErrEnterpriseMemberAdminForbidden},
		{name: "disabled member cannot become admin", role: EnterpriseRoleMember, memberStatus: StatusDisabled, input: UpdateUserInput{Role: RoleAdmin}, wantErr: ErrEnterpriseMemberAdminForbidden},
		{name: "last available owner cannot be disabled", role: EnterpriseRoleOwner, owners: 1, input: UpdateUserInput{Status: StatusDisabled}, wantErr: ErrEnterpriseLastOwner},
		{name: "one of two available owners can be disabled", role: EnterpriseRoleOwner, owners: 2, input: UpdateUserInput{Status: StatusDisabled}},
		{name: "already disabled account does not consume an available owner", role: EnterpriseRoleOwner, userStatus: StatusDisabled, owners: 1, input: UpdateUserInput{Status: StatusDisabled}},
		{name: "disabled member owner is not available", role: EnterpriseRoleOwner, memberStatus: StatusDisabled, owners: 1, input: UpdateUserInput{Status: StatusDisabled}},
		{name: "ordinary member can be disabled", role: EnterpriseRoleMember, owners: 1, input: UpdateUserInput{Status: StatusDisabled}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.memberStatus == "" {
				tc.memberStatus = StatusActive
			}
			if tc.userStatus == "" {
				tc.userStatus = StatusActive
			}
			members := &enterpriseUserMembersStub{member: &EnterpriseMember{ID: 7, EnterpriseID: 9, UserID: 42, Role: tc.role, Status: tc.memberStatus}, owners: tc.owners}
			repo := &rpmUserRepoStub{userRepoStub: &userRepoStub{user: &User{ID: 42, Role: RoleUser, Status: tc.userStatus}}}
			s := &adminServiceImpl{userRepo: repo, enterpriseMembers: members}
			_, err := s.UpdateUser(context.Background(), 42, &tc.input)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.Nil(t, repo.lastUpdated)
			} else {
				require.NoError(t, err)
				require.NotNil(t, repo.lastUpdated)
			}
			require.Equal(t, 1, members.counts)
			require.Equal(t, 2, members.gets, "membership must be rechecked after locking")
		})
	}
}

func TestAdminDeleteUserRequiresEnterpriseRemoval(t *testing.T) {
	for _, status := range []string{StatusActive, StatusDisabled} {
		t.Run(status, func(t *testing.T) {
			s := &adminServiceImpl{enterpriseMembers: &enterpriseUserMembersStub{member: &EnterpriseMember{ID: 7, EnterpriseID: 9, UserID: 42, Status: status}}}
			err := s.DeleteUser(context.Background(), 42)
			require.ErrorIs(t, err, ErrEnterpriseMemberUserDeleteForbidden)
			require.Equal(t, 409, infraerrors.Code(err))
		})
	}
}

func TestAdminEnterprisePersonalBalanceAdjustmentSkipsAffiliateRebate(t *testing.T) {
	for _, status := range []string{StatusActive, StatusDisabled} {
		t.Run(status, func(t *testing.T) {
			repo := &balanceUserRepoStub{userRepoStub: &userRepoStub{user: &User{ID: 42, Balance: 10}}}
			affiliate := &adminRechargeAffiliateAccruerStub{}
			s := &adminServiceImpl{
				userRepo:          repo,
				redeemCodeRepo:    &balanceRedeemRepoStub{},
				settingService:    adminRechargeSettingService(true),
				affiliateService:  affiliate,
				enterpriseMembers: &enterpriseUserMembersStub{member: &EnterpriseMember{ID: 7, EnterpriseID: 9, UserID: 42, Status: status}},
			}
			u, err := s.UpdateUserBalance(context.Background(), 42, 5, "add", "explicit frozen wallet adjustment")
			require.NoError(t, err)
			require.Equal(t, 15.0, u.Balance)
			require.Empty(t, affiliate.calls)
		})
	}
}
