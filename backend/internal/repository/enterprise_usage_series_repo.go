package repository

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// GetEnterpriseUsageSeries reads immutable enterprise snapshots directly from
// usage_logs. Top entities are chosen once over the entire filtered interval;
// the remaining entities are summed into one series per bucket.
func (r *usageLogRepository) GetEnterpriseUsageSeries(ctx context.Context, filters UsageLogFilters, dimension, granularity string, top int) ([]service.EnterpriseUsageSeriesPoint, error) {
	where, args, err := enterpriseUsageWhere(filters)
	if err != nil {
		return nil, err
	}
	var entity, label, joins string
	switch dimension {
	case "models":
		entity = "COALESCE(NULLIF(TRIM(ul.requested_model),''),ul.model)"
		label = entity
	case "members":
		entity = "CAST(ul.user_id AS TEXT)"
		label = "COALESCE(u.username,'')"
		joins = " LEFT JOIN users u ON u.id=ul.user_id "
	case "departments":
		entity = "CAST(COALESCE(ul.enterprise_department_id,0) AS TEXT)"
		label = `COALESCE((SELECT string_agg(ancestor.name,'/' ORDER BY ancestor.depth)
   FROM enterprise_departments ancestor WHERE ancestor.enterprise_id=ul.enterprise_id
   AND ancestor.id=ANY(string_to_array(trim(d.path,'/'),'/')::bigint[])),d.name,'')`
		joins = " LEFT JOIN enterprise_departments d ON d.id=ul.enterprise_department_id AND d.enterprise_id=ul.enterprise_id "
	default:
		return nil, service.ErrEnterpriseInvalidInput
	}
	if granularity != "day" && granularity != "hour" {
		return nil, service.ErrEnterpriseInvalidInput
	}
	if top <= 0 {
		top = 8
	}
	if top > 20 {
		top = 20
	}
	query := fmt.Sprintf(`WITH buckets AS (
 SELECT TO_CHAR(ul.created_at,'%s') AS date, %s AS entity, %s AS label,
 COUNT(*) AS requests,
 COALESCE(SUM(ul.input_tokens+ul.output_tokens+ul.cache_creation_tokens+ul.cache_read_tokens),0) AS total_tokens,
 COALESCE(SUM(ul.actual_cost),0) AS actual_cost
 FROM usage_logs ul %s %s GROUP BY 1,2,3
 ), top_entities AS (
 SELECT entity FROM buckets GROUP BY entity ORDER BY SUM(actual_cost) DESC,entity ASC LIMIT $%d
 ), folded AS (
 SELECT date,CASE WHEN entity IN (SELECT entity FROM top_entities) THEN entity ELSE '__other__' END AS entity,
 CASE WHEN entity IN (SELECT entity FROM top_entities) THEN label ELSE '__other__' END AS label,
 requests,total_tokens,actual_cost FROM buckets
 )
 SELECT date,entity,label,SUM(requests),SUM(total_tokens),SUM(actual_cost)
 FROM folded GROUP BY date,entity,label ORDER BY date,entity,label`, safeDateFormat(granularity), entity, label, joins, where, len(args)+1)
	args = append(args, top)
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	out := []service.EnterpriseUsageSeriesPoint{}
	for rows.Next() {
		var point service.EnterpriseUsageSeriesPoint
		var key, name string
		if err := rows.Scan(&point.Date, &key, &name, &point.Requests, &point.TotalTokens, &point.ActualCost); err != nil {
			return nil, err
		}
		if dimension == "models" {
			point.Model = &key
		} else {
			var id int64
			if key != "__other__" {
				id, err = strconv.ParseInt(key, 10, 64)
				if err != nil {
					return nil, err
				}
			}
			if dimension == "members" {
				point.UserID, point.Username = &id, &name
			} else {
				point.DepartmentID, point.DepartmentName = &id, &name
			}
		}
		out = append(out, point)
	}
	return out, rows.Err()
}

var _ service.EnterpriseUsageSeriesRepository = (*usageLogRepository)(nil)
