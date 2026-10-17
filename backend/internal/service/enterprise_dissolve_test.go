//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type enterpriseDissolveTestRepo struct {
	*enterpriseManagementTestEnterpriseRepo
	dissolved, deleted bool
	t                  *testing.T
}

func (r *enterpriseDissolveTestRepo) Dissolve(ctx context.Context, _ int64) error {
	require.Equal(r.t, true, ctx.Value(enterpriseManagementTestTxKey{}))
	r.dissolved = true
	return nil
}

func (r *enterpriseDissolveTestRepo) Delete(ctx context.Context, _ int64) error {
	require.Equal(r.t, true, ctx.Value(enterpriseManagementTestTxKey{}))
	r.deleted = true
	return nil
}

func TestEnterpriseServiceDissolveRequiresExplicitAdminIntent(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		admin, dissolve, empty bool
		want                   error
	}{
		{"nonadmin", false, true, false, ErrEnterprisePermissionDenied},
		{"ordinary delete refuses members", true, false, false, ErrEnterpriseNotEmpty},
		{"dissolve bypasses last owner", true, true, false, nil},
		{"empty ordinary delete", true, false, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnterpriseManagementTestFixture(t)
			if tc.empty {
				f.e.ids = nil
			}
			f.e.e.Balance = 42.25
			r := &enterpriseDissolveTestRepo{enterpriseManagementTestEnterpriseRepo: f.e, t: t}
			f.s.enterprises = r
			var err error
			if tc.dissolve {
				err = f.s.DissolveEnterprise(context.Background(), EnterpriseActor{UserID: 99, IsAdmin: tc.admin}, 1)
			} else {
				err = f.s.DeleteEnterprise(context.Background(), EnterpriseActor{UserID: 99, IsAdmin: tc.admin}, 1)
			}
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.False(t, r.dissolved || r.deleted)
				require.Empty(t, f.a.logs)
				require.Empty(t, f.auth.ids)
				return
			}
			require.Equal(t, tc.dissolve, r.dissolved)
			require.Equal(t, !tc.dissolve, r.deleted)
			require.Len(t, f.a.logs, 1)
			require.Equal(t, EnterpriseAuditActionEnterpriseDelete, f.a.logs[0].Action)
			require.Equal(t, len(f.e.ids), f.a.logs[0].Detail["member_count"])
			require.Equal(t, 42.25, f.a.logs[0].Detail["remaining_balance"])
			require.ElementsMatch(t, f.e.ids, f.auth.ids)
		})
	}
}

func TestEnterpriseServiceDissolveAuditFailureDoesNotInvalidate(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	f.s.enterprises = &enterpriseDissolveTestRepo{enterpriseManagementTestEnterpriseRepo: f.e, t: t}
	want := errors.New("audit unavailable")
	f.a.err = want
	require.ErrorIs(t, f.s.DissolveEnterprise(context.Background(), EnterpriseActor{IsAdmin: true}, 1), want)
	require.Empty(t, f.auth.ids)
	require.Empty(t, f.billing.scopes)
}

func TestEnterpriseCreatedAccountsExplicitlyStartWithZeroPersonalBalance(t *testing.T) {
	for _, admin := range []bool{false, true} {
		t.Run(map[bool]string{false: "enterprise owner", true: "administrator"}[admin], func(t *testing.T) {
			f := newEnterpriseManagementTestFixture(t)
			f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 10, Role: EnterpriseRoleOwner, MemberStatus: EnterpriseMemberStatusActive, EnterpriseStatus: EnterpriseStatusActive}
			_, err := f.s.CreateMember(context.Background(), EnterpriseActor{UserID: 10, IsAdmin: admin}, 1, CreateEnterpriseMemberInput{Email: "new@example.com", Password: "password123"})
			require.NoError(t, err)
			require.Len(t, f.creator.inputs, 1)
			require.NotNil(t, f.creator.inputs[0].Balance)
			require.Zero(t, *f.creator.inputs[0].Balance)
		})
	}
}
