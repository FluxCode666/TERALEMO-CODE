package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type enterpriseAuditLogRepository struct{ client *dbent.Client }

func NewEnterpriseAuditLogRepository(client *dbent.Client) service.EnterpriseAuditLogRepository {
	return &enterpriseAuditLogRepository{client: client}
}

func (r *enterpriseAuditLogRepository) Create(ctx context.Context, log *service.EnterpriseAuditLog) error {
	if log == nil {
		return fmt.Errorf("enterprise audit log is required")
	}
	detail := log.Detail
	if detail == nil {
		detail = map[string]any{}
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("encode enterprise audit detail: %w", err)
	}
	// Do not require an active enterprise here: enterprise.delete is audited in
	// the same transaction after the enterprise has been soft deleted.
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, `INSERT INTO enterprise_audit_logs
 (enterprise_id, actor_user_id, actor_role, action, target_type, target_id, detail)
 VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb) RETURNING id, created_at`,
		log.EnterpriseID, log.ActorUserID, log.ActorRole, log.Action, log.TargetType, log.TargetID, string(encoded))
	if err != nil {
		return translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return translatePersistenceError(err, nil, nil)
		}
		return translatePersistenceError(sql.ErrNoRows, service.ErrEnterpriseNotFound, nil)
	}
	if err := rows.Scan(&log.ID, &log.CreatedAt); err != nil {
		return translatePersistenceError(err, nil, nil)
	}
	log.Detail = detail
	return nil
}

func (r *enterpriseAuditLogRepository) List(ctx context.Context, enterpriseID int64, params pagination.PaginationParams, action string) ([]service.EnterpriseAuditLog, *pagination.PaginationResult, error) {
	client := clientFromContext(ctx, r.client)
	total, err := enterpriseAuditLogCount(ctx, client, enterpriseID, action)
	if err != nil {
		return nil, nil, err
	}
	order := params.NormalizedSortOrder(pagination.SortOrderDesc)
	rows, err := client.QueryContext(ctx, `SELECT l.id, l.enterprise_id, l.actor_user_id, l.actor_role,
 COALESCE(u.email, ''), l.action, l.target_type, l.target_id, l.detail, l.created_at
 FROM enterprise_audit_logs l LEFT JOIN users u ON u.id = l.actor_user_id
 WHERE l.enterprise_id = $1 AND ($2 = '' OR l.action = $2)
 ORDER BY l.created_at `+order+`, l.id `+order+` LIMIT $3 OFFSET $4`, enterpriseID, action, params.Limit(), params.Offset())
	if err != nil {
		return nil, nil, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.EnterpriseAuditLog, 0)
	for rows.Next() {
		var log service.EnterpriseAuditLog
		var detail []byte
		if err := rows.Scan(&log.ID, &log.EnterpriseID, &log.ActorUserID, &log.ActorRole, &log.ActorEmail,
			&log.Action, &log.TargetType, &log.TargetID, &detail, &log.CreatedAt); err != nil {
			return nil, nil, translatePersistenceError(err, nil, nil)
		}
		if err := json.Unmarshal(detail, &log.Detail); err != nil {
			return nil, nil, fmt.Errorf("decode enterprise audit detail: %w", err)
		}
		items = append(items, log)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, translatePersistenceError(err, nil, nil)
	}
	return items, paginationResultFromTotal(total, params), nil
}

func enterpriseAuditLogCount(ctx context.Context, client *dbent.Client, enterpriseID int64, action string) (int64, error) {
	rows, err := client.QueryContext(ctx, `SELECT COUNT(*) FROM enterprise_audit_logs WHERE enterprise_id = $1 AND ($2 = '' OR action = $2)`, enterpriseID, action)
	if err != nil {
		return 0, translatePersistenceError(err, nil, nil)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, translatePersistenceError(rows.Err(), nil, nil)
	}
	var total int64
	if err := rows.Scan(&total); err != nil {
		return 0, translatePersistenceError(err, nil, nil)
	}
	return total, nil
}
