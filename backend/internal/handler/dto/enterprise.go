package dto

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type Enterprise struct {
	ID                  int64                      `json:"id"`
	Name                string                     `json:"name"`
	Status              string                     `json:"status"`
	Balance             float64                    `json:"balance"`
	TotalRecharged      float64                    `json:"total_recharged"`
	LowBalanceThreshold *float64                   `json:"low_balance_threshold"`
	ContactName         string                     `json:"contact_name"`
	ContactEmail        string                     `json:"contact_email"`
	ContactPhone        string                     `json:"contact_phone"`
	Notes               string                     `json:"notes"`
	Settings            service.EnterpriseSettings `json:"settings"`
	MemberCount         int64                      `json:"member_count"`
	DepartmentCount     int64                      `json:"department_count"`
	Quota               *EnterpriseQuota           `json:"quota,omitempty"`
	CreatedAt           time.Time                  `json:"created_at"`
	UpdatedAt           time.Time                  `json:"updated_at"`
}

func EnterpriseFromService(e *service.Enterprise) *Enterprise {
	if e == nil {
		return nil
	}
	return &Enterprise{ID: e.ID, Name: e.Name, Status: e.Status, Balance: e.Balance, TotalRecharged: e.TotalRecharged, LowBalanceThreshold: e.LowBalanceThreshold, ContactName: e.ContactName, ContactEmail: e.ContactEmail, ContactPhone: e.ContactPhone, Notes: e.Notes, Settings: e.Settings, MemberCount: e.MemberCount, DepartmentCount: e.DepartmentCount, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
}

type EnterpriseDepartment struct {
	ID           int64     `json:"id"`
	EnterpriseID int64     `json:"enterprise_id"`
	Name         string    `json:"name"`
	SortOrder    int       `json:"sort_order"`
	MemberCount  int64     `json:"member_count"`
	CreatedAt    time.Time `json:"created_at"`
}

func EnterpriseDepartmentFromService(d *service.EnterpriseDepartment) *EnterpriseDepartment {
	if d == nil {
		return nil
	}
	return &EnterpriseDepartment{ID: d.ID, EnterpriseID: d.EnterpriseID, Name: d.Name, SortOrder: d.SortOrder, MemberCount: d.MemberCount, CreatedAt: d.CreatedAt}
}

type EnterpriseMember struct {
	ID             int64     `json:"id"`
	EnterpriseID   int64     `json:"enterprise_id"`
	UserID         int64     `json:"user_id"`
	Email          string    `json:"email"`
	Username       string    `json:"username"`
	DepartmentID   *int64    `json:"department_id"`
	DepartmentName *string   `json:"department_name"`
	Role           string    `json:"role"`
	Status         string    `json:"status"`
	Title          string    `json:"title"`
	JoinedAt       time.Time `json:"joined_at"`
	UserStatus     string    `json:"user_status"`
}

func EnterpriseMemberFromService(m *service.EnterpriseMember) *EnterpriseMember {
	if m == nil {
		return nil
	}
	var departmentName *string
	if m.DepartmentID != nil {
		n := m.DepartmentName
		departmentName = &n
	}
	return &EnterpriseMember{ID: m.ID, EnterpriseID: m.EnterpriseID, UserID: m.UserID, Email: m.Email, Username: m.Username, DepartmentID: m.DepartmentID, DepartmentName: departmentName, Role: m.Role, Status: m.Status, Title: m.Title, JoinedAt: m.JoinedAt, UserStatus: m.UserStatus}
}

type EnterpriseQuota struct {
	ScopeType       string    `json:"scope_type"`
	ScopeID         int64     `json:"scope_id"`
	DailyLimitUSD   *float64  `json:"daily_limit_usd"`
	WeeklyLimitUSD  *float64  `json:"weekly_limit_usd"`
	MonthlyLimitUSD *float64  `json:"monthly_limit_usd"`
	TotalLimitUSD   *float64  `json:"total_limit_usd"`
	DailyUsageUSD   float64   `json:"daily_usage_usd"`
	WeeklyUsageUSD  float64   `json:"weekly_usage_usd"`
	MonthlyUsageUSD float64   `json:"monthly_usage_usd"`
	TotalUsageUSD   float64   `json:"total_usage_usd"`
	DailyResetsAt   time.Time `json:"daily_resets_at"`
	WeeklyResetsAt  time.Time `json:"weekly_resets_at"`
	MonthlyResetsAt time.Time `json:"monthly_resets_at"`
}

func EnterpriseQuotaFromService(q *service.EnterpriseQuota) *EnterpriseQuota {
	if q == nil {
		return nil
	}
	now := timezone.Now()
	day, week, month := timezone.StartOfDay(now), timezone.StartOfWeek(now), timezone.StartOfMonth(now)
	effective := func(start *time.Time, boundary time.Time, usage float64) float64 {
		if start == nil || start.Before(boundary) {
			return 0
		}
		return usage
	}
	return &EnterpriseQuota{ScopeType: q.ScopeType, ScopeID: q.ScopeID, DailyLimitUSD: q.DailyLimitUSD, WeeklyLimitUSD: q.WeeklyLimitUSD, MonthlyLimitUSD: q.MonthlyLimitUSD, TotalLimitUSD: q.TotalLimitUSD, DailyUsageUSD: effective(q.DailyWindowStart, day, q.DailyUsageUSD), WeeklyUsageUSD: effective(q.WeeklyWindowStart, week, q.WeeklyUsageUSD), MonthlyUsageUSD: effective(q.MonthlyWindowStart, month, q.MonthlyUsageUSD), TotalUsageUSD: q.TotalUsageUSD, DailyResetsAt: day.AddDate(0, 0, 1), WeeklyResetsAt: week.AddDate(0, 0, 7), MonthlyResetsAt: month.AddDate(0, 1, 0)}
}

type EnterpriseGroup struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	Platform         string `json:"platform"`
	IsExclusive      bool   `json:"is_exclusive"`
	SubscriptionType string `json:"subscription_type"`
	Status           string `json:"status"`
}

