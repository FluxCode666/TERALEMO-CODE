//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type enterpriseModerationSettings struct{ service.SettingRepository }

func (*enterpriseModerationSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{
		service.SettingKeyRiskControlEnabled:      "true",
		service.SettingKeyContentModerationConfig: `{"all_groups":true,"auto_ban_enabled":true,"ban_threshold":1,"violation_window_hours":24}`,
	}, nil
}

type enterpriseModerationInvalidator struct {
	service.APIKeyAuthCacheInvalidator
	userIDs chan int64
}

func (i *enterpriseModerationInvalidator) InvalidateAuthCacheByUserID(_ context.Context, userID int64) {
	i.userIDs <- userID
}

func TestEnterpriseModerationBanOnlyOwnerAuditsWithoutEnterpriseLock(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	e, err := client.Enterprise.Create().SetName(fmt.Sprintf("moderation-owner-%d", time.Now().UnixNano())).Save(ctx)
	require.NoError(t, err)
	var userIDs []int64
	// Account updates must commit independently of the enterprise audit. These
	// fixtures are visible to separate connections, so clean every committed row.
	t.Cleanup(func() {
		_, cleanupErr := integrationDB.ExecContext(context.Background(), `DELETE FROM enterprise_audit_logs WHERE enterprise_id = $1`, e.ID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = integrationDB.ExecContext(context.Background(), `DELETE FROM enterprise_members WHERE enterprise_id = $1`, e.ID)
		require.NoError(t, cleanupErr)
		_, cleanupErr = integrationDB.ExecContext(context.Background(), `DELETE FROM enterprises WHERE id = $1`, e.ID)
		require.NoError(t, cleanupErr)
		for _, userID := range userIDs {
			_, cleanupErr = integrationDB.ExecContext(context.Background(), `DELETE FROM content_moderation_logs WHERE user_id = $1`, userID)
			require.NoError(t, cleanupErr)
			_, cleanupErr = integrationDB.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
			require.NoError(t, cleanupErr)
		}
	})
	u, err := client.User.Create().SetEmail(fmt.Sprintf("moderation-owner-%d@example.com", e.ID)).SetPasswordHash("hash").SetRole(service.RoleUser).SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)
	userIDs = append(userIDs, u.ID)
	m, err := client.EnterpriseMember.Create().SetEnterpriseID(e.ID).SetUserID(u.ID).SetRole(service.EnterpriseRoleOwner).SetStatus(service.EnterpriseMemberStatusActive).Save(ctx)
	require.NoError(t, err)
	enterprises := NewEnterpriseRepository(client)
	before, err := enterprises.GetByID(ctx, e.ID)
	require.NoError(t, err)
	require.Equal(t, 1, before.ActiveOwnerCount)
	invalidator := &enterpriseModerationInvalidator{userIDs: make(chan int64, 4)}
	audits := NewEnterpriseAuditLogRepository(client)
	moderation := service.ProvideContentModerationService(&enterpriseModerationSettings{}, NewContentModerationRepository(integrationDB), nil, nil, NewUserRepository(client, integrationDB), nil, invalidator, nil, NewEnterpriseMemberRepository(client), audits)

	// A held enterprise row lock must not prevent platform enforcement. This
	// catches accidentally reusing the management owner guard from moderation.
	lock := testEntTx(t)
	owners, err := NewEnterpriseMemberRepository(client).CountActiveOwners(dbent.NewTxContext(ctx, lock), e.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, owners)
	const rawContent = "private moderated original body"
	done := make(chan struct{})
	go func() {
		defer close(done)
		moderation.RecordCyberPolicyEvent(ctx, service.CyberPolicyRecordInput{UserID: u.ID, UpstreamMessage: rawContent, UpstreamBody: rawContent})
	}()
	select {
	case <-done:
		require.NoError(t, lock.Rollback())
	case <-time.After(3 * time.Second):
		_ = lock.Rollback()
		<-done
		t.Fatal("automatic moderation waited for the enterprise row lock")
	}
	stored, err := client.User.Get(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusDisabled, stored.Status)
	entries, _, err := audits.List(ctx, e.ID, pagination.DefaultPagination(), service.EnterpriseAuditActionMemberDisabledByModeration)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	entry := entries[0]
	require.Zero(t, entry.ActorUserID)
	require.Equal(t, service.EnterpriseAuditActorRoleSystem, entry.ActorRole)
	require.Equal(t, m.ID, *entry.TargetID)
	require.EqualValues(t, u.ID, entry.Detail["user_id"])
	require.EqualValues(t, m.ID, entry.Detail["member_id"])
	require.NotEmpty(t, entry.Detail["reason"])
	encoded, err := json.Marshal(entry.Detail)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), rawContent)
	after, err := enterprises.GetByID(ctx, e.ID)
	require.NoError(t, err)
	require.Zero(t, after.ActiveOwnerCount)
	listed, _, err := enterprises.List(ctx, pagination.DefaultPagination(), service.EnterpriseListFilters{Search: e.Name})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Zero(t, listed[0].ActiveOwnerCount)
	require.Equal(t, u.ID, <-invalidator.userIDs)
	select {
	case id := <-invalidator.userIDs:
		require.Equal(t, u.ID, id)
	case <-time.After(4 * time.Second):
		t.Fatal("missing delayed invalidation after automatic moderation")
	}

	// The same enforcement path retains its behavior for personal accounts.
	personal, err := client.User.Create().SetEmail(fmt.Sprintf("moderation-personal-%d@example.com", e.ID)).SetPasswordHash("hash").SetRole(service.RoleUser).SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)
	userIDs = append(userIDs, personal.ID)
	moderation.RecordCyberPolicyEvent(ctx, service.CyberPolicyRecordInput{UserID: personal.ID})
	stored, err = client.User.Get(ctx, personal.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusDisabled, stored.Status)
	entries, _, err = audits.List(ctx, e.ID, pagination.DefaultPagination(), "")
	require.NoError(t, err)
	require.Len(t, entries, 1, "a personal account must not add an enterprise audit")
	require.Equal(t, personal.ID, <-invalidator.userIDs)
}
