//go:build integration

package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
)

func TestEnterpriseManagementConcurrentLastOwnerProtection(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	e := enterpriseFixture(t, ctx)
	members := NewEnterpriseMemberRepository(client)
	a := enterpriseMemberFixture(t, ctx, e.ID, nil)
	b := enterpriseMemberFixture(t, ctx, e.ID, nil)
	a.Role, b.Role = service.EnterpriseRoleOwner, service.EnterpriseRoleOwner
	require.NoError(t, members.Update(ctx, a))
	require.NoError(t, members.Update(ctx, b))
	svc := service.NewEnterpriseService(client, NewEnterpriseRepository(client), NewEnterpriseDepartmentRepository(client), members, NewEnterpriseQuotaRepository(client), NewEnterpriseBalanceLogRepository(client), NewEnterpriseAuditLogRepository(client), NewEnterpriseGroupRepository(client), NewUserRepository(client, integrationDB), NewGroupRepository(client, integrationDB), nil, nil, service.NewNoopEnterpriseBillingCache(), nil)
	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for _, id := range []int64{a.ID, b.ID} {
		go func(id int64) {
			ready.Done()
			<-start
			role := service.EnterpriseRoleMember
			_, err := svc.UpdateMember(ctx, service.EnterpriseActor{UserID: a.UserID, IsAdmin: true}, e.ID, id, service.UpdateEnterpriseMemberInput{Role: &role})
			results <- err
		}(id)
	}
	ready.Wait()
	close(start)
	var successes, refusals int
	for range 2 {
		err := <-results
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, service.ErrEnterpriseLastOwner)
			refusals++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, refusals)
	count, err := members.CountActiveOwners(ctx, e.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}
