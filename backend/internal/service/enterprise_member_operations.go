package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type enterpriseSessionRevoker interface {
	RevokeAllUserSessions(context.Context, int64) error
}

type EnterprisePasswordResetResult struct {
	GeneratedPassword string `json:"generated_password,omitempty"`
}

func enterprisePasswordValid(password string) bool {
	return utf8.ValidString(password) && utf8.RuneCountInString(password) >= 6 && len(password) <= 72
}

func enterpriseGeneratePassword() (string, error) {
	var bytes [24]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func (s *EnterpriseService) ResetMemberPassword(ctx context.Context, a EnterpriseActor, id, memberID int64, password *string) (*EnterprisePasswordResetResult, error) {
	out := &EnterprisePasswordResetResult{}
	var uid int64
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		m, err := s.members.GetByID(ctx, id, memberID)
		if err != nil {
			return err
		}
		if !m.AccountCreatedByEnterprise {
			return ErrEnterprisePasswordResetForbidden
		}
		if !a.IsAdmin {
			if m.Role == EnterpriseRoleOwner {
				return ErrEnterprisePermissionDenied
			}
			if err := enterpriseCheck(a, c, e, EnterprisePermissionManageMember, EnterprisePermissionTarget{Member: m}); err != nil {
				return err
			}
		}
		value := ""
		if password == nil {
			value, err = enterpriseGeneratePassword()
			if err != nil {
				return err
			}
			out.GeneratedPassword = value
		} else {
			value = *password
		}
		if !enterprisePasswordValid(value) {
			return ErrEnterpriseInvalidInput
		}
		writer, ok := s.users.(interface {
			Update(context.Context, *User, UserUpdateFields) error
		})
		if !ok {
			return fmt.Errorf("enterprise password writer unavailable")
		}
		u, err := s.users.GetByID(ctx, m.UserID)
		if err != nil {
			return err
		}
		if u.Role == RoleAdmin {
			return ErrEnterprisePasswordResetForbidden
		}
		if err := u.SetPassword(value); err != nil {
			return err
		}
		// Only password_hash is written. The persisted hash changes the token-version
		// fingerprint checked by both access-token and refresh-token authentication.
		if err := writer.Update(ctx, u, UserUpdateFields{PasswordHash: true}); err != nil {
			return err
		}
		uid = u.ID
		return s.audit(ctx, a, c, id, "member.reset_password", EnterpriseScopeMember, memberID, nil, map[string]any{"user_id": uid})
	})
	if err != nil {
		return nil, err
	}
	s.invalidate(ctx, id, uid)
	if s.sessions != nil {
		if err := s.sessions.RevokeAllUserSessions(ctx, uid); err != nil {
			return nil, fmt.Errorf("revoke enterprise member sessions: %w", err)
		}
	}
	return out, nil
}

type EnterpriseBatchMemberQuotaInput struct {
	MemberIDs []int64  `json:"member_ids"`
	Fields    []string `json:"fields"`
	EnterpriseQuotaLimitsInput
}

type EnterpriseBatchMemberQuotaFailure struct {
	MemberID int64  `json:"member_id"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

type EnterpriseBatchMemberQuotaResult struct {
	Updated []int64                             `json:"updated"`
	Failed  []EnterpriseBatchMemberQuotaFailure `json:"failed"`
}

func (s *EnterpriseService) BatchUpdateMemberQuotas(ctx context.Context, a EnterpriseActor, id int64, in EnterpriseBatchMemberQuotaInput) (*EnterpriseBatchMemberQuotaResult, error) {
	if len(in.MemberIDs) == 0 || len(in.MemberIDs) > 500 || len(in.Fields) == 0 || len(in.Fields) > 4 {
		return nil, ErrEnterpriseInvalidInput
	}
	seenFields := map[string]bool{}
	for _, field := range in.Fields {
		if seenFields[field] {
			return nil, ErrEnterpriseInvalidInput
		}
		seenFields[field] = true
		switch field {
		case "daily", "weekly", "monthly", "total":
		default:
			return nil, ErrEnterpriseInvalidInput
		}
	}
	if _, _, err := s.EnterpriseAuthorize(ctx, a, id, true); err != nil {
		return nil, err
	}
	out := &EnterpriseBatchMemberQuotaResult{Updated: []int64{}, Failed: []EnterpriseBatchMemberQuotaFailure{}}
	seen := map[int64]bool{}
	for _, memberID := range in.MemberIDs {
		if seen[memberID] {
			continue
		}
		seen[memberID] = true
		_, err := s.updateQuotaFields(ctx, a, id, EnterpriseScopeMember, memberID, in.EnterpriseQuotaLimitsInput, in.Fields)
		if err != nil {
			code := infraerrors.Reason(err)
			if code == "" {
				code = "INTERNAL_ERROR"
			}
			out.Failed = append(out.Failed, EnterpriseBatchMemberQuotaFailure{MemberID: memberID, Code: code, Message: infraerrors.Message(err)})
		} else {
			out.Updated = append(out.Updated, memberID)
		}
	}
	return out, nil
}