func EnterpriseGroupFromService(g *service.Group) *EnterpriseGroup {
	if g == nil {
		return nil
	}
	return &EnterpriseGroup{ID: g.ID, Name: g.Name, Platform: g.Platform, IsExclusive: g.IsExclusive, SubscriptionType: g.SubscriptionType, Status: g.Status}
}

type EnterpriseGroupWhitelist struct {
	ScopeType string  `json:"scope_type"`
	ScopeID   int64   `json:"scope_id"`
	GroupIDs  []int64 `json:"group_ids"`
}

func EnterpriseGroupWhitelistFromService(w *service.EnterpriseGroupWhitelist) *EnterpriseGroupWhitelist {
	if w == nil {
		return nil
	}
	ids := append([]int64{}, w.GroupIDs...)
	return &EnterpriseGroupWhitelist{ScopeType: w.ScopeType, ScopeID: w.ScopeID, GroupIDs: ids}
}

type EnterpriseBalanceLog struct {
	ID             int64     `json:"id"`
	Type           string    `json:"type"`
	Amount         float64   `json:"amount"`
	BalanceBefore  float64   `json:"balance_before"`
	BalanceAfter   float64   `json:"balance_after"`
	OperatorUserID *int64    `json:"operator_user_id"`
	OperatorEmail  string    `json:"operator_email"`
	Notes          string    `json:"notes"`
	CreatedAt      time.Time `json:"created_at"`
}

func EnterpriseBalanceLogFromService(l *service.EnterpriseBalanceLog) *EnterpriseBalanceLog {
	if l == nil {
		return nil
	}
	return &EnterpriseBalanceLog{ID: l.ID, Type: l.Type, Amount: l.Amount, BalanceBefore: l.BalanceBefore, BalanceAfter: l.BalanceAfter, OperatorUserID: l.OperatorUserID, OperatorEmail: l.OperatorEmail, Notes: l.Notes, CreatedAt: l.CreatedAt}
}

