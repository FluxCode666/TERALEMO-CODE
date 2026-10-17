//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func enterpriseGuardAdminService(client *dbent.Client) service.AdminService {
	s := service.NewAdminService(nil, NewUserRepository(client, integrationDB), nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, client, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	s.(interface {
		SetEnterpriseUserGuard(service.EnterpriseMemberRepository)
	}).SetEnterpriseUserGuard(NewEnterpriseMemberRepository(client))
	return s
}

func TestEnterpriseUserAccountUpdateReusesCallerTransaction(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	u := mustCreateUser(t, tx.Client(), &service.User{Email: fmt.Sprintf("enterprise-caller-tx-%d@example.com", time.Now().UnixNano()), PasswordHash: "hash", Role: service.RoleUser, Status: service.StatusActive})
	// The repository is deliberately constructed with the plain client, just
	// like production wiring, while the outer service owns the transaction.
	repo := NewUserRepository(testEntClient(t), integrationDB)
	inside, err := repo.GetByID(txCtx, u.ID)
	require.NoError(t, err)
	inside.Status = service.StatusDisabled
	require.NoError(t, repo.Update(txCtx, inside, service.UserUpdateFields{Status: true}))
	inside, err = repo.GetByID(txCtx, u.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusDisabled, inside.Status)
	_, err = repo.GetByID(ctx, u.ID)
	require.ErrorIs(t, err, service.ErrUserNotFound, "account changes must not escape the uncommitted outer transaction")
}

func TestEnterpriseExistingAdminUserOperationsPreserveConstraints(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	ctx = dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	e := enterpriseFixture(t, ctx)
	member := enterpriseMemberFixture(t, ctx, e.ID, nil)
	members := NewEnterpriseMemberRepository(client)
	member.Role = service.EnterpriseRoleOwner
	require.NoError(t, members.Update(ctx, member))
	admin := enterpriseGuardAdminService(client)
	_, err := admin.UpdateUser(ctx, member.UserID, &service.UpdateUserInput{Role: service.RoleAdmin})
	require.ErrorIs(t, err, service.ErrEnterpriseMemberAdminForbidden)
	require.ErrorIs(t, admin.DeleteUser(ctx, member.UserID), service.ErrEnterpriseMemberUserDeleteForbidden)
	_, err = admin.UpdateUser(ctx, member.UserID, &service.UpdateUserInput{Status: service.StatusDisabled})
	require.ErrorIs(t, err, service.ErrEnterpriseLastOwner)

	other := enterpriseMemberFixture(t, ctx, e.ID, nil)
	other.Role = service.EnterpriseRoleOwner
	require.NoError(t, members.Update(ctx, other))
	_, err = client.User.UpdateOneID(other.UserID).SetStatus(service.StatusDisabled).Save(ctx)
	require.NoError(t, err)
	count, err := members.CountActiveOwners(ctx, e.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count, "a disabled account is not an available owner")
	_, err = admin.UpdateUser(ctx, member.UserID, &service.UpdateUserInput{Status: service.StatusDisabled})
	require.ErrorIs(t, err, service.ErrEnterpriseLastOwner)
	_, err = client.User.UpdateOneID(other.UserID).SetStatus(service.StatusActive).SetDeletedAt(time.Now()).Save(ctx)
	require.NoError(t, err)
	count, err = members.CountActiveOwners(ctx, e.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count, "a deleted account is not an available owner")
	_, err = client.User.UpdateOneID(other.UserID).ClearDeletedAt().Save(ctx)
	require.NoError(t, err)
	_, err = admin.UpdateUser(ctx, member.UserID, &service.UpdateUserInput{Status: service.StatusDisabled})
	require.NoError(t, err)
	stored, err := client.User.Get(ctx, member.UserID)
	require.NoError(t, err)
	require.Equal(t, service.StatusDisabled, stored.Status)
}

func TestEnterpriseConcurrentAccountOwnerDisableRetainsOneOwner(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	e, err := client.Enterprise.Create().SetName(fmt.Sprintf("enterprise-owner-disable-%d", time.Now().UnixNano())).Save(ctx)
	require.NoError(t, err)
	var userIDs []int64
	// These fixtures commit so both transactions can see them. Clean every
	// committed row to keep the full repository integration suite isolated.
	t.Cleanup(func() {
		_, cleanupErr := integrationDB.ExecContext(context.Background(), `DELETE FROM enterprise_members WHERE enterprise_id = $1`, e.ID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = integrationDB.ExecContext(context.Background(), `DELETE FROM enterprises WHERE id = $1`, e.ID)
		require.NoError(t, cleanupErr)
		for _, userID := range userIDs {
			_, cleanupErr = integrationDB.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
			require.NoError(t, cleanupErr)
		}
	})
	for i := range 2 {
		u, err := client.User.Create().SetEmail(fmt.Sprintf("enterprise-owner-disable-%d-%d@example.com", e.ID, i)).SetPasswordHash("hash").SetRole(service.RoleUser).SetStatus(service.StatusActive).Save(ctx)
		require.NoError(t, err)
		userIDs = append(userIDs, u.ID)
		_, err = client.EnterpriseMember.Create().SetEnterpriseID(e.ID).SetUserID(u.ID).SetRole(service.EnterpriseRoleOwner).Save(ctx)
		require.NoError(t, err)
	}
	admin := enterpriseGuardAdminService(client)
	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for _, userID := range userIDs {
		go func(userID int64) {
			ready.Done()
			<-start
			_, err := admin.UpdateUser(ctx, userID, &service.UpdateUserInput{Status: service.StatusDisabled})
			results <- err
		}(userID)
	}
	ready.Wait()
	close(start)
	var success, blocked int
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else {
			require.True(t, errors.Is(err, service.ErrEnterpriseLastOwner), "unexpected error: %v", err)
			blocked++
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, blocked)
	count, err := NewEnterpriseMemberRepository(client).CountActiveOwners(ctx, e.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}
