package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const scheduledQuestionTestPlanColumns = `p.id, p.name, p.account_ids, p.question_ids, p.model_id,
	p.cron_expression, p.enabled, p.min_pass_ratio, p.cooldown_minutes, p.notify_enabled,
	p.last_run_at, p.next_run_at, p.created_at, p.updated_at`

const scheduledQuestionTestLatestColumns = `COALESCE(latest.status, ''), COALESCE(latest.passed_accounts, 0),
	COALESCE(latest.total_accounts, 0), COALESCE(latest.alerted_accounts, 0)`

const scheduledQuestionTestLatestJoin = `LEFT JOIN LATERAL (
	SELECT status, passed_accounts, total_accounts, alerted_accounts
	FROM scheduled_question_test_runs WHERE plan_id = p.id
	ORDER BY started_at DESC, id DESC LIMIT 1
) latest ON true`

const scheduledQuestionTestRunColumns = `id, plan_id, status, started_at, finished_at,
	total_accounts, passed_accounts, alerted_accounts, notifications_suppressed, error_message, summary, created_at`

type scheduledQuestionTestRepository struct {
	db *sql.DB
}

func NewScheduledQuestionTestRepository(db *sql.DB) service.ScheduledQuestionTestRepository {
	return &scheduledQuestionTestRepository{db: db}
}

func (r *scheduledQuestionTestRepository) Create(ctx context.Context, plan *service.ScheduledQuestionTestPlan) (*service.ScheduledQuestionTestPlan, error) {
	accountIDs, err := json.Marshal(plan.AccountIDs)
	if err != nil {
		return nil, err
	}
	questionIDs, err := json.Marshal(plan.QuestionIDs)
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO scheduled_question_test_plans AS p
		(name, account_ids, question_ids, model_id, cron_expression, enabled, min_pass_ratio, cooldown_minutes, notify_enabled, next_run_at, created_at, updated_at)
		VALUES ($1, $2::jsonb, $3::jsonb, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
		RETURNING `+scheduledQuestionTestPlanColumns+`, '' AS last_status, 0 AS passed_accounts, 0 AS total_accounts, 0 AS alerted_accounts`,
		plan.Name, string(accountIDs), string(questionIDs), plan.ModelID, plan.CronExpression,
		plan.Enabled, plan.MinPassRatio, plan.CooldownMinutes, plan.NotifyEnabled, plan.NextRunAt)
	return scanScheduledQuestionTestPlan(row)
}

func (r *scheduledQuestionTestRepository) GetByID(ctx context.Context, id int64) (*service.ScheduledQuestionTestPlan, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+scheduledQuestionTestPlanColumns+`, `+scheduledQuestionTestLatestColumns+`
		FROM scheduled_question_test_plans p `+scheduledQuestionTestLatestJoin+` WHERE p.id = $1`, id)
	return scanScheduledQuestionTestPlan(row)
}

func (r *scheduledQuestionTestRepository) List(ctx context.Context) ([]*service.ScheduledQuestionTestPlan, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+scheduledQuestionTestPlanColumns+`, `+scheduledQuestionTestLatestColumns+`
		FROM scheduled_question_test_plans p `+scheduledQuestionTestLatestJoin+` ORDER BY p.id DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanScheduledQuestionTestPlans(rows)
}

func (r *scheduledQuestionTestRepository) ListDue(ctx context.Context, now time.Time) ([]*service.ScheduledQuestionTestPlan, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+scheduledQuestionTestPlanColumns+`, `+scheduledQuestionTestLatestColumns+`
		FROM scheduled_question_test_plans p `+scheduledQuestionTestLatestJoin+`
		WHERE p.enabled = true AND (p.next_run_at IS NULL OR p.next_run_at <= $1)
		ORDER BY p.next_run_at ASC NULLS FIRST, p.id ASC`, now)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanScheduledQuestionTestPlans(rows)
}

