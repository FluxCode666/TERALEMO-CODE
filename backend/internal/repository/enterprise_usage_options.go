package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Key options reflect current membership; historical usage remains snapshot based.
// Select only display columns so plaintext keys never enter this query's result.
func (r *usageLogRepository) GetEnterpriseUsageAPIKeyOptions(ctx context.Context, filters UsageLogFilters, search string, limit int) ([]service.EnterpriseUsageAPIKeyOption, error) {
	if filters.EnterpriseID <= 0 {
		return nil, service.ErrEnterpriseNotFound
	}
	query := `SELECT k.id, k.name, u.id, COALESCE(u.username,''), COALESCE(u.email,'')
 FROM api_keys k
 JOIN users u ON u.id = k.user_id AND u.deleted_at IS NULL
 JOIN enterprise_members m ON m.user_id = u.id AND m.deleted_at IS NULL
 JOIN enterprises e ON e.id = m.enterprise_id AND e.deleted_at IS NULL
 WHERE m.enterprise_id = $1 AND k.deleted_at IS NULL`
	args := []any{filters.EnterpriseID}
	for _, id := range []int64{filters.EnterpriseDepartmentID, filters.EnterpriseDepartmentScopeID} {
		if id > 0 {
			query += fmt.Sprintf(" AND m.department_id IN ("+enterpriseDepartmentSubtreeSQL+")", len(args)+1)
			args = append(args, id)
		}
	}
	for _, v := range []struct {
		column string
		value  int64
	}{{"u.id", filters.UserID}, {"k.id", filters.APIKeyID}} {
		if v.value > 0 {
			query += fmt.Sprintf(" AND %s = $%d", v.column, len(args)+1)
			args = append(args, v.value)
		}
	}
	if search = strings.TrimSpace(search); search != "" {
		query += fmt.Sprintf(" AND (k.name ILIKE $%[1]d OR u.username ILIKE $%[1]d OR u.email ILIKE $%[1]d)", len(args)+1)
		args = append(args, "%"+escapeLikePattern(search)+"%")
	}
	query += fmt.Sprintf(" ORDER BY k.id DESC LIMIT $%d", len(args)+1)
	args = append(args, service.NormalizeEnterpriseUsageOptionsLimit(limit))
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.EnterpriseUsageAPIKeyOption, 0)
	for rows.Next() {
		var v service.EnterpriseUsageAPIKeyOption
		if err := rows.Scan(&v.ID, &v.Name, &v.User.ID, &v.User.Username, &v.User.Email); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *usageLogRepository) GetEnterpriseUsageModelOptions(ctx context.Context, filters UsageLogFilters, search string, limit int) ([]service.EnterpriseUsageModelOption, error) {
	where, args, err := enterpriseUsageWhere(filters)
	if err != nil {
		return nil, err
	}
	const modelExpr = "COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model)"
	query := "SELECT " + modelExpr + " AS model, COUNT(*) AS requests FROM usage_logs ul " + where + " AND " + modelExpr + " <> ''"
	if search = strings.TrimSpace(search); search != "" {
		query += fmt.Sprintf(" AND %s ILIKE $%d", modelExpr, len(args)+1)
		args = append(args, "%"+escapeLikePattern(search)+"%")
	}
	query += " GROUP BY " + modelExpr + fmt.Sprintf(" ORDER BY requests DESC, model ASC LIMIT $%d", len(args)+1)
	args = append(args, service.NormalizeEnterpriseUsageOptionsLimit(limit))
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.EnterpriseUsageModelOption, 0)
	for rows.Next() {
		var v service.EnterpriseUsageModelOption
		if err := rows.Scan(&v.Model, &v.Requests); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

var _ service.EnterpriseUsageOptionsRepository = (*usageLogRepository)(nil)
