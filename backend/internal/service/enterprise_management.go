package service

import (
	"context"
	"errors"
	"fmt"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"math"
	"strings"
	"unicode/utf8"
)

func (s *EnterpriseService) withTransaction(ctx context.Context, fn func(context.Context) error) error {
	if s.transact != nil {
		return s.transact(ctx, fn)
	}
	if dbent.TxFromContext(ctx) != nil {
		return fn(ctx)
	}
	if s.client == nil {
		return fmt.Errorf("enterprise transaction client is unavailable")
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = fn(dbent.NewTxContext(ctx, tx)); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *EnterpriseService) EnterpriseAuthorize(ctx context.Context, a EnterpriseActor, id int64, write bool) (*Enterprise, *EnterpriseMemberContext, error) {
	e, err := s.enterprises.GetByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if a.IsAdmin {
		return e, nil, nil
	}
	c, err := s.enterprises.GetMemberContextByUserID(ctx, a.UserID)
	if err != nil {
		return nil, nil, err
	}
	if c == nil || c.EnterpriseID != id {
		return nil, nil, ErrEnterpriseNotMember
	}
	if c.MemberStatus != EnterpriseMemberStatusActive {
		return nil, nil, ErrEnterpriseMemberDisabled
	}
	if write && e.Status != EnterpriseStatusActive {
		return nil, nil, ErrEnterpriseDisabled
	}
	return e, c, nil
}
func enterpriseCheck(a EnterpriseActor, c *EnterpriseMemberContext, e *Enterprise, p EnterprisePermission, t EnterprisePermissionTarget) error {
	if a.IsAdmin {
		return nil
	}
	return CheckEnterprisePermission(c, e.Settings, p, t)
}
func (s *EnterpriseService) mutate(ctx context.Context, a EnterpriseActor, id int64, fn func(context.Context, *Enterprise, *EnterpriseMemberContext) error) error {
	return s.withTransaction(ctx, func(ctx context.Context) error {
		// All management writes lock the parent first. This also serializes a parent
		// limit change with a child-limit check and owner-count with owner mutations.
		if _, err := s.members.CountActiveOwners(ctx, id); err != nil {
			return err
		}
		e, c, err := s.EnterpriseAuthorize(ctx, a, id, true)
		if err != nil {
			return err
		}
		return fn(ctx, e, c)
	})
}
func (s *EnterpriseService) audit(ctx context.Context, a EnterpriseActor, c *EnterpriseMemberContext, id int64, action, target string, targetID int64, before, after any) error {
	role := RoleAdmin
	if !a.IsAdmin && c != nil {
		role = c.Role
	}
	var pointer *int64
	if targetID > 0 {
		pointer = &targetID
	}
	return s.auditLogs.Create(ctx, &EnterpriseAuditLog{EnterpriseID: id, ActorUserID: a.UserID, ActorRole: role, Action: action, TargetType: target, TargetID: pointer, Detail: map[string]any{"before": before, "after": after}})
}
func (s *EnterpriseService) invalidate(ctx context.Context, id int64, userIDs ...int64) {
	if s.authCache == nil {
		return
	}
	if len(userIDs) == 0 {
		var err error
		userIDs, err = s.enterprises.ListMemberUserIDs(ctx, id)
		if err != nil {
			logger.LegacyPrintf("service.enterprise", "list cache invalidation users: enterprise_id=%d err=%v", id, err)
			return
		}
	}
	invalidateEnterpriseMembersAuthCache(ctx, userIDs, s.authCache)
}
func (s *EnterpriseService) invalidateQuota(ctx context.Context, scope string, id int64) {
	if s.billingCache != nil {
		if err := s.billingCache.InvalidateEnterpriseQuota(ctx, scope, id); err != nil {
			logger.LegacyPrintf("service.enterprise", "invalidate quota cache: scope=%s id=%d err=%v", scope, id, err)
		}
	}
}
func enterpriseManagementTextValid(value string, maxRunes int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maxRunes
}

func enterpriseValidNumber(v *float64) bool {
	return v == nil || (!math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= 0)
}
func enterpriseValidStatus(v string) bool {
	return v == EnterpriseStatusActive || v == EnterpriseStatusDisabled
}
func enterpriseValidRole(v string) bool {
	return v == EnterpriseRoleOwner || v == EnterpriseRoleManager || v == EnterpriseRoleMember
}
func (s *EnterpriseService) ListEnterprises(ctx context.Context, a EnterpriseActor, p pagination.PaginationParams, f EnterpriseListFilters) ([]Enterprise, *pagination.PaginationResult, error) {
	if !a.IsAdmin {
		return nil, nil, ErrEnterprisePermissionDenied
	}
	return s.enterprises.List(ctx, p, f)
}
func (s *EnterpriseService) GetEnterprise(ctx context.Context, a EnterpriseActor, id int64) (*Enterprise, error) {
	if !a.IsAdmin {
		return nil, ErrEnterprisePermissionDenied
	}
	return s.enterprises.GetByID(ctx, id)
}
func (s *EnterpriseService) CreateEnterprise(ctx context.Context, a EnterpriseActor, in CreateEnterpriseInput) (*Enterprise, error) {
	if !a.IsAdmin {
		return nil, ErrEnterprisePermissionDenied
	}
	if strings.TrimSpace(in.Name) == "" || !enterpriseManagementTextValid(in.Name, 100) || !enterpriseManagementTextValid(in.ContactName, 100) || !enterpriseManagementTextValid(in.ContactEmail, 255) || !enterpriseManagementTextValid(in.ContactPhone, 50) || !enterpriseValidNumber(in.LowBalanceThreshold) {
		return nil, ErrEnterpriseInvalidInput
	}
	if in.Status == "" {
		in.Status = EnterpriseStatusActive
	}
	if !enterpriseValidStatus(in.Status) {
		return nil, ErrEnterpriseInvalidInput
	}
	settings := DefaultEnterpriseSettings()
	if in.Settings != nil {
		settings = *in.Settings
	}
	e := &Enterprise{Name: strings.TrimSpace(in.Name), Status: in.Status, Notes: in.Notes, ContactName: in.ContactName, ContactEmail: in.ContactEmail, ContactPhone: in.ContactPhone, LowBalanceThreshold: in.LowBalanceThreshold, Settings: settings}
	var createdUserID int64
	err := s.withTransaction(ctx, func(ctx context.Context) error {
		if err := s.enterprises.Create(ctx, e); err != nil {
			return err
		}
		if in.Owner != nil {
			m, created, err := s.createMember(ctx, a, nil, e, CreateEnterpriseMemberInput{UserID: in.Owner.UserID, Email: in.Owner.Email, Password: in.Owner.Password, Username: in.Owner.Username, Role: EnterpriseRoleOwner})
			if err != nil {
				return err
			}
			if created {
				createdUserID = m.UserID
			}
		}
		return s.audit(ctx, a, nil, e.ID, EnterpriseAuditActionEnterpriseCreate, EnterpriseScopeEnterprise, e.ID, nil, e)
	})
	if err != nil {
		return nil, err
	}
	s.afterUserCreated(ctx, createdUserID)
	s.invalidate(ctx, e.ID)
	return s.enterprises.GetByID(ctx, e.ID)
}
func (s *EnterpriseService) UpdateEnterprise(ctx context.Context, a EnterpriseActor, id int64, in UpdateEnterpriseInput) (*Enterprise, error) {
	if !a.IsAdmin {
		return nil, ErrEnterprisePermissionDenied
	}
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		before := *e
		if in.Name != nil {
			if strings.TrimSpace(*in.Name) == "" || !enterpriseManagementTextValid(*in.Name, 100) {
				return ErrEnterpriseInvalidInput
			}
			e.Name = strings.TrimSpace(*in.Name)
		}
		if in.Status != nil {
			if !enterpriseValidStatus(*in.Status) {
				return ErrEnterpriseInvalidInput
			}
			e.Status = *in.Status
		}
		if (in.ContactName != nil && !enterpriseManagementTextValid(*in.ContactName, 100)) || (in.ContactEmail != nil && !enterpriseManagementTextValid(*in.ContactEmail, 255)) || (in.ContactPhone != nil && !enterpriseManagementTextValid(*in.ContactPhone, 50)) {
			return ErrEnterpriseInvalidInput
		}
		if in.Notes != nil {
			e.Notes = *in.Notes
		}
		if in.ContactName != nil {
			e.ContactName = *in.ContactName
		}
		if in.ContactEmail != nil {
			e.ContactEmail = *in.ContactEmail
		}
		if in.ContactPhone != nil {
			e.ContactPhone = *in.ContactPhone
		}
		if in.LowBalanceThresholdSet {
			if !enterpriseValidNumber(in.LowBalanceThreshold) {
				return ErrEnterpriseInvalidInput
			}
			e.LowBalanceThreshold = in.LowBalanceThreshold
		}
		if in.Settings != nil {
			e.Settings = *in.Settings
		}
		if err := s.enterprises.Update(ctx, e); err != nil {
			return err
		}
		if err := s.audit(ctx, a, c, id, EnterpriseAuditActionEnterpriseUpdate, EnterpriseScopeEnterprise, id, &before, e); err != nil {
			return err
		}
		if in.Settings != nil {
			return s.audit(ctx, a, c, id, EnterpriseAuditActionSettingsUpdate, EnterpriseScopeEnterprise, id, before.Settings, e.Settings)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidate(ctx, id)
	return s.enterprises.GetByID(ctx, id)
}
func (s *EnterpriseService) AdjustBalance(ctx context.Context, a EnterpriseActor, id int64, in EnterpriseBalanceAdjustment) (*Enterprise, error) {
	if !a.IsAdmin {
		return nil, ErrEnterprisePermissionDenied
	}
	if (in.Operation != EnterpriseBalanceOperationAdd && in.Operation != EnterpriseBalanceOperationSubtract) || in.Amount <= 0 || math.IsNaN(in.Amount) || math.IsInf(in.Amount, 0) {
		return nil, ErrEnterpriseInvalidInput
	}
	in.OperatorUserID = &a.UserID
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		log, err := s.enterprises.AdjustBalance(ctx, id, in)
		if err != nil {
			return err
		}
		return s.audit(ctx, a, c, id, EnterpriseAuditActionBalanceAdjust, EnterpriseScopeEnterprise, id, map[string]any{"balance": log.BalanceBefore}, map[string]any{"balance": log.BalanceAfter, "operation": in.Operation, "amount": in.Amount, "notes": in.Notes})
	})
	if err != nil {
		return nil, err
	}
	s.invalidate(ctx, id)
	if s.billingCache != nil {
		if err := s.billingCache.InvalidateEnterpriseBalance(ctx, id); err != nil {
			logger.LegacyPrintf("service.enterprise", "invalidate balance cache: enterprise_id=%d err=%v", id, err)
		}
	}
	return s.enterprises.GetByID(ctx, id)
}
func (s *EnterpriseService) ListBalanceLogs(ctx context.Context, a EnterpriseActor, id int64, p pagination.PaginationParams) ([]EnterpriseBalanceLog, *pagination.PaginationResult, error) {
	if !a.IsAdmin {
		return nil, nil, ErrEnterprisePermissionDenied
	}
	if _, err := s.enterprises.GetByID(ctx, id); err != nil {
		return nil, nil, err
	}
	return s.balanceLogs.List(ctx, id, p)
}
func (s *EnterpriseService) GetSettings(ctx context.Context, a EnterpriseActor, id int64) (EnterpriseSettings, error) {
	e, c, err := s.EnterpriseAuthorize(ctx, a, id, false)
	if err != nil {
		return EnterpriseSettings{}, err
	}
	if !a.IsAdmin && (c == nil || c.Role != EnterpriseRoleOwner) {
		return EnterpriseSettings{}, ErrEnterprisePermissionDenied
	}
	return e.Settings, nil
}
func (s *EnterpriseService) UpdateSettings(ctx context.Context, a EnterpriseActor, id int64, in EnterpriseSettings) (EnterpriseSettings, error) {
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		if err := enterpriseCheck(a, c, e, EnterprisePermissionSettings, EnterprisePermissionTarget{}); err != nil {
			return err
		}
		before := e.Settings
		e.Settings = in
		if err := s.enterprises.Update(ctx, e); err != nil {
			return err
		}
		return s.audit(ctx, a, c, id, EnterpriseAuditActionSettingsUpdate, EnterpriseScopeEnterprise, id, before, in)
	})
	if err == nil {
		s.invalidate(ctx, id)
	}
	return in, err
}
func (s *EnterpriseService) ListAuditLogs(ctx context.Context, a EnterpriseActor, id int64, p pagination.PaginationParams, action string) ([]EnterpriseAuditLog, *pagination.PaginationResult, error) {
	e, c, err := s.EnterpriseAuthorize(ctx, a, id, false)
	if err != nil {
		return nil, nil, err
	}
	if err = enterpriseCheck(a, c, e, EnterprisePermissionAudit, EnterprisePermissionTarget{}); err != nil {
		return nil, nil, err
	}
	return s.auditLogs.List(ctx, id, p, action)
}