func (r *scheduledQuestionTestRepository) Update(ctx context.Context, plan *service.ScheduledQuestionTestPlan, fields service.ScheduledQuestionTestPlanUpdateMask) (*service.ScheduledQuestionTestPlan, error) {
	assignments := []string{"updated_at = NOW()"}
	args := []any{plan.ID}
	set := func(field string, value any, cast string) {
		args = append(args, value)
		assignments = append(assignments, fmt.Sprintf("%s = $%d%s", field, len(args), cast))
	}
	if fields.Name {
		set("name", plan.Name, "")
	}
	if fields.AccountIDs {
		data, err := json.Marshal(plan.AccountIDs)
		if err != nil {
			return nil, err
		}
		set("account_ids", string(data), "::jsonb")
	}
	if fields.QuestionIDs {
		data, err := json.Marshal(plan.QuestionIDs)
		if err != nil {
			return nil, err
		}
		set("question_ids", string(data), "::jsonb")
	}
	if fields.ModelID {
		set("model_id", plan.ModelID, "")
	}
	if fields.CronExpression {
		set("cron_expression", plan.CronExpression, "")
	}
	if fields.Enabled {
		set("enabled", plan.Enabled, "")
	}
	if fields.MinPassRatio {
		set("min_pass_ratio", plan.MinPassRatio, "")
	}
	if fields.CooldownMinutes {
		set("cooldown_minutes", plan.CooldownMinutes, "")
	}
	if fields.NotifyEnabled {
		set("notify_enabled", plan.NotifyEnabled, "")
	}
	if fields.NextRunAt {
		if fields.CronExpression {
			set("next_run_at", plan.NextRunAt, "")
		} else {
			// Enabling a stale snapshot must not replace a concurrently edited cron's schedule.
			args = append(args, plan.CronExpression, plan.NextRunAt)
			assignments = append(assignments, fmt.Sprintf("next_run_at = CASE WHEN cron_expression = $%d THEN $%d ELSE next_run_at END", len(args)-1, len(args)))
		}
	}
	row := r.db.QueryRowContext(ctx, `WITH updated AS (
		UPDATE scheduled_question_test_plans SET `+strings.Join(assignments, ", ")+` WHERE id = $1 RETURNING *
	) SELECT `+scheduledQuestionTestPlanColumns+`, `+scheduledQuestionTestLatestColumns+`
		FROM updated p `+scheduledQuestionTestLatestJoin, args...)
	return scanScheduledQuestionTestPlan(row)
}

