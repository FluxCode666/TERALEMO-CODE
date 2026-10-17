package service

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type contentModerationEnterpriseMemberReader interface {
	GetByUserID(context.Context, int64) (*EnterpriseMember, error)
}

type contentModerationEnterpriseAuditWriter interface {
	Create(context.Context, *EnterpriseAuditLog) error
}

// ProvideContentModerationService installs enterprise auditing in production
// without changing the standalone moderation constructor's dependencies.
func ProvideContentModerationService(
	settingRepo SettingRepository,
	repo ContentModerationRepository,
	hashCache ContentModerationHashCache,
	groupRepo GroupRepository,
	userRepo UserRepository,
	proxyRepo ProxyRepository,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	emailService *EmailService,
	members EnterpriseMemberRepository,
	audit EnterpriseAuditLogRepository,
) *ContentModerationService {
	svc := NewContentModerationService(settingRepo, repo, hashCache, groupRepo, userRepo, proxyRepo, authCacheInvalidator, emailService)
	svc.enterpriseMembers = members
	svc.enterpriseAudit = audit
	return svc
}

func (s *ContentModerationService) auditModerationMemberDisabled(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog) {
	if s.enterpriseMembers == nil {
		return
	}
	// The user update has already succeeded. Do not acquire an enterprise row
	// lock or couple best-effort auditing to the account update transaction.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	member, err := s.enterpriseMembers.GetByUserID(ctx, *log.UserID)
	if errors.Is(err, ErrEnterpriseMemberNotFound) {
		return
	}
	if err != nil {
		slog.Error("content_moderation.enterprise_member_lookup_failed", "user_id", *log.UserID, "error", err)
		return
	}
	if member == nil {
		return
	}
	audit := &EnterpriseAuditLog{
		EnterpriseID: member.EnterpriseID,
		ActorUserID:  0, // Automatic platform enforcement has no human actor.
		ActorRole:    EnterpriseAuditActorRoleSystem,
		Action:       EnterpriseAuditActionMemberDisabledByModeration,
		TargetType:   EnterpriseScopeMember,
		TargetID:     &member.ID,
		// Only fixed reason text and numeric rule metadata are allowed here.
		// Moderation excerpts, provider details and errors may contain raw input.
		Detail: map[string]any{
			"user_id":                member.UserID,
			"member_id":              member.ID,
			"reason":                 "内容审核违规次数达到自动封禁阈值",
			"violation_count":        log.ViolationCount,
			"ban_threshold":          cfg.BanThreshold,
			"violation_window_hours": cfg.ViolationWindowHours,
		},
	}
	if s.enterpriseAudit == nil {
		err = errors.New("enterprise audit writer unavailable")
	} else {
		err = s.enterpriseAudit.Create(ctx, audit)
	}
	if err != nil {
		slog.Error("content_moderation.enterprise_audit_failed", "enterprise_id", member.EnterpriseID, "member_id", member.ID, "user_id", member.UserID, "error", err)
	}
}
