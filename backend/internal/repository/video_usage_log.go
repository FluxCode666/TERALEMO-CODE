package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Keep this order aligned with prepareUsageLogInsert. Unlike the ordinary
// best-effort path, video settlement requires a confirmed, retryable write.
var settledVideoUsageColumns = [...]string{
	"user_id",
	"api_key_id",
	"account_id",
	"request_id",
	"model",
	"requested_model",
	"upstream_model",
	"upstream_response_model",
	"upstream_model_mismatch",
	"group_id",
	"subscription_id",
	"input_tokens",
	"output_tokens",
	"cache_creation_tokens",
	"cache_read_tokens",
	"cache_creation_5m_tokens",
	"cache_creation_1h_tokens",
	"image_output_tokens",
	"image_output_cost",
	"image_input_tokens",
	"image_input_cost",
	"input_cost",
	"output_cost",
	"cache_creation_cost",
	"cache_read_cost",
	"total_cost",
	"actual_cost",
	"rate_multiplier",
	"account_rate_multiplier",
	"billing_type",
	"request_type",
	"stream",
	"openai_ws_mode",
	"duration_ms",
	"first_token_ms",
	"user_agent",
	"ip_address",
	"image_count",
	"image_size",
	"image_input_size",
	"image_output_size",
	"image_size_source",
	"image_size_breakdown",
	"video_count",
	"video_resolution",
	"video_duration_seconds",
	"service_tier",
	"reasoning_effort",
	"requested_reasoning_effort",
	"inbound_endpoint",
	"upstream_endpoint",
	"cache_ttl_overridden",
	"long_context_billing_applied",
	"channel_id",
	"model_mapping_chain",
	"billing_tier",
	"billing_mode",
	"account_stats_cost",
	"upstream_request_id",
	"session_id",
	"native_compaction_v2",
	"enterprise_id",
	"enterprise_department_id",
	"created_at",
}

// UpsertSettledVideoUsage repairs an old zero-cost placeholder only after the
// billing transaction succeeded. A positive-cost row is final and cannot be
// overwritten by a retry. The ordinary usage-log insertion remains append-only.
func (r *usageLogRepository) UpsertSettledVideoUsage(ctx context.Context, usageLog *service.UsageLog) error {
	if usageLog == nil || strings.TrimSpace(usageLog.RequestID) == "" || usageLog.APIKeyID <= 0 ||
		usageLog.ActualCost < 0 || math.IsNaN(usageLog.ActualCost) || math.IsInf(usageLog.ActualCost, 0) {
		return errors.New("invalid settled video usage")
	}
	if r == nil || r.sql == nil {
		return errors.New("usage log repository is unavailable")
	}
	prepared := prepareUsageLogInsert(usageLog)
	if len(prepared.args) != len(settledVideoUsageColumns) {
		return errors.New("settled video usage column count mismatch")
	}
	sqlq := r.sql
	if tx := dbent.TxFromContext(ctx); tx != nil {
		sqlq = tx.Client()
	}
	query := buildSettledVideoUsageQuery()
	err := scanSingleRow(ctx, sqlq, query, prepared.args, &usageLog.ID, &usageLog.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		// ON CONFLICT does not RETURN a row when its WHERE guard protects a
		// previously settled positive charge. Confirm that row exists rather
		// than treating an unconfirmed insert as success.
		var actualCost float64
		err = scanSingleRow(ctx, sqlq,
			"SELECT id, created_at, actual_cost FROM usage_logs WHERE request_id = $1 AND api_key_id = $2",
			[]any{prepared.requestID, usageLog.APIKeyID}, &usageLog.ID, &usageLog.CreatedAt, &actualCost)
		if err == nil && actualCost <= 0 {
			return errors.New("settled video usage was not persisted")
		}
	}
	return err
}

func buildSettledVideoUsageQuery() string {
	placeholders := make([]string, len(settledVideoUsageColumns))
	updates := make([]string, 0, len(settledVideoUsageColumns))
	for i, column := range settledVideoUsageColumns {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		switch column {
		case "request_id", "api_key_id", "created_at":
			// Stable identity and the first observation timestamp are retained.
		default:
			updates = append(updates, column+" = EXCLUDED."+column)
		}
	}
	return "INSERT INTO usage_logs (" + strings.Join(settledVideoUsageColumns[:], ", ") +
		") VALUES (" + strings.Join(placeholders, ", ") +
		") ON CONFLICT (request_id, api_key_id) DO UPDATE SET " + strings.Join(updates, ", ") +
		" WHERE usage_logs.actual_cost = 0 RETURNING id, created_at"
}
