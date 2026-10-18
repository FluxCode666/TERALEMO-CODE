//go:build unit

package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func concurrencyInt(v int) *int { return &v }

func (r *enterpriseManagementTestEnterpriseRepo) Create(_ context.Context, e *Enterprise) error {
	e.ID = 1
	r.e = *e
	return nil
}

func TestEnterpriseMemberCreationConcurrencyEntrypoints(t *testing.T) {
	for _, entry := range []string{"enterprise_create", "enterprise_import", "admin_create", "new_enterprise_owner"} {
		for _, policy := range []string{"platform_setting", "config_fallback", "enterprise_override"} {
			t.Run(entry+"/"+policy, func(t *testing.T) {
				f, users, _ := newEnterpriseImportTestFixture(t)
				f.s.authCache = nil
				cfg := &config.Config{}
				cfg.Default.UserConcurrency = 11
				repo := &settingRepoStub{values: map[string]string{SettingKeyDefaultConcurrency: "7"}}
				f.s.settings = NewSettingService(repo, cfg)
				want := 7
				if policy == "config_fallback" {
					repo.values[SettingKeyDefaultConcurrency] = "invalid"
					want = 11
				}
				if policy == "enterprise_override" {
					f.e.e.MemberConcurrency = concurrencyInt(19)
					want = 19
				}
				actor := EnterpriseActor{UserID: 10}
				in := CreateEnterpriseMemberInput{Email: "concurrency@example.com", Password: "password-123", Role: EnterpriseRoleMember}
				var userID int64
				switch entry {
				case "enterprise_create", "admin_create":
					actor.IsAdmin = entry == "admin_create"
					m, err := f.s.CreateMember(context.Background(), actor, 1, in)
					require.NoError(t, err)
					userID = m.UserID
				case "enterprise_import":
					result, err := f.s.CommitImport(context.Background(), actor, 1, []EnterpriseImportRow{{Row: 1, Email: in.Email, Role: EnterpriseRoleMember, Password: &in.Password}})
					require.NoError(t, err)
					require.Equal(t, 1, result.Summary.Created)
					userID = result.Rows[0].UserID
				case "new_enterprise_owner":
					// A newly created enterprise starts without an override, even
					// when another enterprise in the fixture has one configured.
					if policy == "enterprise_override" {
						want = 7
					}
					_, err := f.s.CreateEnterprise(context.Background(), EnterpriseActor{UserID: 10, IsAdmin: true}, CreateEnterpriseInput{Name: "New", Owner: &EnterpriseOwnerInput{Email: in.Email, Password: in.Password}})
					require.NoError(t, err)
					u, err := users.GetByEmail(context.Background(), in.Email)
					require.NoError(t, err)
					userID = u.ID
				}
				require.Equal(t, want, users.users[userID].Concurrency)
				require.Zero(t, users.users[userID].Balance)
			})
		}
	}
}

func TestEnterpriseExistingMemberInheritsConcurrency(t *testing.T) {
	for _, override := range []*int{nil, concurrencyInt(23)} {
		f, users, _ := newEnterpriseImportTestFixture(t)
		f.s.authCache = nil
		f.e.e.MemberConcurrency = override
		u := users.users[30]
		u.Concurrency = 9
		users.users[30] = u
		m, err := f.s.CreateMember(context.Background(), EnterpriseActor{IsAdmin: true}, 1, CreateEnterpriseMemberInput{UserID: 30})
		require.NoError(t, err)
		want := 9
		if override != nil {
			want = *override
		}
		require.Equal(t, want, users.users[30].Concurrency)
		require.NoError(t, f.s.RemoveMember(context.Background(), EnterpriseActor{IsAdmin: true}, 1, m.ID))
		require.Equal(t, want, users.users[30].Concurrency)
	}
}

