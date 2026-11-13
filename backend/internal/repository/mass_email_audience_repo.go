package repository

import (
	"context"
	"sort"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enterprise"
	"github.com/Wei-Shaw/sub2api/ent/enterprisemember"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// ResolveMassEmailUsers 解析用户来源的候选收件人。
//
// includeAll=true 表示"全部用户"，忽略 filter；否则按现有字段筛选：
// 状态、角色、分组（user_allowed_groups）、邮箱/用户名模糊搜索、注册时间区间。
func (r *massEmailRepository) ResolveMassEmailUsers(
	ctx context.Context,
	filter domain.MassEmailUserFilter,
	includeAll bool,
) ([]domain.MassEmailRecipientCandidate, error) {
	// 关键边界：user_filter 是值类型，反序列化 null/未传后是零值。
	// 只有显式"全部用户"或有真实条件时才解析用户来源，否则返回空（不能因为零值就查全表）。
	if !filter.WantsAllUsers(includeAll) {
		return []domain.MassEmailRecipientCandidate{}, nil
	}

	client := clientFromContext(ctx, r.client)
	query := client.User.Query()

	if !includeAll {
		if statuses := massEmailNonEmptyStrings(filter.Statuses); len(statuses) > 0 {
			query = query.Where(user.StatusIn(statuses...))
		}
		if roles := massEmailNonEmptyStrings(filter.Roles); len(roles) > 0 {
			query = query.Where(user.RoleIn(roles...))
		}
		if groupIDs := massEmailPositiveIDs(filter.GroupIDs); len(groupIDs) > 0 {
			query = query.Where(user.HasAllowedGroupsWith(group.IDIn(groupIDs...)))
		}
		if search := strings.TrimSpace(filter.Search); search != "" {
			query = query.Where(user.Or(
				user.EmailContainsFold(search),
				user.UsernameContainsFold(search),
			))
		}
		if filter.CreatedFrom != nil {
			query = query.Where(user.CreatedAtGTE(*filter.CreatedFrom))
		}
		if filter.CreatedTo != nil {
			query = query.Where(user.CreatedAtLTE(*filter.CreatedTo))
		}
	}

	users, err := query.
		Select(user.FieldID, user.FieldEmail, user.FieldUsername).
		Order(dbent.Asc(user.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]domain.MassEmailRecipientCandidate, 0, len(users))
	for _, item := range users {
		email := domain.NormalizeEmail(item.Email)
		if !domain.IsValidMassEmailAddress(email) {
			continue
		}
		userID := item.ID
		out = append(out, domain.MassEmailRecipientCandidate{
			Email:  email,
			Name:   massEmailDisplayName(item.Username, email),
			UserID: &userID,
			Source: domain.MassEmailSourceUser,
		})
	}
	return out, nil
}

// ResolveMassEmailEnterprises 解析企业来源的候选收件人。
//
// scope=contacts 只取企业联系人邮箱；members 只取企业成员（关联用户）邮箱；both 两者都取。
// 企业联系人排在前面：同一邮箱同时是联系人和成员时，服务层按邮箱去重会保留联系人这条。
func (r *massEmailRepository) ResolveMassEmailEnterprises(
	ctx context.Context,
	enterpriseIDs []int64,
	scope string,
) ([]domain.MassEmailRecipientCandidate, error) {
	ids := massEmailPositiveIDs(enterpriseIDs)
	if len(ids) == 0 {
		return []domain.MassEmailRecipientCandidate{}, nil
	}

	filter := domain.MassEmailEnterpriseFilter{EnterpriseIDs: ids, Scope: scope}
	if filter.NormalizedScope() == "" {
		return nil, domain.ErrMassEmailInvalidFilter
	}

	client := clientFromContext(ctx, r.client)
	out := make([]domain.MassEmailRecipientCandidate, 0, len(ids))

	if filter.WantsContacts() {
		contacts, err := client.Enterprise.Query().
			Where(
				enterprise.IDIn(ids...),
				enterprise.ContactEmailNEQ(""),
			).
			Select(
				enterprise.FieldID,
				enterprise.FieldContactEmail,
				enterprise.FieldContactName,
				enterprise.FieldName,
			).
			Order(dbent.Asc(enterprise.FieldID)).
			All(ctx)
		if err != nil {
			return nil, err
		}
		for _, contact := range contacts {
			email := domain.NormalizeEmail(contact.ContactEmail)
			if !domain.IsValidMassEmailAddress(email) {
				continue
			}
			enterpriseID := contact.ID
			name := strings.TrimSpace(contact.ContactName)
			if name == "" {
				name = contact.Name
			}
			out = append(out, domain.MassEmailRecipientCandidate{
				Email:        email,
				Name:         name,
				EnterpriseID: &enterpriseID,
				Source:       domain.MassEmailSourceEnterpriseContact,
			})
		}
	}

	if filter.WantsMembers() {
		members, err := client.EnterpriseMember.Query().
			Where(
				enterprisemember.EnterpriseIDIn(ids...),
				enterprisemember.StatusEQ(domain.EnterpriseMemberStatusActive),
			).
			Select(enterprisemember.FieldEnterpriseID, enterprisemember.FieldUserID).
			Order(dbent.Asc(enterprisemember.FieldID)).
			All(ctx)
		if err != nil {
			return nil, err
		}
		if len(members) > 0 {
			userIDs := make([]int64, 0, len(members))
			memberEnterprise := make(map[int64]int64, len(members))
			for _, member := range members {
				if _, ok := memberEnterprise[member.UserID]; ok {
					continue
				}
				memberEnterprise[member.UserID] = member.EnterpriseID
				userIDs = append(userIDs, member.UserID)
			}

			users, err := client.User.Query().
				Where(user.IDIn(userIDs...)).
				Select(user.FieldID, user.FieldEmail, user.FieldUsername).
				All(ctx)
			if err != nil {
				return nil, err
			}
			for _, item := range users {
				email := domain.NormalizeEmail(item.Email)
				if !domain.IsValidMassEmailAddress(email) {
					continue
				}
				enterpriseID, ok := memberEnterprise[item.ID]
				if !ok {
					continue
				}
				userID := item.ID
				enterpriseRef := enterpriseID
				out = append(out, domain.MassEmailRecipientCandidate{
					Email:        email,
					Name:         massEmailDisplayName(item.Username, email),
					UserID:       &userID,
					EnterpriseID: &enterpriseRef,
					Source:       domain.MassEmailSourceEnterpriseMember,
				})
			}
		}
	}

	return out, nil
}

// MassEmailEnterpriseNames 批量取企业名（软删除的企业自动过滤）。
func (r *massEmailRepository) MassEmailEnterpriseNames(ctx context.Context, enterpriseIDs []int64) (map[int64]string, error) {
	ids := massEmailPositiveIDs(enterpriseIDs)
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	client := clientFromContext(ctx, r.client)
	entities, err := client.Enterprise.Query().
		Where(enterprise.IDIn(ids...)).
		Select(enterprise.FieldID, enterprise.FieldName).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, entity := range entities {
		out[entity.ID] = entity.Name
	}
	return out, nil
}

// massEmailUserDisplayNames 批量取用户显示名（username 优先，空则用邮箱）。
func massEmailUserDisplayNames(ctx context.Context, client *dbent.Client, ids []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	users, err := client.User.Query().
		Where(user.IDIn(ids...)).
		Select(user.FieldID, user.FieldEmail, user.FieldUsername).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range users {
		out[item.ID] = massEmailDisplayName(item.Username, item.Email)
	}
	return out, nil
}

// massEmailDisplayName 生成收件人显示名：用户名优先，退化为邮箱本地部分。
func massEmailDisplayName(username, email string) string {
	name := strings.TrimSpace(username)
	if name != "" {
		return name
	}
	email = domain.NormalizeEmail(email)
	if at := strings.IndexByte(email, '@'); at > 0 {
		return email[:at]
	}
	return email
}

// massEmailNonEmptyStrings 去空白、去重，保留顺序。
func massEmailNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

// massEmailPositiveIDs 过滤非正数 ID 并去重。
func massEmailPositiveIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
