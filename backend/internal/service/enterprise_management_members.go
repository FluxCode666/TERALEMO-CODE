package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"net/mail"
	"strings"
)

func (s *EnterpriseService) ListDepartments(ctx context.Context, a EnterpriseActor, id int64) ([]EnterpriseDepartment, error) {
	if _, _, err := s.EnterpriseAuthorize(ctx, a, id, false); err != nil {
		return nil, err
	}
	return s.departments.List(ctx, id)
}
func (s *EnterpriseService) CreateDepartment(ctx context.Context, a EnterpriseActor, id int64, in EnterpriseDepartmentInput) (*EnterpriseDepartment, error) {
	if strings.TrimSpace(in.Name) == "" || !enterpriseManagementTextValid(in.Name, 100) {
		return nil, ErrEnterpriseInvalidInput
	}
	d := &EnterpriseDepartment{EnterpriseID: id, Name: strings.TrimSpace(in.Name), SortOrder: in.SortOrder}
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
		if in.Name != nil {
			d.Name = strings.TrimSpace(*in.Name)
		}
		if in.SortOrder != nil {
			d.SortOrder = *in.SortOrder
		}
		if err = s.departments.Update(ctx, d); err != nil {
			return err
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
	if !a.IsAdmin && c.Role == EnterpriseRoleManager {
		if f.DepartmentID != nil && *f.DepartmentID != *c.DepartmentID {
			return nil, nil, ErrEnterprisePermissionDenied
		}
		f.DepartmentID = c.DepartmentID
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
		if emailErr != nil || address.Address != strings.TrimSpace(in.Email) || len(in.Password) < 6 || len(in.Password) > 72 {
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
		m, err := s.members.GetByID(ctx, id, memberID)
		if err != nil {
			return err
		}
		before := *m
		uid = m.UserID
		if err = enterpriseCheck(a, c, e, EnterprisePermissionManageMember, EnterprisePermissionTarget{Member: m}); err != nil {
			return err
		}
		// Managers may edit only the state/title of a department member. Even a
		// redundant role/department field is rejected, so privilege changes cannot hide in patches.
		if !a.IsAdmin && c.Role == EnterpriseRoleManager && (in.Role != nil || in.DepartmentID != nil) {
			return ErrEnterprisePermissionDenied
		}
		if in.Role != nil {
			if !enterpriseValidRole(*in.Role) {
				return ErrEnterpriseInvalidInput
			}
			m.Role = *in.Role
		}
		if in.Status != nil {
			if !enterpriseValidStatus(*in.Status) {
				return ErrEnterpriseInvalidInput
			}
			m.Status = *in.Status
		}
		if in.Title != nil {
			if !enterpriseManagementTextValid(*in.Title, 100) {
				return ErrEnterpriseInvalidInput
			}
			m.Title = *in.Title
		}
		if in.DepartmentID != nil {
			if *in.DepartmentID < 0 {
				return ErrEnterpriseInvalidInput
			}
			m.DepartmentID = in.DepartmentID
			if *in.DepartmentID == 0 {
				m.DepartmentID = nil
			}
		}
		if m.Role == EnterpriseRoleManager && m.DepartmentID == nil {
			return ErrEnterpriseManagerRequiresDepartment
		}
		if m.DepartmentID != nil {
			if _, err = s.departments.GetByID(ctx, id, *m.DepartmentID); err != nil {
				return err
			}
		}
		if before.Role == EnterpriseRoleOwner && before.Status == EnterpriseMemberStatusActive && (m.Role != EnterpriseRoleOwner || m.Status != EnterpriseMemberStatusActive) {
			count, err := s.members.CountActiveOwners(ctx, id)
			if err != nil {
				return err
			}
			if count <= 1 {
				return ErrEnterpriseLastOwner
			}
		}
		if err = s.members.Update(ctx, m); err != nil {
			return err
		}
		return s.audit(ctx, a, c, id, EnterpriseAuditActionMemberUpdate, EnterpriseScopeMember, memberID, &before, m)
	})
	if err != nil {
		return nil, err
	}
	s.invalidate(ctx, id, uid)
	return s.members.GetByID(ctx, id, memberID)
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
		if m.Role == EnterpriseRoleOwner && m.Status == EnterpriseMemberStatusActive {
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
