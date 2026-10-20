//go:build unit

package service

import (
	"fmt"
	"testing"
)

func enterpriseTestID(v int64) *int64 { return &v }
func TestEnterprisePermissionsMatrix(t *testing.T) {
	roles := []string{EnterpriseRoleOwner, EnterpriseRoleManager, EnterpriseRoleMember}
	targets := []struct {
		name   string
		member EnterpriseMember
	}{
		{"own_department", EnterpriseMember{ID: 20, EnterpriseID: 1, UserID: 20, DepartmentID: enterpriseTestID(3), Role: EnterpriseRoleMember}},
		{"other_department", EnterpriseMember{ID: 21, EnterpriseID: 1, UserID: 21, DepartmentID: enterpriseTestID(4), Role: EnterpriseRoleMember}},
		{"self", EnterpriseMember{ID: 10, EnterpriseID: 1, UserID: 10, DepartmentID: enterpriseTestID(3)}},
		{"owner", EnterpriseMember{ID: 22, EnterpriseID: 1, UserID: 22, DepartmentID: enterpriseTestID(3), Role: EnterpriseRoleOwner}},
	}
	for _, role := range roles {
		for _, enabled := range []bool{false, true} {
			for _, target := range targets {
				for _, enterpriseDisabled := range []bool{false, true} {
					for _, memberDisabled := range []bool{false, true} {
						name := fmt.Sprintf("%s/setting=%t/%s/enterprise_disabled=%t/member_disabled=%t", role, enabled, target.name, enterpriseDisabled, memberDisabled)
						t.Run(name, func(t *testing.T) {
							c := &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 10, DepartmentID: enterpriseTestID(3), Role: role, EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive}
							if enterpriseDisabled {
								c.EnterpriseStatus = EnterpriseStatusDisabled
							}
							if memberDisabled {
								c.MemberStatus = EnterpriseMemberStatusDisabled
							}
							m := target.member
							if target.name == "self" {
								m.Role = role
							}
							settings := EnterpriseSettings{ManagerCanManageMembers: enabled, ManagerCanEditMemberQuota: enabled, ManagerCanEditMemberGroups: enabled, MemberQuotaVisibleToManager: enabled, MemberQuotaVisibleToMember: enabled}
							for _, op := range []EnterprisePermission{EnterprisePermissionManageMember, EnterprisePermissionEditQuota, EnterprisePermissionEditWhitelist, EnterprisePermissionViewQuota} {
								write := op != EnterprisePermissionViewQuota
								allowed := role == EnterpriseRoleOwner
								if role == EnterpriseRoleManager {
									allowed = enabled && m.DepartmentID != nil && *m.DepartmentID == 3 && (op == EnterprisePermissionViewQuota || m.Role == EnterpriseRoleMember)
								}
								if role != EnterpriseRoleOwner && op == EnterprisePermissionViewQuota && m.ID == 10 {
									// Own quota is always visible regardless of the legacy visibility setting.
									allowed = true
								}
								err := CheckEnterprisePermission(c, settings, op, EnterprisePermissionTarget{ScopeType: EnterpriseScopeMember, ScopeID: m.ID, Member: &m})
								expected := error(nil)
								if memberDisabled {
									expected = ErrEnterpriseMemberDisabled
								} else if enterpriseDisabled && write {
									expected = ErrEnterpriseDisabled
								} else if op == EnterprisePermissionEditWhitelist {
									expected = ErrEnterpriseGroupScopeDisabled
								} else if !allowed {
									expected = ErrEnterprisePermissionDenied
								}
								if err != expected {
									t.Errorf("%s: got %v, want %v", op, err, expected)
								}
							}
						})
					}
				}
			}
		}
	}
}
func TestEnterprisePermissionsVisibilityAndOwnerOperations(t *testing.T) {
	for _, role := range []string{EnterpriseRoleOwner, EnterpriseRoleManager, EnterpriseRoleMember} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", role, enabled), func(t *testing.T) {
				settings := EnterpriseSettings{BalanceVisibleToManager: enabled, BalanceVisibleToMember: enabled, DepartmentQuotaVisibleToManager: enabled}
				c := &EnterpriseMemberContext{EnterpriseID: 1, MemberID: 2, DepartmentID: enterpriseTestID(3), Role: role, EnterpriseStatus: EnterpriseStatusActive, MemberStatus: EnterpriseMemberStatusActive}
				if got, want := EnterpriseBalanceVisible(role, settings), role == EnterpriseRoleOwner || enabled; got != want {
					t.Errorf("balance visibility got %t want %t", got, want)
				}
				for _, op := range []EnterprisePermission{EnterprisePermissionManageDepartments, EnterprisePermissionSettings, EnterprisePermissionAudit, EnterprisePermissionDepartmentUsage, EnterprisePermissionResetQuota} {
					err := CheckEnterprisePermission(c, settings, op, EnterprisePermissionTarget{})
					if (err == nil) != (role == EnterpriseRoleOwner || (role == EnterpriseRoleManager && op == EnterprisePermissionDepartmentUsage)) {
						t.Errorf("%s unexpectedly allowed=%t", op, err == nil)
					}
				}
				for _, scopeID := range []int64{3, 4} {
					err := CheckEnterprisePermission(c, settings, EnterprisePermissionViewQuota, EnterprisePermissionTarget{ScopeType: EnterpriseScopeDepartment, ScopeID: scopeID})
					want := role == EnterpriseRoleOwner || (role == EnterpriseRoleManager && enabled && scopeID == 3)
					if (err == nil) != want {
						t.Errorf("department=%d allowed=%t want=%t", scopeID, err == nil, want)
					}
				}
				permissions := EnterprisePermissionsFor(c, settings)
				if permissions.ViewMembers != (role != EnterpriseRoleMember) {
					t.Errorf("view members %+v", permissions)
				}
				c.EnterpriseStatus = EnterpriseStatusDisabled
				p := EnterprisePermissionsFor(c, settings)
				if p.ManageDepartments || p.ManageMembers || p.CreateMembers || p.EditMemberQuota || p.EditMemberGroups || p.EditQuotas || p.EditSettings {
					t.Errorf("disabled enterprise writes %+v", p)
				}
				c.MemberStatus = EnterpriseMemberStatusDisabled
				if p := EnterprisePermissionsFor(c, settings); p != (EnterprisePermissions{}) {
					t.Errorf("disabled member %+v", p)
				}
			})
		}
	}
	if err := CheckEnterprisePermission(nil, DefaultEnterpriseSettings(), EnterprisePermissionRead, EnterprisePermissionTarget{}); err != ErrEnterpriseNotMember {
		t.Fatal(err)
	}
}