func TestEnterpriseUpdateMemberConcurrencyAuditAndDelayedInvalidation(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	f.e.ids = []int64{20, 30}
	f.e.e.MemberConcurrency = concurrencyInt(5)
	invalidator := &enterpriseDelayedInvalidationStub{calls: make(chan enterpriseDelayedInvalidationCall, 4)}
	f.s.authCache = invalidator
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, err := f.s.UpdateMemberConcurrency(ctx, EnterpriseActor{UserID: 99, IsAdmin: true}, 1, concurrencyInt(17))
	require.NoError(t, err)
	require.Equal(t, 2, result.UpdatedCount)
	require.Equal(t, concurrencyInt(17), f.e.e.MemberConcurrency)
	for _, id := range f.e.ids {
		require.Equal(t, id, (<-invalidator.calls).id)
		require.Equal(t, 17, f.s.users.(*enterpriseManagementTestUsers).users[id].Concurrency)
	}
	require.Len(t, f.a.logs, 1)
	log := f.a.logs[0]
	require.Equal(t, EnterpriseAuditActionMembersConcurrencyUpdate, log.Action)
	require.Equal(t, int64(99), log.ActorUserID)
	require.Equal(t, RoleAdmin, log.ActorRole)
	require.Equal(t, map[string]any{"old_value": concurrencyInt(5), "new_value": concurrencyInt(17), "updated_count": 2}, log.Detail)
	cancel()
	for _, id := range f.e.ids {
		select {
		case call := <-invalidator.calls:
			require.Equal(t, id, call.id)
			require.NoError(t, call.err)
		case <-time.After(5 * time.Second):
			t.Fatal("missing delayed invalidation")
		}
	}
}

func TestEnterpriseClearMemberConcurrencyPreservesUsers(t *testing.T) {
	f := newEnterpriseManagementTestFixture(t)
	f.e.e.MemberConcurrency = concurrencyInt(17)
	users := f.s.users.(*enterpriseManagementTestUsers)
	u := users.users[20]
	u.Concurrency = 17
	users.users[20] = u
	result, err := f.s.UpdateMemberConcurrency(context.Background(), EnterpriseActor{IsAdmin: true}, 1, nil)
	require.NoError(t, err)
	require.Nil(t, result.MemberConcurrency)
	require.Zero(t, result.UpdatedCount)
	require.Nil(t, f.e.e.MemberConcurrency)
	require.Equal(t, 17, users.users[20].Concurrency)
	require.Empty(t, f.auth.ids)
	require.Nil(t, f.a.logs[0].Detail["new_value"].(*int))
}

func TestEnterpriseMemberConcurrencyValidationAndAdminOnly(t *testing.T) {
	for _, v := range []int{0, -1, math.MaxInt32 + 1} {
		f := newEnterpriseManagementTestFixture(t)
		_, err := f.s.UpdateMemberConcurrency(context.Background(), EnterpriseActor{IsAdmin: true}, 1, &v)
		require.ErrorIs(t, err, ErrEnterpriseInvalidInput)
		require.Zero(t, f.m.countCalls)
	}
	for _, role := range []string{EnterpriseRoleOwner, EnterpriseRoleManager, EnterpriseRoleMember} {
		f := newEnterpriseManagementTestFixture(t)
		f.e.caller = &EnterpriseMemberContext{EnterpriseID: 1, Role: role}
		_, err := f.s.UpdateMemberConcurrency(context.Background(), EnterpriseActor{UserID: 10}, 1, concurrencyInt(5))
		require.ErrorIs(t, err, ErrEnterprisePermissionDenied)
		require.Zero(t, f.m.countCalls)
	}
	require.True(t, EnterpriseMemberConcurrencyValid(concurrencyInt(math.MaxInt32)))
}

func TestEnterpriseMemberConcurrencyFailureDoesNotInvalidate(t *testing.T) {
	for _, failure := range []string{"audit", "commit"} {
		f := newEnterpriseManagementTestFixture(t)
		if failure == "audit" {
			f.a.err = errors.New("audit failed")
		} else {
			transaction := f.s.transact
			f.s.transact = func(ctx context.Context, fn func(context.Context) error) error {
				if err := transaction(ctx, fn); err != nil {
					return err
				}
				return errors.New("commit failed")
			}
		}
		_, err := f.s.UpdateMemberConcurrency(context.Background(), EnterpriseActor{IsAdmin: true}, 1, concurrencyInt(8))
		require.Error(t, err)
		require.Empty(t, f.auth.ids)
	}
}
