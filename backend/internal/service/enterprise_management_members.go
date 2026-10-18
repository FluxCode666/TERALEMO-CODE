package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"net/mail"
	"slices"
	"strings"
)

func (s *EnterpriseService) ListDepartments(ctx context.Context, a EnterpriseActor, id int64) ([]EnterpriseDepartment, error) {
	_, c, err := s.EnterpriseAuthorize(ctx, a, id, false)
	if err != nil {
		return nil, err
	}
	items, err := s.departments.List(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.IsAdmin || c.Role == EnterpriseRoleOwner {
		return items, nil
	}
	out := []EnterpriseDepartment{}
	for _, d := range items {
		if c.DepartmentID != nil && (d.ID == *c.DepartmentID || slices.Contains(d.PathIDs, *c.DepartmentID)) {
			out = append(out, d)
		}
	}
	return out, nil
}
func (s *EnterpriseService) CreateDepartment(ctx context.Context, a EnterpriseActor, id int64, in EnterpriseDepartmentInput) (*EnterpriseDepartment, error) {
	if strings.TrimSpace(in.Name) == "" || !enterpriseManagementTextValid(in.Name, 100) {
		return nil, ErrEnterpriseInvalidInput
	}
	if in.ParentID != nil && *in.ParentID < 0 {
		return nil, ErrEnterpriseInvalidInput
	}
	d := &EnterpriseDepartment{ParentID: in.ParentID, EnterpriseID: id, Name: strings.TrimSpace(in.Name), SortOrder: in.SortOrder}
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		if err := enterpriseCheck(a, c, e, EnterprisePermissionManageDepartments, EnterprisePermissionTarget{}); err != nil {
			return err
		}
		if err := s.departments.Create(ctx, d); err != nil {
			return err
		}
		return s.audit(ctx, a, c, id, EnterpriseAuditActionDepartmentCreate, EnterpriseScopeDepartment, d.ID, nil, d)
	})
	if err != nil {
		return nil, err
	}
	s.invalidate(ctx, id)
	return s.departments.GetByID(ctx, id, d.ID)
}
func (s *EnterpriseService) UpdateDepartment(ctx context.Context, a EnterpriseActor, id, departmentID int64, in UpdateEnterpriseDepartmentInput) (*EnterpriseDepartment, error) {
	if in.ParentID != nil && *in.ParentID < 0 {
		return nil, ErrEnterpriseInvalidInput
	}
	if in.Name != nil && (strings.TrimSpace(*in.Name) == "" || !enterpriseManagementTextValid(*in.Name, 100)) {
		return nil, ErrEnterpriseInvalidInput
	}
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		if err := enterpriseCheck(a, c, e, EnterprisePermissionManageDepartments, EnterprisePermissionTarget{}); err != nil {
			return err
		}
		d, err := s.departments.GetByID(ctx, id, departmentID)
		if err != nil {
			return err
		}
		before := *d
		if in.ParentID != nil {
			d.ParentID = in.ParentID
			if *in.ParentID == 0 {
				d.ParentID = nil
			}
		}
		if in.Name != nil {
			d.Name = strings.TrimSpace(*in.Name)
		}
		if in.SortOrder != nil {
			d.SortOrder = *in.SortOrder
		}
		if err = s.departments.Update(ctx, d); err != nil {
			return err
		}
		if in.ParentID != nil {
			if err := s.validateDepartmentSubtreeQuotaParents(ctx, id, departmentID); err != nil {
				return err
			}
		}
		return s.audit(ctx, a, c, id, EnterpriseAuditActionDepartmentUpdate, EnterpriseScopeDepartment, departmentID, &before, d)
	})
	if err != nil {
		return nil, err
	}
	s.invalidate(ctx, id)
	return s.departments.GetByID(ctx, id, departmentID)
}
func (s *EnterpriseService) DeleteDepartment(ctx context.Context, a EnterpriseActor, id, departmentID int64) error {
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		if err := enterpriseCheck(a, c, e, EnterprisePermissionManageDepartments, EnterprisePermissionTarget{}); err != nil {
			return err
		}
		d, err := s.departments.GetByID(ctx, id, departmentID)
		if err != nil {
			return err
		}
		if d.MemberCount > 0 {
			return ErrEnterpriseDepartmentNotEmpty
		}
		if err = s.departments.Delete(ctx, id, departmentID); err != nil {
			return err
		}
		return s.audit(ctx, a, c, id, EnterpriseAuditActionDepartmentDelete, EnterpriseScopeDepartment, departmentID, d, nil)
	})
	if err == nil {
		s.invalidate(ctx, id)
		s.invalidateQuota(ctx, EnterpriseScopeDepartment, departmentID)
	}
	return err
}
func (s *EnterpriseService) ListMembers(ctx context.Context, a EnterpriseActor, id int64, p pagination.PaginationParams, f EnterpriseMemberListFilters) ([]EnterpriseMember, *pagination.PaginationResult, error) {
	e, c, err := s.EnterpriseAuthorize(ctx, a, id, false)
	if err != nil {
		return nil, nil, err
	}
	if err = enterpriseCheck(a, c, e, EnterprisePermissionViewMembers, EnterprisePermissionTarget{}); err != nil {
		return nil, nil, err
	}
	f.DepartmentScopeID = 0
	if !a.IsAdmin && c.Role == EnterpriseRoleManager {
		f.DepartmentScopeID = *c.DepartmentID
		if f.DepartmentID != nil && !enterpriseDepartmentInSubtree(c, *f.DepartmentID) {
			return nil, nil, ErrEnterprisePermissionDenied
		}
		if f.DepartmentID == nil {
			f.DepartmentID = c.DepartmentID
		}
	}
	return s.members.List(ctx, id, p, f)
}
func (s *EnterpriseService) createMember(ctx context.Context, a EnterpriseActor, c *EnterpriseMemberContext, e *Enterprise, in CreateEnterpriseMemberInput) (*EnterpriseMember, bool, error) {
	if !enterpriseManagementTextValid(in.Title, 100) || !enterpriseManagementTextValid(in.Email, 255) || !enterpriseManagementTextValid(in.Username, 100) {
		return nil, false, ErrEnterpriseInvalidInput
	}
	if in.Role == "" {
		in.Role = EnterpriseRoleMember
	}
	if !enterpriseValidRole(in.Role) {
		return nil, false, ErrEnterpriseInvalidInput
	}
	if in.DepartmentID != nil && *in.DepartmentID == 0 {
		in.DepartmentID = nil
	}
	m := &EnterpriseMember{EnterpriseID: e.ID, DepartmentID: in.DepartmentID, Role: in.Role, Status: EnterpriseMemberStatusActive, Title: in.Title}
	if m.DepartmentID != nil {
		d, err := s.departments.GetByID(ctx, e.ID, *m.DepartmentID)
		if err != nil {
			return nil, false, err
		}
		m.DepartmentPathIDs = d.PathIDs
	}
	if err := enterpriseCheck(a, c, e, EnterprisePermissionCreateMember, EnterprisePermissionTarget{Member: m}); err != nil {
		return nil, false, err
	}
	if in.Role == EnterpriseRoleManager && in.DepartmentID == nil {
		return nil, false, ErrEnterpriseManagerRequiresDepartment
	}
	if in.DepartmentID != nil {
		if _, err := s.departments.GetByID(ctx, e.ID, *in.DepartmentID); err != nil {
			return nil, false, err
		}
	}
	var u *User
	var err error
	created := in.UserID == 0
	if in.UserID < 0 {
		return nil, false, ErrEnterpriseInvalidInput
	}
	if in.UserID > 0 {
		if !a.IsAdmin {
			return nil, false, ErrEnterprisePermissionDenied
		}
		if in.Email != "" || in.Password != "" || in.Username != "" {
			return nil, false, ErrEnterpriseInvalidInput
		}
		u, err = s.users.GetByID(ctx, in.UserID)
	} else {
		address, emailErr := mail.ParseAddress(strings.TrimSpace(in.Email))
		if emailErr != nil || address.Address != strings.TrimSpace(in.Email) || !enterprisePasswordValid(in.Password) {
			return nil, false, ErrEnterpriseInvalidInput
		}
		u, err = s.createUser(ctx, a, in)
	}
	if err != nil {
		return nil, false, err
	}
	if u.Role == RoleAdmin {
		return nil, false, ErrEnterpriseMemberAdminForbidden
	}
	existing, err := s.members.GetByUserID(ctx, u.ID)
	if err != nil && !enterpriseMissingMember(err) {
		return nil, false, err
	}
	if existing != nil {
		return nil, false, ErrEnterpriseMemberExists
	}
	m.UserID = u.ID
	m.AccountCreatedByEnterprise = created
	if err = s.members.Create(ctx, m); err != nil {
		return nil, false, err
	}
	action := EnterpriseAuditActionMemberAdd
	if created {
		action = EnterpriseAuditActionMemberCreate
	}
	if err = s.audit(ctx, a, c, e.ID, action, EnterpriseScopeMember, m.ID, nil, m); err != nil {
		return nil, false, err
	}
	return m, created, nil
}
func (s *EnterpriseService) CreateMember(ctx context.Context, a EnterpriseActor, id int64, in CreateEnterpriseMemberInput) (*EnterpriseMember, error) {
	var m *EnterpriseMember
	var created bool
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		var err error
		m, created, err = s.createMember(ctx, a, c, e, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	if created {
		s.afterUserCreated(ctx, m.UserID)
	}
	s.invalidate(ctx, id, m.UserID)
	return s.members.GetByID(ctx, id, m.ID)
}
func (s *EnterpriseService) UpdateMember(ctx context.Context, a EnterpriseActor, id, memberID int64, in UpdateEnterpriseMemberInput) (*EnterpriseMember, error) {
	var uid int64
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		m, err := s.updateMember(ctx, a, e, c, id, memberID, in)
		if m != nil {
			uid = m.UserID
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	s.invalidate(ctx, id, uid)
	return s.members.GetByID(ctx, id, memberID)
}
func (s *EnterpriseService) updateMember(ctx context.Context, a EnterpriseActor, e *Enterprise, c *EnterpriseMemberContext, id, memberID int64, in UpdateEnterpriseMemberInput) (*EnterpriseMember, error) {
	m, err := s.members.GetByID(ctx, id, memberID)
	if err != nil {
		return nil, err
	}
	before := *m
	if err = enterpriseCheck(a, c, e, EnterprisePermissionManageMember, EnterprisePermissionTarget{Member: m}); err != nil {
		return nil, err
	}
	// Managers may edit only the state/title of a department member. Even a
	// redundant role/department field is rejected, so privilege changes cannot hide in patches.
	if !a.IsAdmin && c.Role == EnterpriseRoleManager && (in.Role != nil || in.DepartmentID != nil) {
		return nil, ErrEnterprisePermissionDenied
	}
	if in.Role != nil {
		if !enterpriseValidRole(*in.Role) {
			return nil, ErrEnterpriseInvalidInput
		}
		m.Role = *in.Role
	}
	if in.Status != nil {
		if !enterpriseValidStatus(*in.Status) {
			return nil, ErrEnterpriseInvalidInput
		}
		m.Status = *in.Status
	}
	if in.Title != nil {
		if !enterpriseManagementTextValid(*in.Title, 100) {
			return nil, ErrEnterpriseInvalidInput
		}
		m.Title = *in.Title
	}
	if in.DepartmentID != nil {
		if *in.DepartmentID < 0 {
			return nil, ErrEnterpriseInvalidInput
		}
		m.DepartmentID = in.DepartmentID
		if *in.DepartmentID == 0 {
			m.DepartmentID = nil
		}
	}
	if m.Role == EnterpriseRoleManager && m.DepartmentID == nil {
		return nil, ErrEnterpriseManagerRequiresDepartment
	}
	if m.DepartmentID != nil {
		if _, err = s.departments.GetByID(ctx, id, *m.DepartmentID); err != nil {
			return nil, err
		}
	}
	if enterpriseMemberIsAvailableOwner(&before) && (m.Role != EnterpriseRoleOwner || m.Status != EnterpriseMemberStatusActive) {
		count, err := s.members.CountActiveOwners(ctx, id)
		if err != nil {
			return nil, err
		}
		if count <= 1 {
			return nil, ErrEnterpriseLastOwner
		}
	}
	if err = s.members.Update(ctx, m); err != nil {
		return nil, err
	}
	if in.DepartmentID != nil {
		target, err := s.scopeTarget(ctx, id, EnterpriseScopeMember, m.ID)
		if err != nil {
			return nil, err
		}
		q, err := s.quotas.GetByScope(ctx, id, EnterpriseScopeMember, m.ID)
		if err != nil {
			return nil, err
		}
		if q == nil {
			return nil, ErrEnterpriseInvalidScope
		}
		if err := s.validateQuotaParents(ctx, id, target, q); err != nil {
			return nil, err
		}
	}
	if err := s.audit(ctx, a, c, id, EnterpriseAuditActionMemberUpdate, EnterpriseScopeMember, memberID, &before, m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *EnterpriseService) RemoveMember(ctx context.Context, a EnterpriseActor, id, memberID int64) error {
	var uid int64
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		m, err := s.members.GetByID(ctx, id, memberID)
		if err != nil {
			return err
		}
		uid = m.UserID
		if err = enterpriseCheck(a, c, e, EnterprisePermissionManageMember, EnterprisePermissionTarget{Member: m}); err != nil {
			return err
		}
		if enterpriseMemberIsAvailableOwner(m) {
			count, err := s.members.CountActiveOwners(ctx, id)
			if err != nil {
				return err
			}
			if count <= 1 {
				return ErrEnterpriseLastOwner
			}
		}
		if err = s.members.Delete(ctx, id, memberID); err != nil {
			return err
		}
		return s.audit(ctx, a, c, id, EnterpriseAuditActionMemberRemove, EnterpriseScopeMember, memberID, m, nil)
	})
	if err == nil {
		s.invalidate(ctx, id, uid)
		s.invalidateQuota(ctx, EnterpriseScopeMember, memberID)
	}
	return err
}

// The member lookup runs under the enterprise lock and joins only users that
// have not been deleted. A deleted account has an empty UserStatus, matching
// CountActiveOwners.
func enterpriseMemberIsAvailableOwner(m *EnterpriseMember) bool {
	return m.Role == EnterpriseRoleOwner && m.Status == EnterpriseMemberStatusActive && m.UserStatus == StatusActive
}
