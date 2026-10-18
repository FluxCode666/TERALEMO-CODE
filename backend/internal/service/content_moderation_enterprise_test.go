package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type moderationEnterpriseMembersStub struct {
	EnterpriseMemberRepository
	member   *EnterpriseMember
	err      error
	onLookup func()
}

func (r *moderationEnterpriseMembersStub) GetByUserID(context.Context, int64) (*EnterpriseMember, error) {
	if r.onLookup != nil {
		r.onLookup()
	}
	return r.member, r.err
}

type moderationEnterpriseAuditStub struct {
	EnterpriseAuditLogRepository
	logs []*EnterpriseAuditLog
	err  error
}

func (r *moderationEnterpriseAuditStub) Create(_ context.Context, log *EnterpriseAuditLog) error {
	r.logs = append(r.logs, log)
	return r.err
}

type moderationEnterpriseInvalidator struct {
	APIKeyAuthCacheInvalidator
	calls chan moderationEnterpriseInvalidation
}

type moderationEnterpriseInvalidation struct {
	userID int64
	err    error
}

func (i *moderationEnterpriseInvalidator) InvalidateAuthCacheByUserID(ctx context.Context, userID int64) {
	i.calls <- moderationEnterpriseInvalidation{userID: userID, err: ctx.Err()}
}

func TestContentModerationAutoBanEnterpriseOwnerAuditsAndInvalidates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const userID int64 = 1001
	userRepo := &contentModerationTestUserRepo{user: &User{ID: userID, Role: RoleUser, Status: StatusActive}}
	members := &moderationEnterpriseMembersStub{member: &EnterpriseMember{
		ID: 23, EnterpriseID: 7, UserID: userID, Role: EnterpriseRoleOwner, Status: EnterpriseMemberStatusActive,
	}}
	members.onLookup = func() {
		require.Equal(t, StatusDisabled, userRepo.user.Status, "auditing must follow the successful account update")
	}
	audit := &moderationEnterpriseAuditStub{}
	invalidator := &moderationEnterpriseInvalidator{calls: make(chan moderationEnterpriseInvalidation, 2)}
	svc := ProvideContentModerationService(nil, nil, nil, nil, userRepo, nil, invalidator, nil, members, audit)
	cfg := &ContentModerationConfig{AutoBanEnabled: true, BanThreshold: 1, ViolationWindowHours: 24}
	log := newContentModerationFlaggedLog(userID)
	const rawContent = "private original moderated input"
	log.InputExcerpt, log.Error, log.HighestCategory = rawContent, rawContent, rawContent

	require.True(t, svc.applyFlaggedAccountSideEffects(ctx, cfg, log))
	require.Equal(t, StatusDisabled, userRepo.user.Status, "even the only available owner must be banned")
	require.True(t, log.AutoBanned)
	require.Len(t, audit.logs, 1)
	entry := audit.logs[0]
	require.Equal(t, int64(7), entry.EnterpriseID)
	require.Zero(t, entry.ActorUserID)
	require.Equal(t, EnterpriseAuditActorRoleSystem, entry.ActorRole)
	require.Equal(t, EnterpriseAuditActionMemberDisabledByModeration, entry.Action)
	require.Equal(t, EnterpriseScopeMember, entry.TargetType)
	require.Equal(t, int64(23), *entry.TargetID)
	require.Equal(t, map[string]any{
		"user_id": userID, "member_id": int64(23), "reason": "内容审核违规次数达到自动封禁阈值",
		"violation_count": 1, "ban_threshold": 1, "violation_window_hours": 24,
	}, entry.Detail)
	encoded, err := json.Marshal(entry.Detail)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), rawContent)
	first := <-invalidator.calls
	require.Equal(t, userID, first.userID)
	require.NoError(t, first.err)
	cancel()
	select {
	case second := <-invalidator.calls:
		require.Equal(t, userID, second.userID)
		require.NoError(t, second.err, "delayed invalidation must survive request cancellation")
	case <-time.After(4 * time.Second):
		t.Fatal("missing delayed auth cache invalidation")
	}

	require.False(t, svc.applyFlaggedAccountSideEffects(context.Background(), cfg, log))
	require.Len(t, userRepo.updated, 1)
	require.Len(t, audit.logs, 1, "already disabled accounts must not produce another disable audit")
}

