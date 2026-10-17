//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func enterpriseOwnerAvailabilityService(client *dbent.Client) *service.EnterpriseService {
	return service.NewEnterpriseService(client, NewEnterpriseRepository(client), NewEnterpriseDepartmentRepository(client), NewEnterpriseMemberRepository(client), NewEnterpriseQuotaRepository(client), NewEnterpriseBalanceLogRepository(client), NewEnterpriseAuditLogRepository(client), NewEnterpriseGroupRepository(client), NewUserRepository(client, integrationDB), NewGroupRepository(client, integrationDB), nil, nil, service.NewNoopEnterpriseBillingCache())
}

func enterpriseOwnerAvailabilityMutation(ctx context.Context, svc *service.EnterpriseService, actor service.EnterpriseActor, enterpriseID, memberID int64, operation string) error {
	switch operation {
	case "demote":
		role := service.EnterpriseRoleMember
		_, err := svc.UpdateMember(ctx, actor, enterpriseID, memberID, service.UpdateEnterpriseMemberInput{Role: &role})
		return err
	case "disable":
		status := service.EnterpriseMemberStatusDisabled
		_, err := svc.UpdateMember(ctx, actor, enterpriseID, memberID, service.UpdateEnterpriseMemberInput{Status: &status})
		return err
	default:
		return svc.RemoveMember(ctx, actor, enterpriseID, memberID)
	}
}

func TestEnterpriseMemberUnavailableOwnerCanBeChanged(t *testing.T) {
	for _, route := range []string{"admin", "enterprise"} {
		for _, operation := range []string{"demote", "disable", "remove"} {
			for _, account := range []string{"disabled", "deleted"} {
				t.Run(route+"/"+operation+"/"+account, func(t *testing.T) {
					tx := testEntTx(t)
					ctx := dbent.NewTxContext(context.Background(), tx)
					client := tx.Client()
					e := enterpriseFixture(t, ctx)
					available := enterpriseMemberFixture(t, ctx, e.ID, nil)
					unavailable := enterpriseMemberFixture(t, ctx, e.ID, nil)
					members := NewEnterpriseMemberRepository(client)
					for _, m := range []*service.EnterpriseMember{available, unavailable} {
						m.Role = service.EnterpriseRoleOwner
						require.NoError(t, members.Update(ctx, m))
					}
					update := client.User.UpdateOneID(unavailable.UserID)
					if account == "disabled" {
						update.SetStatus(service.StatusDisabled)
					} else {
						update.SetDeletedAt(time.Now())
					}
					_, err := update.Save(ctx)
					require.NoError(t, err)
					count, err := members.CountActiveOwners(ctx, e.ID)
					require.NoError(t, err)
					require.EqualValues(t, 1, count)
					target, err := members.GetByID(ctx, e.ID, unavailable.ID)
					require.NoError(t, err)
					require.NotEqual(t, service.StatusActive, target.UserStatus)

					actor := service.EnterpriseActor{UserID: available.UserID, IsAdmin: route == "admin"}
					require.NoError(t, enterpriseOwnerAvailabilityMutation(ctx, enterpriseOwnerAvailabilityService(client), actor, e.ID, unavailable.ID, operation))
					count, err = members.CountActiveOwners(ctx, e.ID)
					require.NoError(t, err)
					require.EqualValues(t, 1, count, "changing an unavailable owner must retain the existing available owner")
					target, err = members.GetByID(ctx, e.ID, unavailable.ID)
					if operation == "remove" {
						require.ErrorIs(t, err, service.ErrEnterpriseMemberNotFound)
					} else {
						require.NoError(t, err)
						if operation == "demote" {
							require.Equal(t, service.EnterpriseRoleMember, target.Role)
						} else {
							require.Equal(t, service.EnterpriseMemberStatusDisabled, target.Status)
						}
					}
				})
			}
		}
	}
}

