package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"slices"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type EnterpriseImportRow struct {
	Row            int     `json:"row"`
	Email          string  `json:"email"`
	Username       string  `json:"username"`
	DepartmentPath string  `json:"department_path"`
	Role           string  `json:"role"`
	Title          string  `json:"title"`
	Password       *string `json:"password,omitempty"`
}

type EnterpriseImportPreviewRow struct {
	Row            int      `json:"row"`
	Status         string   `json:"status"`
	Email          string   `json:"email"`
	DepartmentPath string   `json:"department_path"`
	Role           string   `json:"role"`
	Messages       []string `json:"messages"`
}

type EnterpriseImportPreviewSummary struct {
	Total  int `json:"total"`
	Create int `json:"create"`
	Update int `json:"update"`
	Skip   int `json:"skip"`
	Error  int `json:"error"`
}

type EnterpriseImportPreview struct {
	Summary             EnterpriseImportPreviewSummary `json:"summary"`
	DepartmentsToCreate []string                       `json:"departments_to_create"`
	Rows                []EnterpriseImportPreviewRow   `json:"rows"`
}

type EnterpriseImportCommitRow struct {
	Row               int    `json:"row"`
	Status            string `json:"status"`
	MemberID          int64  `json:"member_id,omitempty"`
	UserID            int64  `json:"user_id,omitempty"`
	GeneratedPassword string `json:"generated_password,omitempty"`
	Message           string `json:"message,omitempty"`
}