type EnterpriseAuditLog struct {
	ID          int64          `json:"id"`
	ActorUserID int64          `json:"actor_user_id"`
	ActorEmail  string         `json:"actor_email"`
	ActorRole   string         `json:"actor_role"`
	Action      string         `json:"action"`
	TargetType  string         `json:"target_type"`
	TargetID    *int64         `json:"target_id"`
	Detail      map[string]any `json:"detail"`
	CreatedAt   time.Time      `json:"created_at"`
}

func EnterpriseAuditLogFromService(l *service.EnterpriseAuditLog) *EnterpriseAuditLog {
	if l == nil {
		return nil
	}
	detail := l.Detail
	if detail == nil {
		detail = map[string]any{}
	}
	return &EnterpriseAuditLog{ID: l.ID, ActorUserID: l.ActorUserID, ActorEmail: l.ActorEmail, ActorRole: l.ActorRole, Action: l.Action, TargetType: l.TargetType, TargetID: l.TargetID, Detail: detail, CreatedAt: l.CreatedAt}
}

type EnterpriseOverview struct {
	Enterprise             EnterpriseOverviewCompany     `json:"enterprise"`
	Me                     EnterpriseOverviewMember      `json:"me"`
	BalanceVisible         bool                          `json:"balance_visible"`
	Balance                *float64                      `json:"balance"`
	EnterpriseQuota        *EnterpriseQuota              `json:"enterprise_quota"`
	DepartmentQuotaVisible bool                          `json:"department_quota_visible"`
	DepartmentQuota        *EnterpriseQuota              `json:"department_quota"`
	MemberQuotaVisible     bool                          `json:"member_quota_visible"`
	MemberQuota            *EnterpriseQuota              `json:"member_quota"`
	Settings               *service.EnterpriseSettings   `json:"settings"`
	Permissions            service.EnterprisePermissions `json:"permissions"`
	Stats                  EnterpriseOverviewStats       `json:"stats"`
}
type EnterpriseOverviewCompany struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}
type EnterpriseOverviewMember struct {
	MemberID       int64   `json:"member_id"`
	UserID         int64   `json:"user_id"`
	Role           string  `json:"role"`
	Status         string  `json:"status"`
	DepartmentID   *int64  `json:"department_id"`
	DepartmentName *string `json:"department_name"`
	Title          string  `json:"title"`
}
type EnterpriseOverviewStats struct {
	TodayCost float64 `json:"today_cost"`
	MonthCost float64 `json:"month_cost"`
}

func EnterpriseOverviewFromService(o *service.EnterpriseOverviewData) *EnterpriseOverview {
	if o == nil {
		return nil
	}
	member := EnterpriseMemberFromService(o.Me)
	return &EnterpriseOverview{Enterprise: EnterpriseOverviewCompany{ID: o.Enterprise.ID, Name: o.Enterprise.Name, Status: o.Enterprise.Status}, Me: EnterpriseOverviewMember{MemberID: member.ID, UserID: member.UserID, Role: member.Role, Status: member.Status, DepartmentID: member.DepartmentID, DepartmentName: member.DepartmentName, Title: member.Title}, BalanceVisible: o.BalanceVisible, Balance: o.Balance, EnterpriseQuota: EnterpriseQuotaFromService(o.EnterpriseQuota), DepartmentQuotaVisible: o.DepartmentQuotaVisible, DepartmentQuota: EnterpriseQuotaFromService(o.DepartmentQuota), MemberQuotaVisible: o.MemberQuotaVisible, MemberQuota: EnterpriseQuotaFromService(o.MemberQuota), Settings: o.Settings, Permissions: o.Permissions}
}
