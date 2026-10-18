//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseUsageConstrainScope(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	department := int64(8)
	for _, tt := range []struct {
		name, role                         string
		disabled                           bool
		requestedUser, requestedDepartment int64
		wantUser, wantDepartment           int64
		wantErr                            error
	}{
		{name: "admin can narrow", requestedUser: 42, requestedDepartment: 9, wantUser: 42, wantDepartment: 9},
		{name: "owner whole enterprise", role: EnterpriseRoleOwner},
		{name: "owner narrows department", role: EnterpriseRoleOwner, requestedDepartment: 9, wantDepartment: 9},
		{name: "manager department enforced", role: EnterpriseRoleManager, wantDepartment: 8},
		{name: "manager can narrow user", role: EnterpriseRoleManager, requestedUser: 42, wantUser: 42, wantDepartment: 8},
		{name: "manager cannot widen department", role: EnterpriseRoleManager, requestedDepartment: 9, wantErr: ErrEnterprisePermissionDenied},
		{name: "member self enforced", role: EnterpriseRoleMember, wantUser: 7},
		{name: "member cannot query another user", role: EnterpriseRoleMember, requestedUser: 42, wantErr: ErrEnterprisePermissionDenied},
		{name: "member may narrow old department", role: EnterpriseRoleMember, requestedDepartment: 9, wantUser: 7, wantDepartment: 9},
		{name: "disabled member", role: EnterpriseRoleOwner, disabled: true, wantErr: ErrEnterpriseMemberDisabled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			scope := EnterpriseUsageScope{EnterpriseID: 3, UserID: 7}
			if tt.role != "" {
				status := EnterpriseMemberStatusActive
				if tt.disabled {
					status = EnterpriseMemberStatusDisabled
				}
				scope.Member = &EnterpriseMemberContext{EnterpriseID: 3, Role: tt.role, MemberStatus: status, EnterpriseStatus: EnterpriseStatusDisabled, DepartmentID: &department}
			}
			f, err := ConstrainEnterpriseUsageFilters(scope, usagestats.UsageLogFilters{UserID: tt.requestedUser, EnterpriseDepartmentID: tt.requestedDepartment}, now)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 3, f.EnterpriseID)
			require.Equal(t, tt.wantUser, f.UserID)
			require.Equal(t, tt.wantDepartment, f.EnterpriseDepartmentID)
			require.Equal(t, 30*24*time.Hour, f.EndTime.Sub(*f.StartTime))
			require.True(t, f.ExactTotal)
		})
	}
	scope := EnterpriseUsageScope{EnterpriseID: 3, UserID: 7, Member: &EnterpriseMemberContext{EnterpriseID: 3, Role: EnterpriseRoleManager, MemberStatus: EnterpriseMemberStatusActive}}
	_, err := ConstrainEnterpriseUsageFilters(scope, usagestats.UsageLogFilters{}, now)
	require.ErrorIs(t, err, ErrEnterprisePermissionDenied)
}
func TestEnterpriseUsageRangeValidation(t *testing.T) {
	now := time.Now()
	scope := EnterpriseUsageScope{EnterpriseID: 1}
	for _, days := range []int{1, 30, 366, 367} {
		t.Run(time.Duration(days).String(), func(t *testing.T) {
			start := now.Add(-time.Duration(days) * 24 * time.Hour)
			_, err := ConstrainEnterpriseUsageFilters(scope, usagestats.UsageLogFilters{StartTime: &start, EndTime: &now}, now)
			require.Equal(t, days > 366, err != nil)
		})
	}
	_, err := ConstrainEnterpriseUsageFilters(scope, usagestats.UsageLogFilters{StartTime: &now, EndTime: &now}, now)
	require.Error(t, err)
	_, err = ConstrainEnterpriseUsageFilters(scope, usagestats.UsageLogFilters{EnterpriseID: 2}, now)
	require.ErrorIs(t, err, ErrEnterprisePermissionDenied)
}
func TestEnterpriseUsageDTORedactsSensitiveAssociations(t *testing.T) {
	value := EnterpriseUsageLog{ID: 1, User: EnterpriseUsageUser{ID: 2, Username: "a", Email: "a@example.com"}, APIKey: &EnterpriseUsageNamedResource{ID: 3, Name: "key"}, ActualCost: 2}
	data, err := json.Marshal(value)
	require.NoError(t, err)
	for _, s := range []string{`"key":`, `"balance":`, `"account":`, `"account_id":`, `"upstream_endpoint":`, `"account_rate_multiplier":`, `"account_stats_cost":`, `"session_id":`} {
		require.NotContains(t, string(data), s)
	}
	require.Contains(t, string(data), `"api_key":{"id":3,"name":"key"}`)
}

type enterpriseUsageRepositoryStub struct {
	UsageLogRepository
	captured usagestats.UsageLogFilters
}

func (s *enterpriseUsageRepositoryStub) ListEnterpriseUsageLogs(_ context.Context, _ pagination.PaginationParams, f usagestats.UsageLogFilters) ([]EnterpriseUsageLog, *pagination.PaginationResult, error) {
	s.captured = f
	return []EnterpriseUsageLog{}, &pagination.PaginationResult{}, nil
}
func (s *enterpriseUsageRepositoryStub) GetStatsWithFilters(_ context.Context, f usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	s.captured = f
	return &usagestats.UsageStats{TotalRequests: 1, TotalTokens: 2, TotalActualCost: 3}, nil
}
func (s *enterpriseUsageRepositoryStub) GetEnterpriseMemberUsage(_ context.Context, f usagestats.UsageLogFilters) ([]EnterpriseMemberUsage, error) {
	s.captured = f
	return []EnterpriseMemberUsage{}, nil
}
func (s *enterpriseUsageRepositoryStub) GetEnterpriseDepartmentUsage(_ context.Context, f usagestats.UsageLogFilters) ([]EnterpriseDepartmentUsage, error) {
	s.captured = f
	return []EnterpriseDepartmentUsage{}, nil
}
func TestEnterpriseUsageServiceAppliesScopeBeforeRepository(t *testing.T) {
	repo := &enterpriseUsageRepositoryStub{}
	svc := NewEnterpriseUsageService(repo)
	scope := EnterpriseUsageScope{EnterpriseID: 5, UserID: 9, Member: &EnterpriseMemberContext{EnterpriseID: 5, Role: EnterpriseRoleMember, MemberStatus: EnterpriseMemberStatusActive}}
	_, _, err := svc.List(context.Background(), scope, pagination.DefaultPagination(), usagestats.UsageLogFilters{})
	require.NoError(t, err)
	require.EqualValues(t, 9, repo.captured.UserID)
	require.EqualValues(t, 5, repo.captured.EnterpriseID)
	stats, err := svc.Stats(context.Background(), scope, usagestats.UsageLogFilters{})
	require.NoError(t, err)
	require.EqualValues(t, 3, stats.TotalActualCost)
	_, err = svc.Departments(context.Background(), scope, usagestats.UsageLogFilters{})
	require.ErrorIs(t, err, ErrEnterprisePermissionDenied)
	_, _, err = svc.List(context.Background(), scope, pagination.DefaultPagination(), usagestats.UsageLogFilters{UserID: 10})
	require.ErrorIs(t, err, ErrEnterprisePermissionDenied)
}
