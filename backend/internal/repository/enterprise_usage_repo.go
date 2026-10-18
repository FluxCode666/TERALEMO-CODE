package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Archived department rows remain selectable so historical usage keeps its leaf attribution.
const enterpriseDepartmentSubtreeSQL = `SELECT child.id FROM enterprise_departments child JOIN enterprise_departments root ON root.enterprise_id=child.enterprise_id WHERE root.id=$%d AND (child.id=root.id OR child.path LIKE (COALESCE(NULLIF(root.path,''),'/' || CAST(root.id AS TEXT) || '/') || '%%'))`

func appendEnterpriseUsageWhereConditions(conditions []string, args []any, filters UsageLogFilters, alias string) ([]string, []any) {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	if filters.EnterpriseID != 0 {
		conditions = append(conditions, fmt.Sprintf("%senterprise_id = $%d", prefix, len(args)+1))
		args = append(args, filters.EnterpriseID)
	}
	for _, id := range []int64{filters.EnterpriseDepartmentID, filters.EnterpriseDepartmentScopeID} {
		if id > 0 {
			conditions = append(conditions, fmt.Sprintf("%senterprise_department_id IN ("+enterpriseDepartmentSubtreeSQL+")", prefix, len(args)+1))
			args = append(args, id)
		}
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
	params = service.NormalizeEnterpriseUsageSort(params)
	query := `SELECT ul.id,
 ul.user_id,
 ul.api_key_id,
 COALESCE(ul.request_id,''),
 ul.service_tier,
 ul.inbound_endpoint,
 ul.group_id,
 ul.input_tokens,
 ul.output_tokens,
 ul.cache_creation_tokens,
 ul.cache_read_tokens,
 ul.cache_creation_5m_tokens,
 ul.cache_creation_1h_tokens,
 ul.input_cost,
 ul.output_cost,
 ul.cache_creation_cost,
 ul.cache_read_cost,
 ul.total_cost,
 ul.actual_cost,
 ul.rate_multiplier,
 ul.long_context_billing_applied,
 ul.billing_type,
 ul.stream,
 ul.openai_ws_mode,
 ul.native_compaction_v2,
 ul.duration_ms,
 ul.first_token_ms,
 ul.image_count,
 ul.image_size,
 ul.image_input_size,
 ul.image_output_size,
 ul.image_input_tokens,
 ul.image_input_cost,
 ul.image_output_tokens,
 ul.image_output_cost,
 ul.image_size_source,
 ul.image_size_breakdown,
 ul.user_agent,
 ul.ip_address,
 ul.cache_ttl_overridden,
 ul.billing_mode,
 ul.created_at,
 ul.model,
 COALESCE(NULLIF(TRIM(ul.requested_model),''),ul.model) AS requested_model,
 ul.reasoning_effort,
 ul.requested_reasoning_effort,
 ul.request_type,
 ul.enterprise_department_id,
 COALESCE(u.username,''),
 COALESCE(u.email,''),
 d.name,
 k.id AS api_key_resource_id,
 COALESCE(k.name,''),
 g.id AS group_resource_id,
 COALESCE(g.name,''),
 COALESCE(g.platform,'')
 FROM usage_logs ul LEFT JOIN users u ON u.id=ul.user_id LEFT JOIN api_keys k ON k.id=ul.api_key_id LEFT JOIN groups g ON g.id=ul.group_id
 LEFT JOIN enterprise_departments d ON d.id=ul.enterprise_department_id AND d.enterprise_id=ul.enterprise_id AND d.deleted_at IS NULL ` + where
	// Resolve sort columns against a derived table to reuse the personal usage ordering.
	query = "SELECT * FROM (" + query + ") enterprise_usage ORDER BY " + usageLogOrderBy(params) + fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	args = append(args, params.Limit(), params.Offset())
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.EnterpriseUsageLog, 0)
	for rows.Next() {
		var log service.UsageLog
		var requestType int16
		var imageSizeBreakdown sql.NullString
		var key, group sql.NullInt64
		var departmentName *string
		var username, email, keyName, groupName, platform string
		if err := rows.Scan(&log.ID,
			&log.UserID,
			&log.APIKeyID,
			&log.RequestID,
			&log.ServiceTier,
			&log.InboundEndpoint,
			&log.GroupID,
			&log.InputTokens,
			&log.OutputTokens,
			&log.CacheCreationTokens,
			&log.CacheReadTokens,
			&log.CacheCreation5mTokens,
			&log.CacheCreation1hTokens,
			&log.InputCost,
			&log.OutputCost,
			&log.CacheCreationCost,
			&log.CacheReadCost,
			&log.TotalCost,
			&log.ActualCost,
			&log.RateMultiplier,
			&log.LongContextBillingApplied,
			&log.BillingType,
			&log.Stream,
			&log.OpenAIWSMode,
			&log.NativeCompactionV2,
			&log.DurationMs,
			&log.FirstTokenMs,
			&log.ImageCount,
			&log.ImageSize,
			&log.ImageInputSize,
			&log.ImageOutputSize,
			&log.ImageInputTokens,
			&log.ImageInputCost,
			&log.ImageOutputTokens,
			&log.ImageOutputCost,
			&log.ImageSizeSource,
			&imageSizeBreakdown,
			&log.UserAgent,
			&log.IPAddress,
			&log.CacheTTLOverridden,
			&log.BillingMode,
			&log.CreatedAt,
			&log.Model,
			&log.RequestedModel,
			&log.ReasoningEffort,
			&log.RequestedReasoningEffort,
			&requestType,
			&log.EnterpriseDepartmentID,
			&username,
			&email,
			&departmentName,
			&key,
			&keyName,
			&group,
			&groupName,
			&platform); err != nil {
			return nil, nil, err
		}
		log.ImageSizeBreakdown = stringIntMapFromNullJSON(imageSizeBreakdown)
		log.RequestType = service.RequestTypeFromInt16(requestType)
		v := service.EnterpriseUsageLogFromUsageLog(&log)
		v.User.Username, v.User.Email = username, email
		v.DepartmentName = departmentName
		if key.Valid {
			v.APIKey = &service.EnterpriseUsageNamedResource{ID: key.Int64, Name: keyName}
		}
		if group.Valid {
			v.Group = &service.EnterpriseUsageGroup{ID: group.Int64, Name: groupName, Platform: platform}
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

func (r *usageLogRepository) EnterpriseUsageDepartmentExists(ctx context.Context, enterpriseID, departmentID int64) (bool, error) {
	var exists bool
	err := scanSingleRow(ctx, r.sql, `SELECT EXISTS (SELECT 1 FROM enterprise_departments WHERE enterprise_id=$1 AND id=$2 AND deleted_at IS NULL)`, []any{enterpriseID, departmentID}, &exists)
	return exists, err
}