// Identity includes disabled memberships so the caller can still display why the
// workbench is unavailable. Nonmembers have a null enterprise identity.
func (s *EnterpriseService) Identity(ctx context.Context, userID int64) (*EnterpriseIdentity, error) {
	c, err := s.enterprises.GetMemberContextByUserID(ctx, userID)
	if err != nil || c == nil {
		return nil, err
	}
	e, err := s.enterprises.GetByID(ctx, c.EnterpriseID)
	if err != nil {
		return nil, err
	}
	m, err := s.members.GetByID(ctx, c.EnterpriseID, c.MemberID)
	if err != nil {
		return nil, err
	}
	identity := &EnterpriseIdentity{ID: e.ID, Name: e.Name, Role: m.Role, DepartmentID: m.DepartmentID, MemberStatus: m.Status, EnterpriseStatus: e.Status, BalanceVisible: EnterpriseBalanceVisible(m.Role, e.Settings)}
	if m.DepartmentID != nil {
		name := EnterpriseMemberDepartmentPathName(m)
		identity.DepartmentName = &name
	}
	if identity.BalanceVisible {
		identity.Balance = &e.Balance
		identity.FrozenBalance = &e.FrozenBalance
	}
	return identity, nil
}
func (s *EnterpriseService) createUser(ctx context.Context, a EnterpriseActor, in CreateEnterpriseMemberInput) (*User, error) {
	if s.admin == nil {
		return nil, fmt.Errorf("enterprise user creator is unavailable")
	}
	creator := s.admin
	if concrete, ok := s.admin.(*adminServiceImpl); ok {
		clone := *concrete
		clone.defaultSubAssigner = nil
		creator = &clone
	}
	// Reuse the administrator's account creation core, including password hashing.
	// Enterprise-created accounts start with a frozen personal wallet at zero;
	// the global signup gift must not be issued. Subscription side effects run only after commit.
	zeroBalance := 0.0
	return creator.CreateUser(ctx, &CreateUserInput{Balance: &zeroBalance, Email: strings.TrimSpace(in.Email), Password: in.Password, Username: in.Username, Role: RoleUser, Concurrency: 1, ActorAdminID: a.UserID})
}
func (s *EnterpriseService) afterUserCreated(ctx context.Context, userID int64) {
	if userID > 0 {
		if concrete, ok := s.admin.(*adminServiceImpl); ok {
			concrete.assignDefaultSubscriptions(ctx, userID)
		}
	}
}
func enterpriseMissingMember(err error) bool {
	return errors.Is(err, ErrEnterpriseMemberNotFound) || errors.Is(err, ErrEnterpriseNotMember)
}
