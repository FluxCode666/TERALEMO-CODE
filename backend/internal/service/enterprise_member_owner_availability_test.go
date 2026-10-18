//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type enterpriseOwnerAvailabilityMembers struct {
	*enterpriseManagementTestMembers
	firstLookup bool
}

func (r *enterpriseOwnerAvailabilityMembers) GetByID(ctx context.Context, enterpriseID, memberID int64) (*EnterpriseMember, error) {
	if !r.firstLookup {
		r.firstLookup = true
		require.Equal(r.t, true, ctx.Value(enterpriseManagementTestTxKey{}), "target availability must be read inside the transaction")
		require.Positive(r.t, r.countCalls, "the enterprise lock must precede the target lookup")
	}
	return r.enterpriseManagementTestMembers.GetByID(ctx, enterpriseID, memberID)
}

func TestEnterpriseMemberLastOwnerGuardUsesAccountAvailability(t *testing.T) {
	for _, route := range []string{"admin", "enterprise"} {
		for _, operation := range []string{"demote", "disable", "remove"} {
			for _, account := range []struct {
				name       string
				status     string
				owners     int64
				wantError  error
				targetSelf bool
			}{
				{name: "only_available_owner", status: StatusActive, owners: 1, wantError: ErrEnterpriseLastOwner, targetSelf: true},
				{name: "another_available_owner", status: StatusActive, owners: 2},
				{name: "disabled_account", status: StatusDisabled, owners: 1},
				{name: "deleted_account", status: "", owners: 1},
			} {
				t.Run(route+"/"+operation+"/"+account.name, func(t *testing.T) {
					f := newEnterpriseManagementTestFixture(t)
					targetID := int64(20)
					if account.targetSelf {
						targetID = 10
					}
					target := f.m.items[targetID]
					target.Role = EnterpriseRoleOwner
					target.UserStatus = account.status
					f.m.items[targetID] = target
					f.m.owners = account.owners
					f.s.members = &enterpriseOwnerAvailabilityMembers{enterpriseManagementTestMembers: f.m}
					f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 10, Role: EnterpriseRoleOwner, MemberStatus: EnterpriseMemberStatusActive, EnterpriseStatus: EnterpriseStatusActive}
					actor := EnterpriseActor{UserID: 10, IsAdmin: route == "admin"}
					var err error
					switch operation {
					case "demote":
						_, err = f.s.UpdateMember(context.Background(), actor, 1, targetID, UpdateEnterpriseMemberInput{Role: enterpriseTestString(EnterpriseRoleMember)})
					case "disable":
						_, err = f.s.UpdateMember(context.Background(), actor, 1, targetID, UpdateEnterpriseMemberInput{Status: enterpriseTestString(EnterpriseMemberStatusDisabled)})
					case "remove":
						err = f.s.RemoveMember(context.Background(), actor, 1, targetID)
					}
					if account.wantError != nil {
						require.ErrorIs(t, err, account.wantError)
						require.Zero(t, f.m.updated+f.m.deleted)
						require.Empty(t, f.a.logs)
						require.Empty(t, f.auth.ids)
						return
					}
					require.NoError(t, err)
					require.Equal(t, 1, f.m.updated+f.m.deleted)
					require.Len(t, f.a.logs, 1)
					require.Contains(t, f.auth.ids, target.UserID)
					if account.status != StatusActive {
						require.Equal(t, 1, f.m.countCalls, "an unavailable target must not be treated as an owner removed from the available set")
					}
				})
			}
		}
	}
}