type EnterpriseImportCommitSummary struct {
	Total   int `json:"total"`
	Created int `json:"created"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
}

type EnterpriseImportCommit struct {
	Summary EnterpriseImportCommitSummary `json:"summary"`
	Rows    []EnterpriseImportCommitRow   `json:"rows"`
}

type enterpriseImportPlanRow struct {
	preview      EnterpriseImportPreviewRow
	input        EnterpriseImportRow
	user         *User
	member       *EnterpriseMember
	department   *EnterpriseDepartment
	missingPaths []string
	update       UpdateEnterpriseMemberInput
}

func enterpriseImportRole(role string) string {
	switch strings.TrimSpace(role) {
	case "企业负责人":
		return EnterpriseRoleOwner
	case "部门负责人":
		return EnterpriseRoleManager
	case "员工":
		return EnterpriseRoleMember
	default:
		return strings.TrimSpace(role)
	}
}

func enterpriseImportNormalize(row EnterpriseImportRow) EnterpriseImportRow {
	row.Email = strings.ToLower(strings.TrimSpace(row.Email))
	row.Username = strings.TrimSpace(row.Username)
	row.Title = strings.TrimSpace(row.Title)
	row.Role = enterpriseImportRole(row.Role)
	if row.Password != nil && *row.Password == "" {
		row.Password = nil
	}
	parts := strings.Split(strings.TrimSpace(row.DepartmentPath), "/")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	row.DepartmentPath = strings.Join(parts, "/")
	return row
}

func enterpriseImportDuplicates(rows []EnterpriseImportRow) map[string]bool {
	counts := map[string]int{}
	for _, row := range rows {
		counts[enterpriseImportNormalize(row).Email]++
	}
	out := map[string]bool{}
	for email, count := range counts {
		if count > 1 {
			out[email] = true
		}
	}
	return out
}

func enterpriseImportPermission(a EnterpriseActor, e *Enterprise, c *EnterpriseMemberContext) error {
	if a.IsAdmin || c.Role == EnterpriseRoleOwner {
		return nil
	}
	if c.Role == EnterpriseRoleManager && c.DepartmentID != nil && e.Settings.ManagerCanManageMembers {
		return nil
	}
	return ErrEnterprisePermissionDenied
}

func enterpriseImportDepartmentMap(items []EnterpriseDepartment) (map[string]*EnterpriseDepartment, error) {
	out := map[string]*EnterpriseDepartment{}
	byID := map[int64]EnterpriseDepartment{}
	for _, d := range items {
		byID[d.ID] = d
	}
	for _, d := range items {
		names := d.PathNames
		if len(names) == 0 {
			ids := FrozenEnterpriseDepartmentIDs(d.PathIDs, d.ID)
			for _, id := range ids {
				names = append(names, byID[id].Name)
			}
		}
		path := strings.Join(names, "/")
		if _, exists := out[path]; exists {
			return nil, ErrEnterpriseDepartmentNameExists
		}
		copy := d
		out[path] = &copy
	}
	return out, nil
}

// validateImportRow is read-only. It is used both by preview and again under
// the enterprise lock for every committed row; no preview state is trusted.
func (s *EnterpriseService) validateImportRow(ctx context.Context, a EnterpriseActor, e *Enterprise, c *EnterpriseMemberContext, raw EnterpriseImportRow, duplicates map[string]bool, departments map[string]*EnterpriseDepartment, owners int64) (enterpriseImportPlanRow, error) {
	in := enterpriseImportNormalize(raw)
	plan := enterpriseImportPlanRow{input: in, preview: EnterpriseImportPreviewRow{Row: in.Row, Email: in.Email, DepartmentPath: in.DepartmentPath, Role: in.Role, Messages: []string{}}}
	fail := func(err error) (enterpriseImportPlanRow, error) {
		plan.preview.Status = "error"
		plan.preview.Messages = []string{infraerrors.Message(err)}
		return plan, nil
	}
	if in.Row <= 0 {
		return fail(ErrEnterpriseInvalidInput)
	}
	if duplicates[in.Email] {
		return fail(infraerrors.BadRequest("ENTERPRISE_IMPORT_DUPLICATE_EMAIL", "文件内邮箱重复"))
	}
	address, err := mail.ParseAddress(in.Email)
	if err != nil || address.Address != in.Email || !enterpriseManagementTextValid(in.Email, 255) || !enterpriseManagementTextValid(in.Username, 100) || !enterpriseManagementTextValid(in.Title, 100) || !enterpriseValidRole(in.Role) {
		return fail(ErrEnterpriseInvalidInput)
	}
	if in.DepartmentPath != "" {
		parts := strings.Split(in.DepartmentPath, "/")
		if len(parts) > EnterpriseDepartmentMaxDepth {
			return fail(ErrEnterpriseDepartmentDepthExceeded)
		}
		var parent *EnterpriseDepartment
		for i, name := range parts {
			if name == "" || !enterpriseManagementTextValid(name, 100) {
				return fail(ErrEnterpriseInvalidInput)
			}
			path := strings.Join(parts[:i+1], "/")
			if d, ok := departments[path]; ok {
				parent = d
				continue
			}
			if !a.IsAdmin && c.Role != EnterpriseRoleOwner {
				return fail(infraerrors.Forbidden("ENTERPRISE_PERMISSION_DENIED", "部门负责人不能新建部门"))
			}
			plan.missingPaths = append(plan.missingPaths, path)
			// Only existing ancestor IDs matter for the prospective quota checks.
			virtual := &EnterpriseDepartment{EnterpriseID: e.ID, Name: name, Depth: i + 1}
			if parent != nil {
				virtual.PathIDs = slices.Clone(parent.PathIDs)
			}
			parent = virtual
		}
		plan.department = parent
	}
	if in.Role == EnterpriseRoleManager && plan.department == nil {
		return fail(ErrEnterpriseManagerRequiresDepartment)
	}
	if !a.IsAdmin && c.Role == EnterpriseRoleManager {
		if in.Role != EnterpriseRoleMember || plan.department == nil || !enterpriseDepartmentInSubtree(c, plan.department.ID) {
			return fail(ErrEnterprisePermissionDenied)
		}
	}
	reader, ok := s.users.(interface {
		GetByEmail(context.Context, string) (*User, error)
	})
	if !ok {
		return plan, fmt.Errorf("enterprise import user reader unavailable")
	}
	u, err := reader.GetByEmail(ctx, in.Email)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return plan, err
	}
	if err == nil && u != nil {
		plan.user = u
		existing, err := s.members.GetByUserID(ctx, u.ID)
		if err != nil && !enterpriseMissingMember(err) {
			return plan, err
		}
		if existing != nil && existing.EnterpriseID == e.ID {
			plan.member = existing
		} else {
			if !a.IsAdmin {
				return fail(infraerrors.BadRequest("ENTERPRISE_IMPORT_EMAIL_OCCUPIED", "邮箱已被占用"))
			}
			if u.Role == RoleAdmin {
				return fail(ErrEnterpriseMemberAdminForbidden)
			}
			if existing != nil {
				return fail(ErrEnterpriseMemberExists)
			}
		}
	}
	if plan.member == nil {
		if plan.user == nil && in.Password != nil && !enterprisePasswordValid(*in.Password) {
			return fail(ErrEnterpriseInvalidInput)
		}
		plan.preview.Status = "create"
		return plan, nil
	}
	m := plan.member
	if err := enterpriseCheck(a, c, e, EnterprisePermissionManageMember, EnterprisePermissionTarget{Member: m}); err != nil {
		return fail(err)
	}
	departmentID := int64(0)
	if plan.department != nil {
		departmentID = plan.department.ID
	}
	oldDepartmentID := int64(0)
	if m.DepartmentID != nil {
		oldDepartmentID = *m.DepartmentID
	}
	// Existing member import follows the normal update policy: a manager can
	// change titles, but cannot move members or change their roles.
	if !a.IsAdmin && c.Role == EnterpriseRoleManager && (oldDepartmentID != departmentID || m.Role != in.Role) {
		return fail(ErrEnterprisePermissionDenied)
	}
	if enterpriseMemberIsAvailableOwner(m) && in.Role != EnterpriseRoleOwner && owners <= 1 {
		return fail(ErrEnterpriseLastOwner)
	}
	if m.Role == in.Role && oldDepartmentID == departmentID && len(plan.missingPaths) == 0 && m.Title == in.Title {
		plan.preview.Status = "skip"
		return plan, nil
	}
	if oldDepartmentID != departmentID || len(plan.missingPaths) > 0 {
		q, err := s.quotas.GetByScope(ctx, e.ID, EnterpriseScopeMember, m.ID)
		if err != nil {
			return plan, err
		}
		if q == nil {
			return fail(ErrEnterpriseInvalidScope)
		}
		scopes := []EnterpriseQuotaScope{{ScopeType: EnterpriseScopeEnterprise, ScopeID: e.ID}}
		if plan.department != nil {
			ids := plan.department.PathIDs
			if len(ids) == 0 && plan.department.ID > 0 {
				ids = []int64{plan.department.ID}
			}
			for _, id := range ids {
				scopes = append(scopes, EnterpriseQuotaScope{ScopeType: EnterpriseScopeDepartment, ScopeID: id})
			}
		}
		parents, err := s.quotas.GetByScopes(ctx, e.ID, scopes)
		if err != nil {
			return plan, err
		}
		if len(parents) != len(scopes) {
			return fail(ErrEnterpriseInvalidScope)
		}
		for i := range parents {
			if err := enterpriseValidateQuotaParent(q, &parents[i]); err != nil {
				return fail(err)
			}
		}
	}
	plan.update = UpdateEnterpriseMemberInput{Title: &in.Title}
	if a.IsAdmin || c.Role == EnterpriseRoleOwner {
		plan.update.Role = &in.Role
		if oldDepartmentID != departmentID || len(plan.missingPaths) > 0 {
			plan.update.DepartmentID = &departmentID
		}
	}
	plan.preview.Status = "update"
	return plan, nil
}

func (s *EnterpriseService) importPlan(ctx context.Context, a EnterpriseActor, e *Enterprise, c *EnterpriseMemberContext, rows []EnterpriseImportRow) (*EnterpriseImportPreview, []enterpriseImportPlanRow, error) {
	if err := enterpriseImportPermission(a, e, c); err != nil {
		return nil, nil, err
	}
	items, err := s.departments.List(ctx, e.ID)
	if err != nil {
		return nil, nil, err
	}
	departments, err := enterpriseImportDepartmentMap(items)
	if err != nil {
		return nil, nil, err
	}
	owners, err := s.members.CountActiveOwners(ctx, e.ID)
	if err != nil {
		return nil, nil, err
	}
	duplicates := enterpriseImportDuplicates(rows)
	out := &EnterpriseImportPreview{Summary: EnterpriseImportPreviewSummary{Total: len(rows)}, DepartmentsToCreate: []string{}, Rows: []EnterpriseImportPreviewRow{}}
	plans := make([]enterpriseImportPlanRow, 0, len(rows))
	missing := map[string]bool{}
	for _, raw := range rows {
		plan, err := s.validateImportRow(ctx, a, e, c, raw, duplicates, departments, owners)
		if err != nil {
			return nil, nil, err
		}
		switch plan.preview.Status {
		case "create":
			out.Summary.Create++
		case "update":
			out.Summary.Update++
		case "skip":
			out.Summary.Skip++
		case "error":
			out.Summary.Error++
		}
		if plan.preview.Status != "error" {
			for _, path := range plan.missingPaths {
				if !missing[path] {
					missing[path] = true
					out.DepartmentsToCreate = append(out.DepartmentsToCreate, path)
				}
			}
			if plan.member != nil && plan.preview.Status == "update" {
				if enterpriseMemberIsAvailableOwner(plan.member) && plan.input.Role != EnterpriseRoleOwner {
					owners--
				}
				if plan.member.Role != EnterpriseRoleOwner && plan.input.Role == EnterpriseRoleOwner && plan.member.Status == EnterpriseMemberStatusActive && plan.member.UserStatus == StatusActive {
					owners++
				}
			}
			if plan.member == nil && plan.input.Role == EnterpriseRoleOwner && (plan.user == nil || plan.user.Status == StatusActive) {
				owners++
			}
		}
		plans = append(plans, plan)
		out.Rows = append(out.Rows, plan.preview)
	}
	return out, plans, nil
}

func (s *EnterpriseService) PreviewImport(ctx context.Context, a EnterpriseActor, id int64, rows []EnterpriseImportRow) (*EnterpriseImportPreview, error) {
	if len(rows) > 1000 {
		return nil, ErrEnterpriseInvalidInput
	}
	var out *EnterpriseImportPreview
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		var err error
		out, _, err = s.importPlan(ctx, a, e, c, rows)
		return err
	})
	return out, err
}

func (s *EnterpriseService) CommitImport(ctx context.Context, a EnterpriseActor, id int64, rows []EnterpriseImportRow) (*EnterpriseImportCommit, error) {
	if len(rows) > 1000 {
		return nil, ErrEnterpriseInvalidInput
	}
	var plans []enterpriseImportPlanRow
	var actorContext *EnterpriseMemberContext
	// All missing department paths are created atomically, before member rows.
	err := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
		actorContext = c
		preview, checked, err := s.importPlan(ctx, a, e, c, rows)
		if err != nil {
			return err
		}
		plans = checked
		items, err := s.departments.List(ctx, id)
		if err != nil {
			return err
		}
		departments, err := enterpriseImportDepartmentMap(items)
		if err != nil {
			return err
		}
		for _, path := range preview.DepartmentsToCreate {
			parts := strings.Split(path, "/")
			d := &EnterpriseDepartment{EnterpriseID: id, Name: parts[len(parts)-1]}
			if len(parts) > 1 {
				parent := departments[strings.Join(parts[:len(parts)-1], "/")]
				if parent == nil {
					return ErrEnterpriseDepartmentNotFound
				}
				d.ParentID = &parent.ID
			}
			if err := s.departments.Create(ctx, d); err != nil {
				return err
			}
			departments[path] = d
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidate(ctx, id)
	out := &EnterpriseImportCommit{Summary: EnterpriseImportCommitSummary{Total: len(rows)}, Rows: []EnterpriseImportCommitRow{}}
	duplicates := enterpriseImportDuplicates(rows)
	for i, raw := range rows {
		result := EnterpriseImportCommitRow{Row: raw.Row}
		// Rows that failed initial validation cannot create departments or members.
		if plans[i].preview.Status == "error" {
			result.Status = "failed"
			result.Message = strings.Join(plans[i].preview.Messages, "；")
		} else {
			var member *EnterpriseMember
			var createdUser bool
			var generated string
			rowErr := s.mutate(ctx, a, id, func(ctx context.Context, e *Enterprise, c *EnterpriseMemberContext) error {
				if err := enterpriseImportPermission(a, e, c); err != nil {
					return err
				}
				items, err := s.departments.List(ctx, id)
				if err != nil {
					return err
				}
				departments, err := enterpriseImportDepartmentMap(items)
				if err != nil {
					return err
				}
				owners, err := s.members.CountActiveOwners(ctx, id)
				if err != nil {
					return err
				}
				plan, err := s.validateImportRow(ctx, a, e, c, raw, duplicates, departments, owners)
				if err != nil {
					return err
				}
				if plan.preview.Status == "error" {
					return infraerrors.BadRequest("ENTERPRISE_IMPORT_ROW_INVALID", strings.Join(plan.preview.Messages, "；"))
				}
				if len(plan.missingPaths) > 0 {
					return ErrEnterpriseDepartmentNotFound
				}
				if plan.preview.Status == "skip" {
					member = plan.member
					result.Status = "skipped"
					return nil
				}
				if plan.preview.Status == "update" {
					member, err = s.updateMember(ctx, a, e, c, id, plan.member.ID, plan.update)
					result.Status = "updated"
					return err
				}
				input := CreateEnterpriseMemberInput{Email: plan.input.Email, Username: plan.input.Username, Role: plan.input.Role, Title: plan.input.Title}
				if plan.department != nil {
					input.DepartmentID = &plan.department.ID
				}
				if plan.user != nil {
					input.UserID = plan.user.ID
					input.Email = ""
					input.Username = ""
				} else if plan.input.Password != nil {
					input.Password = *plan.input.Password
				} else {
					input.Password, err = enterpriseGeneratePassword()
					if err != nil {
						return err
					}
					generated = input.Password
				}
				member, createdUser, err = s.createMember(ctx, a, c, e, input)
				result.Status = "created"
				return err
			})
			if rowErr != nil {
				result.Status = "failed"
				result.Message = infraerrors.Message(rowErr)
			} else if member != nil {
				result.MemberID, result.UserID = member.ID, member.UserID
				result.GeneratedPassword = generated
				if createdUser {
					s.afterUserCreated(ctx, member.UserID)
				}
				if result.Status != "skipped" {
					s.invalidate(ctx, id, member.UserID)
				}
			}
		}
		switch result.Status {
		case "created":
			out.Summary.Created++
		case "updated":
			out.Summary.Updated++
		case "skipped":
			out.Summary.Skipped++
		default:
			out.Summary.Failed++
		}
		out.Rows = append(out.Rows, result)
	}
	// Only aggregate counts are audited. Neither input rows nor results (which can
	// contain generated passwords) may enter audit detail.
	if err := s.withTransaction(ctx, func(ctx context.Context) error {
		return s.audit(ctx, a, actorContext, id, "member.import", EnterpriseScopeEnterprise, id, nil, out.Summary)
	}); err != nil {
		return nil, err
	}
	return out, nil
}
