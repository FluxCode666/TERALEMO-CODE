package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func appendEnterpriseUsageWhereConditions(conditions []string, args []any, filters UsageLogFilters, alias string) ([]string, []any) {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	if filters.EnterpriseID != 0 {
		conditions = append(conditions, fmt.Sprintf("%senterprise_id = $%d", prefix, len(args)+1))
		args = append(args, filters.EnterpriseID)
	}
	if filters.EnterpriseDepartmentID != 0 {
		conditions = append(conditions, fmt.Sprintf("%senterprise_department_id = $%d", prefix, len(args)+1))
		args = append(args, filters.EnterpriseDepartmentID)
	}
	return conditions, args
}
func appendEnterpriseUsageQueryFilter(query string, args []any, filters UsageLogFilters, alias string) (string, []any) {
	conditions, args := appendEnterpriseUsageWhereConditions(nil, args, filters, alias)
	if len(conditions) > 0 {
		query += " AND " + strings.Join(conditions, " AND ")
	}
	return query, args
}

// Enterprise records are selected by immutable usage snapshots. These joins only
// supply display labels; current membership never determines visibility.
func enterpriseUsageWhere(filters UsageLogFilters) (string, []any, error) {
	if filters.EnterpriseID <= 0 {
		return "", nil, service.ErrEnterpriseNotFound
	}
	c, args := appendEnterpriseUsageWhereConditions(nil, nil, filters, "ul")
	for _, v := range []struct {
		column string
		value  int64
	}{{"user_id", filters.UserID}, {"api_key_id", filters.APIKeyID}, {"group_id", filters.GroupID}, {"account_id", filters.AccountID}} {
		if v.value > 0 {
			c = append(c, fmt.Sprintf("ul.%s = $%d", v.column, len(args)+1))
			args = append(args, v.value)
		}
	}
	if filters.Model != "" {
		c = append(c, fmt.Sprintf("COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model) = $%d", len(args)+1))
		args = append(args, filters.Model)
	}
	if filters.StartTime != nil {
		c = append(c, fmt.Sprintf("ul.created_at >= $%d", len(args)+1))
		args = append(args, *filters.StartTime)
	}
	if filters.EndTime != nil {
		c = append(c, fmt.Sprintf("ul.created_at < $%d", len(args)+1))
		args = append(args, *filters.EndTime)
	}
	return buildWhere(c), args, nil
}