func TestContentModerationAutoBanEnterpriseAuditFailuresDoNotUndoBan(t *testing.T) {
	for _, tc := range []struct {
		name       string
		lookupErr  error
		auditErr   error
		missingLog bool
		wantLog    string
	}{
		{name: "audit write fails", auditErr: errors.New("audit write unavailable"), wantLog: "content_moderation.enterprise_audit_failed"},
		{name: "membership lookup fails", lookupErr: errors.New("membership read unavailable"), wantLog: "content_moderation.enterprise_member_lookup_failed"},
		{name: "audit writer missing", missingLog: true, wantLog: "content_moderation.enterprise_audit_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			previousLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
			t.Cleanup(func() { slog.SetDefault(previousLogger) })
			userRepo := &contentModerationTestUserRepo{user: &User{ID: 1001, Role: RoleUser, Status: StatusActive}}
			members := &moderationEnterpriseMembersStub{member: &EnterpriseMember{ID: 23, EnterpriseID: 7, UserID: 1001}, err: tc.lookupErr}
			audit := &moderationEnterpriseAuditStub{err: tc.auditErr}
			invalidator := &moderationEnterpriseInvalidator{calls: make(chan moderationEnterpriseInvalidation, 2)}
			svc := ProvideContentModerationService(nil, nil, nil, nil, userRepo, nil, invalidator, nil, members, audit)
			if tc.missingLog {
				svc.enterpriseAudit = nil
			}
			log := newContentModerationFlaggedLog(1001)
			require.True(t, svc.applyFlaggedAccountSideEffects(context.Background(), &ContentModerationConfig{AutoBanEnabled: true, BanThreshold: 1}, log))
			require.True(t, log.AutoBanned)
			require.Equal(t, StatusDisabled, userRepo.user.Status)
			require.Equal(t, int64(1001), (<-invalidator.calls).userID)
			require.Contains(t, output.String(), "level=ERROR")
			require.Contains(t, output.String(), tc.wantLog)
		})
	}
}

func TestContentModerationAutoBanNonMemberPreservesBehavior(t *testing.T) {
	userRepo := &contentModerationTestUserRepo{user: &User{ID: 1001, Role: RoleUser, Status: StatusActive}}
	members := &moderationEnterpriseMembersStub{err: ErrEnterpriseMemberNotFound}
	audit := &moderationEnterpriseAuditStub{}
	invalidator := &moderationEnterpriseInvalidator{calls: make(chan moderationEnterpriseInvalidation, 2)}
	svc := ProvideContentModerationService(nil, nil, nil, nil, userRepo, nil, invalidator, nil, members, audit)
	log := newContentModerationFlaggedLog(1001)
	require.True(t, svc.applyFlaggedAccountSideEffects(context.Background(), &ContentModerationConfig{AutoBanEnabled: true, BanThreshold: 1}, log))
	require.True(t, log.AutoBanned)
	require.Equal(t, StatusDisabled, userRepo.user.Status)
	require.Empty(t, audit.logs)
	require.Equal(t, int64(1001), (<-invalidator.calls).userID)
}

type moderationEnterpriseFailingUserRepo struct{ *contentModerationTestUserRepo }

func (*moderationEnterpriseFailingUserRepo) Update(context.Context, *User, UserUpdateFields) error {
	return errors.New("account update failed")
}

func TestContentModerationFailedBanDoesNotAuditOrInvalidate(t *testing.T) {
	userRepo := &moderationEnterpriseFailingUserRepo{&contentModerationTestUserRepo{user: &User{ID: 1001, Role: RoleUser, Status: StatusActive}}}
	members := &moderationEnterpriseMembersStub{onLookup: func() { t.Fatal("failed ban must not query membership") }}
	audit := &moderationEnterpriseAuditStub{}
	invalidator := &moderationEnterpriseInvalidator{calls: make(chan moderationEnterpriseInvalidation, 2)}
	svc := ProvideContentModerationService(nil, nil, nil, nil, userRepo, nil, invalidator, nil, members, audit)
	log := newContentModerationFlaggedLog(1001)
	require.False(t, svc.applyFlaggedAccountSideEffects(context.Background(), &ContentModerationConfig{AutoBanEnabled: true, BanThreshold: 1}, log))
	require.False(t, log.AutoBanned)
	require.Equal(t, StatusActive, userRepo.user.Status)
	require.Empty(t, audit.logs)
	require.Empty(t, invalidator.calls)
}