func (r *scheduledQuestionTestRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM scheduled_question_test_plans WHERE id = $1`, id)
	if err != nil {
		return err
	}
	return checkScheduledQuestionTestAffected(result)
}

func (r *scheduledQuestionTestRepository) ListRuns(ctx context.Context, planID int64, limit int) ([]*service.ScheduledQuestionTestRun, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+scheduledQuestionTestRunColumns+`
		FROM scheduled_question_test_runs WHERE plan_id = $1 ORDER BY started_at DESC, id DESC LIMIT $2`, planID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	runs := make([]*service.ScheduledQuestionTestRun, 0)
	for rows.Next() {
		run, err := scanScheduledQuestionTestRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (r *scheduledQuestionTestRepository) ListStates(ctx context.Context, planID int64) ([]*service.ScheduledQuestionTestAccountState, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, plan_id, account_id, state, last_alert_at, updated_at
		FROM scheduled_question_test_account_states WHERE plan_id = $1 ORDER BY account_id ASC`, planID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	states := make([]*service.ScheduledQuestionTestAccountState, 0)
	for rows.Next() {
		state := &service.ScheduledQuestionTestAccountState{}
		if err := rows.Scan(&state.ID, &state.PlanID, &state.AccountID, &state.State, &state.LastAlertAt, &state.UpdatedAt); err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, rows.Err()
}

func (r *scheduledQuestionTestRepository) FinishRun(ctx context.Context, run *service.ScheduledQuestionTestRun, states []*service.ScheduledQuestionTestAccountState, nextRunAt time.Time) (*service.ScheduledQuestionTestRun, error) {
	summary := run.Summary
	if summary == nil {
		summary = []service.ScheduledQuestionTestAccountSummary{}
	}
	data, err := json.Marshal(summary)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Serialize persistence with plan deletion/edits, not with the upstream calls.
	var planID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM scheduled_question_test_plans WHERE id = $1 FOR UPDATE`, run.PlanID).Scan(&planID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrScheduledQuestionTestNotFound
		}
		return nil, err
	}
	row := tx.QueryRowContext(ctx, `INSERT INTO scheduled_question_test_runs
		(plan_id, status, started_at, finished_at, total_accounts, passed_accounts, alerted_accounts, notifications_suppressed, error_message, summary, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, NOW()) RETURNING `+scheduledQuestionTestRunColumns,
		run.PlanID, run.Status, run.StartedAt, run.FinishedAt, run.TotalAccounts,
		run.PassedAccounts, run.AlertedAccounts, run.NotificationsSuppressed, run.ErrorMessage, string(data))
	created, err := scanScheduledQuestionTestRun(row)
	if err != nil {
		return nil, err
	}
	for _, state := range states {
		result, err := tx.ExecContext(ctx, `INSERT INTO scheduled_question_test_account_states
			(plan_id, account_id, state, last_alert_at, updated_at) VALUES ($1, $2, $3, $4, NOW())
			ON CONFLICT (plan_id, account_id) DO UPDATE SET
			state = EXCLUDED.state, last_alert_at = EXCLUDED.last_alert_at, updated_at = NOW()`,
			run.PlanID, state.AccountID, state.State, state.LastAlertAt)
		if err != nil {
			return nil, err
		}
		if err := checkScheduledQuestionTestAffected(result); err != nil {
			return nil, err
		}
	}
	// An edit while the round was running owns the new schedule; never overwrite it.
	result, err := tx.ExecContext(ctx, `UPDATE scheduled_question_test_plans
		SET last_run_at = $2,
			next_run_at = CASE WHEN enabled = true AND cron_expression = $4 AND updated_at = $5 THEN $3 ELSE next_run_at END,
			updated_at = NOW()
		WHERE id = $1`, run.PlanID, run.FinishedAt, nextRunAt, run.CronExpression, run.PlanUpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := checkScheduledQuestionTestAffected(result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

func checkScheduledQuestionTestAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrScheduledQuestionTestNotFound
	}
	return nil
}

func scanScheduledQuestionTestPlan(row scannable) (*service.ScheduledQuestionTestPlan, error) {
	plan := &service.ScheduledQuestionTestPlan{}
	var accountIDs, questionIDs []byte
	if err := row.Scan(&plan.ID, &plan.Name, &accountIDs, &questionIDs, &plan.ModelID,
		&plan.CronExpression, &plan.Enabled, &plan.MinPassRatio, &plan.CooldownMinutes, &plan.NotifyEnabled,
		&plan.LastRunAt, &plan.NextRunAt, &plan.CreatedAt, &plan.UpdatedAt,
		&plan.LastStatus, &plan.PassedAccounts, &plan.TotalAccounts, &plan.AlertedAccounts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrScheduledQuestionTestNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal(accountIDs, &plan.AccountIDs); err != nil {
		return nil, fmt.Errorf("decode scheduled question test account ids: %w", err)
	}
	if err := json.Unmarshal(questionIDs, &plan.QuestionIDs); err != nil {
		return nil, fmt.Errorf("decode scheduled question test question ids: %w", err)
	}
	return plan, nil
}

func scanScheduledQuestionTestPlans(rows *sql.Rows) ([]*service.ScheduledQuestionTestPlan, error) {
	plans := make([]*service.ScheduledQuestionTestPlan, 0)
	for rows.Next() {
		plan, err := scanScheduledQuestionTestPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}

func scanScheduledQuestionTestRun(row scannable) (*service.ScheduledQuestionTestRun, error) {
	run := &service.ScheduledQuestionTestRun{}
	var summary []byte
	if err := row.Scan(&run.ID, &run.PlanID, &run.Status, &run.StartedAt, &run.FinishedAt,
		&run.TotalAccounts, &run.PassedAccounts, &run.AlertedAccounts, &run.NotificationsSuppressed, &run.ErrorMessage, &summary, &run.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(summary, &run.Summary); err != nil {
		return nil, fmt.Errorf("decode scheduled question test summary: %w", err)
	}
	if run.Summary == nil {
		run.Summary = []service.ScheduledQuestionTestAccountSummary{}
	}
	return run, nil
}