func (r *usageLogRepository) ListEnterpriseUsageLogs(ctx context.Context, params pagination.PaginationParams, filters UsageLogFilters) ([]service.EnterpriseUsageLog, *pagination.PaginationResult, error) {
	where, args, err := enterpriseUsageWhere(filters)
	if err != nil {
		return nil, nil, err
	}
	var total int64
	if err := scanSingleRow(ctx, r.sql, "SELECT COUNT(*) FROM usage_logs ul "+where, args, &total); err != nil {
		return nil, nil, translatePersistenceError(err, nil, nil)
	}
	query := `SELECT ul.id,ul.user_id,COALESCE(u.username,''),COALESCE(u.email,''),ul.enterprise_department_id,
 k.id,COALESCE(k.name,''),g.id,COALESCE(g.name,''),COALESCE(NULLIF(TRIM(ul.requested_model),''),ul.model),
 ul.input_tokens,ul.output_tokens,ul.cache_creation_tokens,ul.cache_read_tokens,ul.actual_cost,ul.billing_type,ul.request_type,ul.stream,COALESCE(ul.duration_ms,0),ul.first_token_ms,ul.created_at
 FROM usage_logs ul LEFT JOIN users u ON u.id=ul.user_id LEFT JOIN api_keys k ON k.id=ul.api_key_id LEFT JOIN groups g ON g.id=ul.group_id ` + where
	query += fmt.Sprintf(" ORDER BY ul.created_at DESC,ul.id DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	args = append(args, params.Limit(), params.Offset())
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.EnterpriseUsageLog, 0)
	for rows.Next() {
		var v service.EnterpriseUsageLog
		var dept, key, group sql.NullInt64
		var first sql.NullInt64
		var keyName, groupName string
		if err := rows.Scan(&v.ID, &v.User.ID, &v.User.Username, &v.User.Email, &dept, &key, &keyName, &group, &groupName, &v.Model, &v.InputTokens, &v.OutputTokens, &v.CacheCreationTokens, &v.CacheReadTokens, &v.ActualCost, &v.BillingType, &v.RequestType, &v.Stream, &v.DurationMs, &first, &v.CreatedAt); err != nil {
			return nil, nil, err
		}
		if dept.Valid {
			v.DepartmentID = &dept.Int64
		}
		if key.Valid {
			v.APIKey = &service.EnterpriseUsageNamedResource{ID: key.Int64, Name: keyName}
		}
		if group.Valid {
			v.Group = &service.EnterpriseUsageNamedResource{ID: group.Int64, Name: groupName}
		}
		if first.Valid {
			n := int(first.Int64)
			v.FirstTokenMs = &n
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return out, paginationResultFromTotal(total, params), nil
}

func (r *usageLogRepository) GetEnterpriseMemberUsage(ctx context.Context, filters UsageLogFilters) ([]service.EnterpriseMemberUsage, error) {
	where, args, err := enterpriseUsageWhere(filters)
	if err != nil {
		return nil, err
	}
	// A member may have consumed in several departments. Aggregate once per user;
	// the department label reflects the most recent consumption in this range.
	query := `SELECT ul.user_id,COALESCE(u.username,''),COALESCE(u.email,''),
 (array_agg(ul.enterprise_department_id ORDER BY ul.created_at DESC,ul.id DESC))[1],
 (array_agg(d.name ORDER BY ul.created_at DESC,ul.id DESC))[1],COUNT(*),
 COALESCE(SUM(ul.input_tokens+ul.output_tokens+ul.cache_creation_tokens+ul.cache_read_tokens),0),COALESCE(SUM(ul.actual_cost),0)
 FROM usage_logs ul LEFT JOIN users u ON u.id=ul.user_id
 LEFT JOIN enterprise_departments d ON d.id=ul.enterprise_department_id AND d.enterprise_id=ul.enterprise_id ` + where + `
 GROUP BY ul.user_id,u.username,u.email ORDER BY SUM(ul.actual_cost) DESC,ul.user_id ASC`
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.EnterpriseMemberUsage, 0)
	for rows.Next() {
		var v service.EnterpriseMemberUsage
		var dept sql.NullInt64
		var name sql.NullString
		if err := rows.Scan(&v.UserID, &v.Username, &v.Email, &dept, &name, &v.Requests, &v.TotalTokens, &v.ActualCost); err != nil {
			return nil, err
		}
		if dept.Valid {
			v.DepartmentID = &dept.Int64
		}
		if name.Valid {
			v.DepartmentName = &name.String
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *usageLogRepository) GetEnterpriseDepartmentUsage(ctx context.Context, filters UsageLogFilters) ([]service.EnterpriseDepartmentUsage, error) {
	where, args, err := enterpriseUsageWhere(filters)
	if err != nil {
		return nil, err
	}
	query := `SELECT ul.enterprise_department_id,d.name,COUNT(*),
 COALESCE(SUM(ul.input_tokens+ul.output_tokens+ul.cache_creation_tokens+ul.cache_read_tokens),0),COALESCE(SUM(ul.actual_cost),0)
 FROM usage_logs ul LEFT JOIN enterprise_departments d ON d.id=ul.enterprise_department_id AND d.enterprise_id=ul.enterprise_id ` + where + `
 GROUP BY ul.enterprise_department_id,d.name ORDER BY SUM(ul.actual_cost) DESC,ul.enterprise_department_id ASC NULLS LAST`
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.EnterpriseDepartmentUsage, 0)
	for rows.Next() {
		var v service.EnterpriseDepartmentUsage
		var dept sql.NullInt64
		var name sql.NullString
		if err := rows.Scan(&dept, &name, &v.Requests, &v.TotalTokens, &v.ActualCost); err != nil {
			return nil, err
		}
		if dept.Valid {
			v.DepartmentID = &dept.Int64
		}
		if name.Valid {
			v.DepartmentName = &name.String
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

var _ service.EnterpriseUsageLogReader = (*usageLogRepository)(nil)
var _ service.EnterpriseUsageRankingRepository = (*usageLogRepository)(nil)