func TestEnterpriseMemberOnlyAvailableOwnerRemainsProtected(t *testing.T) {
	for _, route := range []string{"admin", "enterprise"} {
		for _, operation := range []string{"demote", "disable", "remove"} {
			t.Run(route+"/"+operation, func(t *testing.T) {
				tx := testEntTx(t)
				ctx := dbent.NewTxContext(context.Background(), tx)
				client := tx.Client()
				e := enterpriseFixture(t, ctx)
				available := enterpriseMemberFixture(t, ctx, e.ID, nil)
				unavailable := enterpriseMemberFixture(t, ctx, e.ID, nil)
				members := NewEnterpriseMemberRepository(client)
				for _, m := range []*service.EnterpriseMember{available, unavailable} {
					m.Role = service.EnterpriseRoleOwner
					require.NoError(t, members.Update(ctx, m))
				}
				_, err := client.User.UpdateOneID(unavailable.UserID).SetStatus(service.StatusDisabled).Save(ctx)
				require.NoError(t, err)
				actor := service.EnterpriseActor{UserID: available.UserID, IsAdmin: route == "admin"}
				require.ErrorIs(t, enterpriseOwnerAvailabilityMutation(ctx, enterpriseOwnerAvailabilityService(client), actor, e.ID, available.ID, operation), service.ErrEnterpriseLastOwner)
				count, err := members.CountActiveOwners(ctx, e.ID)
				require.NoError(t, err)
				require.EqualValues(t, 1, count)
				unchanged, err := members.GetByID(ctx, e.ID, available.ID)
				require.NoError(t, err)
				require.Equal(t, service.EnterpriseRoleOwner, unchanged.Role)
				require.Equal(t, service.EnterpriseMemberStatusActive, unchanged.Status)
			})
		}
	}
}

func TestEnterpriseMemberConcurrentOwnerMutationsRetainAvailableOwner(t *testing.T) {
	for _, route := range []string{"admin", "enterprise"} {
		for _, operation := range []string{"demote", "disable", "remove"} {
			t.Run(route+"/"+operation, func(t *testing.T) {
				ctx := context.Background()
				client := testEntClient(t)
				e, err := client.Enterprise.Create().SetName(fmt.Sprintf("owner-availability-%d", time.Now().UnixNano())).Save(ctx)
				require.NoError(t, err)
				var userIDs []int64
				// Concurrent service transactions need committed fixtures. Remove every
				// row, including audits and soft-deleted members, after this subtest.
				t.Cleanup(func() {
					for _, table := range []string{"enterprise_audit_logs", "enterprise_members"} {
						_, cleanupErr := integrationDB.ExecContext(context.Background(), "DELETE FROM "+table+" WHERE enterprise_id = $1", e.ID)
						require.NoError(t, cleanupErr)
					}
					_, cleanupErr := integrationDB.ExecContext(context.Background(), "DELETE FROM enterprises WHERE id = $1", e.ID)
					require.NoError(t, cleanupErr)
					for _, userID := range userIDs {
						_, cleanupErr = integrationDB.ExecContext(context.Background(), "DELETE FROM users WHERE id = $1", userID)
						require.NoError(t, cleanupErr)
					}
				})
				var targets []service.EnterpriseMember
				for i := range 3 {
					status := service.StatusActive
					if i == 2 {
						status = service.StatusDisabled
					}
					u, err := client.User.Create().SetEmail(fmt.Sprintf("owner-availability-%d-%d@example.com", e.ID, i)).SetPasswordHash("hash").SetRole(service.RoleUser).SetStatus(status).Save(ctx)
					require.NoError(t, err)
					userIDs = append(userIDs, u.ID)
					m, err := client.EnterpriseMember.Create().SetEnterpriseID(e.ID).SetUserID(u.ID).SetRole(service.EnterpriseRoleOwner).Save(ctx)
					require.NoError(t, err)
					if i < 2 {
						targets = append(targets, service.EnterpriseMember{ID: m.ID, UserID: u.ID})
					}
				}
				svc := enterpriseOwnerAvailabilityService(client)
				start := make(chan struct{})
				results := make(chan error, len(targets))
				var ready sync.WaitGroup
				ready.Add(len(targets))
				for _, target := range targets {
					go func(target service.EnterpriseMember) {
						ready.Done()
						<-start
						actor := service.EnterpriseActor{UserID: target.UserID, IsAdmin: route == "admin"}
						results <- enterpriseOwnerAvailabilityMutation(ctx, svc, actor, e.ID, target.ID, operation)
					}(target)
				}
				ready.Wait()
				close(start)
				var successes, refusals int
				for range targets {
					if err := <-results; err == nil {
						successes++
					} else {
						require.ErrorIs(t, err, service.ErrEnterpriseLastOwner)
						refusals++
					}
				}
				require.Equal(t, 1, successes)
				require.Equal(t, 1, refusals)
				count, err := NewEnterpriseMemberRepository(client).CountActiveOwners(ctx, e.ID)
				require.NoError(t, err)
				require.EqualValues(t, 1, count, "the disabled third owner's account must not hide loss of the final available owner")
			})
		}
	}
}
