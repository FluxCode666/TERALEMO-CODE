//go:build unit

package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

var scheduledQuestionTestPlanTestColumns = []string{
	"id", "name", "account_ids", "question_ids", "model_id", "cron_expression", "enabled",
	"min_pass_ratio", "cooldown_minutes", "notify_enabled", "last_run_at", "next_run_at",
	"created_at", "updated_at", "report_enabled", "email_enabled", "feishu_enabled", "feishu_webhook_url",
	"feishu_secret", "auto_pause_enabled", "auto_resume_enabled", "last_status", "passed_accounts", "total_accounts", "alerted_accounts", "skipped_accounts",
}

var scheduledQuestionTestRunTestColumns = []string{
	"id", "plan_id", "status", "started_at", "finished_at", "total_accounts", "passed_accounts",
	"alerted_accounts", "notifications_suppressed", "error_message", "summary", "created_at",
	"paused_accounts", "resumed_accounts", "auto_pause_skipped", "notification_results", "skipped_accounts",
}

var scheduledQuestionTestStateTestColumns = []string{"id", "plan_id", "account_id", "state", "last_alert_at", "updated_at", "auto_paused", "auto_paused_at"}

func newScheduledQuestionTestTestRepository(t *testing.T) (service.ScheduledQuestionTestRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
		require.NoError(t, mock.ExpectationsWereMet())
	})
	return NewScheduledQuestionTestRepository(db), mock
}

func scheduledQuestionTestPlanTestValues(now time.Time, nextRunAt driver.Value) []driver.Value {
	return []driver.Value{
		int64(7), "plan", []byte(`[3,1]`), []byte(`[4,2]`), "model", "*/30 * * * *", true,
		80, 360, true, nil, nextRunAt, now, now, false, true, false, "", "", false, false, "success", 1, 2, 1, 1,
	}
}

func scheduledQuestionTestRunTestValues(now time.Time, summary string) []driver.Value {
	return []driver.Value{int64(9), int64(7), "success", now, now, 2, 1, 1, false, "", []byte(summary), now, 0, 0, false, []byte("[]"), 0}
}

func TestScheduledQuestionTestRepositoryCreate(t *testing.T) {
	repo, mock := newScheduledQuestionTestTestRepository(t)
	now := time.Now().UTC()
	next := now.Add(time.Hour)
	plan := &service.ScheduledQuestionTestPlan{
		Name: "plan", AccountIDs: []int64{3, 1}, QuestionIDs: []int64{4, 2}, ModelID: "model",
		CronExpression: "*/30 * * * *", Enabled: true, MinPassRatio: 80, CooldownMinutes: 360, NotifyEnabled: true, NextRunAt: &next,
		ReportEnabled: true, EmailEnabled: false, FeishuEnabled: true,
		FeishuWebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/abcd1234", FeishuSecret: "test-secret",
		AutoPauseEnabled: true, AutoResumeEnabled: true,
	}
	planValues := scheduledQuestionTestPlanTestValues(now, next)
	planValues[14], planValues[15], planValues[16] = true, false, true
	planValues[17], planValues[18], planValues[19], planValues[20] = "https://open.feishu.cn/open-apis/bot/v2/hook/abcd1234", "test-secret", true, true
	mock.ExpectQuery(`INSERT INTO scheduled_question_test_plans AS p .* RETURNING`).
		WithArgs("plan", "[3,1]", "[4,2]", "model", "*/30 * * * *", true, 80, 360, true, true, false, true,
			"https://open.feishu.cn/open-apis/bot/v2/hook/abcd1234", "test-secret", true, true, next).
		WillReturnRows(sqlmock.NewRows(scheduledQuestionTestPlanTestColumns).AddRow(planValues...))
	created, err := repo.Create(context.Background(), plan)
	require.NoError(t, err)
	require.Equal(t, int64(7), created.ID)
	require.Equal(t, plan.AccountIDs, created.AccountIDs)
	require.Equal(t, plan.QuestionIDs, created.QuestionIDs)
	require.Equal(t, plan.CronExpression, created.CronExpression)
	require.Equal(t, &next, created.NextRunAt)
	require.Nil(t, created.LastRunAt)
	require.True(t, created.ReportEnabled)
	require.False(t, created.EmailEnabled)
	require.True(t, created.FeishuEnabled)
	require.True(t, created.FeishuWebhookConfigured)
	require.True(t, created.FeishuSecretConfigured)
	require.True(t, created.AutoPauseEnabled)
	require.True(t, created.AutoResumeEnabled)
	encoded, err := json.Marshal(created)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "abcd1234")
	require.NotContains(t, string(encoded), "test-secret")
}

func TestScheduledQuestionTestRepositoryGetAndListLatestRun(t *testing.T) {
	repo, mock := newScheduledQuestionTestTestRepository(t)
	now := time.Now().UTC()
	query := `SELECT .* FROM scheduled_question_test_plans p LEFT JOIN LATERAL \( SELECT status, passed_accounts, total_accounts, alerted_accounts, skipped_accounts FROM scheduled_question_test_runs WHERE plan_id = p.id ORDER BY started_at DESC, id DESC LIMIT 1 \) latest ON true`
	mock.ExpectQuery(query + ` WHERE p.id = \$1`).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows(scheduledQuestionTestPlanTestColumns).AddRow(scheduledQuestionTestPlanTestValues(now, nil)...)).RowsWillBeClosed()
	plan, err := repo.GetByID(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, "success", plan.LastStatus)
	require.Equal(t, 1, plan.PassedAccounts)
	require.Equal(t, 2, plan.TotalAccounts)
	require.Equal(t, 1, plan.AlertedAccounts)
	require.Equal(t, 1, plan.SkippedAccounts)

	mock.ExpectQuery(query + ` ORDER BY p.id DESC`).
		WillReturnRows(sqlmock.NewRows(scheduledQuestionTestPlanTestColumns).AddRow(scheduledQuestionTestPlanTestValues(now, nil)...)).RowsWillBeClosed()
	plans, err := repo.List(context.Background())
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Equal(t, plan, plans[0])

	mock.ExpectQuery(query + ` ORDER BY p.id DESC`).WillReturnRows(sqlmock.NewRows(scheduledQuestionTestPlanTestColumns)).RowsWillBeClosed()
	plans, err = repo.List(context.Background())
	require.NoError(t, err)
	require.Equal(t, []*service.ScheduledQuestionTestPlan{}, plans)
}

func TestScheduledQuestionTestRepositoryDueIncludesNullSchedule(t *testing.T) {
	repo, mock := newScheduledQuestionTestTestRepository(t)
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	nullNext := scheduledQuestionTestPlanTestValues(now, nil)
	pastNext := scheduledQuestionTestPlanTestValues(now, past)
	pastNext[0] = int64(8)
	mock.ExpectQuery(`WHERE p.enabled = true AND \(p.next_run_at IS NULL OR p.next_run_at <= \$1\) ORDER BY p.next_run_at ASC NULLS FIRST, p.id ASC`).
		WithArgs(now).WillReturnRows(sqlmock.NewRows(scheduledQuestionTestPlanTestColumns).AddRow(nullNext...).AddRow(pastNext...)).RowsWillBeClosed()
	plans, err := repo.ListDue(context.Background(), now)
	require.NoError(t, err)
	require.Len(t, plans, 2)
	require.Nil(t, plans[0].NextRunAt)
	require.Equal(t, &past, plans[1].NextRunAt)
}

func TestScheduledQuestionTestRepositoryUpdateMask(t *testing.T) {
	repo, mock := newScheduledQuestionTestTestRepository(t)
	now := time.Now().UTC()
	stale := &service.ScheduledQuestionTestPlan{ID: 7, Name: "stale", AccountIDs: []int64{99}, Enabled: false, FeishuWebhookURL: "stale", FeishuSecret: "stale"}
	values := scheduledQuestionTestPlanTestValues(now, nil)
	values[1], values[6] = "concurrent name", false
	values[17], values[18] = "https://open.feishu.cn/open-apis/bot/v2/hook/current-token", "current-secret"
	query := `WITH updated AS (
		UPDATE scheduled_question_test_plans SET updated_at = NOW(), enabled = $2 WHERE id = $1 RETURNING *
	) SELECT ` + scheduledQuestionTestPlanColumns + `, ` + scheduledQuestionTestLatestColumns + `
		FROM updated p ` + scheduledQuestionTestLatestJoin
	mock.ExpectQuery("^"+regexp.QuoteMeta(query)+"$").WithArgs(int64(7), false).
		WillReturnRows(sqlmock.NewRows(scheduledQuestionTestPlanTestColumns).AddRow(values...))
	updated, err := repo.Update(context.Background(), stale, service.ScheduledQuestionTestPlanUpdateMask{Enabled: true})
	require.NoError(t, err)
	require.False(t, updated.Enabled)
	require.Equal(t, "concurrent name", updated.Name)
	require.Equal(t, []int64{3, 1}, updated.AccountIDs)
	require.Equal(t, "https://open.feishu.cn/open-apis/bot/v2/hook/current-token", updated.FeishuWebhookURL)
	require.Equal(t, "current-secret", updated.FeishuSecret)
}

func TestScheduledQuestionTestRepositoryNotificationProjection(t *testing.T) {
	for _, method := range []string{"get", "list", "due", "update"} {
		t.Run(method, func(t *testing.T) {
			repo, mock := newScheduledQuestionTestTestRepository(t)
			now := time.Now().UTC()
			values := scheduledQuestionTestPlanTestValues(now, nil)
			values[16], values[17], values[18] = true, "https://open.feishu.cn/open-apis/bot/v2/hook/test-token-abcd1234", "secret-value"
			mock.ExpectQuery(`.*`).WillReturnRows(sqlmock.NewRows(scheduledQuestionTestPlanTestColumns).AddRow(values...)).RowsWillBeClosed()
			var plan *service.ScheduledQuestionTestPlan
			var err error
			switch method {
			case "get":
				plan, err = repo.GetByID(context.Background(), 7)
			case "update":
				plan, err = repo.Update(context.Background(), &service.ScheduledQuestionTestPlan{ID: 7}, service.ScheduledQuestionTestPlanUpdateMask{})
			default:
				var plans []*service.ScheduledQuestionTestPlan
				if method == "list" {
					plans, err = repo.List(context.Background())
				} else {
					plans, err = repo.ListDue(context.Background(), now)
				}
				require.NoError(t, err)
				require.Len(t, plans, 1)
				plan = plans[0]
			}
			require.NoError(t, err)
			require.True(t, plan.FeishuWebhookConfigured)
			require.True(t, plan.FeishuSecretConfigured)
			require.Contains(t, plan.FeishuWebhookHint, "****1234")
			encoded, err := json.Marshal(plan)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "test-token-abcd1234")
			require.NotContains(t, string(encoded), "secret-value")
			require.NotContains(t, string(encoded), "https://open.feishu.cn")
		})
	}
}

func TestScheduledQuestionTestRepositoryEnablePreservesConcurrentCronSchedule(t *testing.T) {
	repo, mock := newScheduledQuestionTestTestRepository(t)
	now := time.Now().UTC()
	next := now.Add(24 * time.Hour)
	concurrentNext := now.Add(time.Hour)
	stale := &service.ScheduledQuestionTestPlan{ID: 7, Enabled: true, CronExpression: "0 0 * * *", NextRunAt: &next}
	values := scheduledQuestionTestPlanTestValues(now, concurrentNext)
	values[5] = "0 * * * *"
	query := `WITH updated AS (
		UPDATE scheduled_question_test_plans SET updated_at = NOW(), enabled = $2, next_run_at = CASE WHEN cron_expression = $3 THEN $4 ELSE next_run_at END WHERE id = $1 RETURNING *
	) SELECT ` + scheduledQuestionTestPlanColumns + `, ` + scheduledQuestionTestLatestColumns + `
		FROM updated p ` + scheduledQuestionTestLatestJoin
	mock.ExpectQuery("^"+regexp.QuoteMeta(query)+"$").WithArgs(int64(7), true, stale.CronExpression, next).
		WillReturnRows(sqlmock.NewRows(scheduledQuestionTestPlanTestColumns).AddRow(values...))
	updated, err := repo.Update(context.Background(), stale, service.ScheduledQuestionTestPlanUpdateMask{Enabled: true, NextRunAt: true})
	require.NoError(t, err)
	require.True(t, updated.Enabled)
	require.Equal(t, "0 * * * *", updated.CronExpression)
	require.Equal(t, &concurrentNext, updated.NextRunAt)
}

func TestScheduledQuestionTestRepositoryUpdateAllFieldsAndExplicitZero(t *testing.T) {
	repo, mock := newScheduledQuestionTestTestRepository(t)
	now := time.Now().UTC()
	plan := &service.ScheduledQuestionTestPlan{
		ID: 7, Name: "changed", AccountIDs: []int64{1}, QuestionIDs: []int64{2}, CronExpression: "0 * * * *",
	}
	query := `WITH updated AS (
		UPDATE scheduled_question_test_plans SET updated_at = NOW(), name = $2, account_ids = $3::jsonb, question_ids = $4::jsonb, model_id = $5, cron_expression = $6, enabled = $7, min_pass_ratio = $8, cooldown_minutes = $9, notify_enabled = $10, report_enabled = $11, email_enabled = $12, feishu_enabled = $13, feishu_webhook_url = $14, feishu_secret = $15, auto_pause_enabled = $16, auto_resume_enabled = $17, next_run_at = $18 WHERE id = $1 RETURNING *
	) SELECT ` + scheduledQuestionTestPlanColumns + `, ` + scheduledQuestionTestLatestColumns + `
		FROM updated p ` + scheduledQuestionTestLatestJoin
	mock.ExpectQuery("^"+regexp.QuoteMeta(query)+"$").
		WithArgs(int64(7), "changed", "[1]", "[2]", "", "0 * * * *", false, 0, 0, false, false, false, false, "", "", false, false, nil).
		WillReturnRows(sqlmock.NewRows(scheduledQuestionTestPlanTestColumns).AddRow(scheduledQuestionTestPlanTestValues(now, nil)...))
	_, err := repo.Update(context.Background(), plan, service.ScheduledQuestionTestPlanUpdateMask{
		Name: true, AccountIDs: true, QuestionIDs: true, ModelID: true, CronExpression: true,
		Enabled: true, MinPassRatio: true, CooldownMinutes: true, NotifyEnabled: true, ReportEnabled: true, EmailEnabled: true,
		FeishuEnabled: true, FeishuWebhookURL: true, FeishuSecret: true, AutoPauseEnabled: true, AutoResumeEnabled: true, NextRunAt: true,
	})
	require.NoError(t, err)
}

func TestScheduledQuestionTestRepositoryNotFound(t *testing.T) {
	repo, mock := newScheduledQuestionTestTestRepository(t)
	mock.ExpectQuery(`SELECT .* WHERE p.id = \$1`).WithArgs(int64(999)).WillReturnError(sql.ErrNoRows)
	_, err := repo.GetByID(context.Background(), 999)
	require.ErrorIs(t, err, service.ErrScheduledQuestionTestNotFound)
	mock.ExpectQuery(`WITH updated AS`).WillReturnRows(sqlmock.NewRows(scheduledQuestionTestPlanTestColumns))
	_, err = repo.Update(context.Background(), &service.ScheduledQuestionTestPlan{ID: 999}, service.ScheduledQuestionTestPlanUpdateMask{})
	require.ErrorIs(t, err, service.ErrScheduledQuestionTestNotFound)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT account_id FROM`).WithArgs(int64(999)).WillReturnRows(sqlmock.NewRows([]string{"account_id"}))
	mock.ExpectExec(`DELETE FROM scheduled_question_test_plans WHERE id = \$1`).WithArgs(int64(999)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	require.ErrorIs(t, repo.Delete(context.Background(), 999), service.ErrScheduledQuestionTestNotFound)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT account_id FROM`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"account_id"}))
	mock.ExpectExec(`DELETE FROM scheduled_question_test_plans WHERE id = \$1`).WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, repo.Delete(context.Background(), 7))
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT account_id FROM`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"account_id"}))
	mock.ExpectExec(`DELETE FROM scheduled_question_test_plans`).WillReturnResult(sqlmock.NewErrorResult(errors.New("rows affected unavailable")))
	mock.ExpectRollback()
	require.EqualError(t, repo.Delete(context.Background(), 7), "rows affected unavailable")
}

const scheduledQuestionDeleteAccountIDsTestQuery = `SELECT account_id FROM (
		SELECT value::bigint AS account_id FROM scheduled_question_test_plans p,
		jsonb_array_elements_text(p.account_ids) AS ids(value) WHERE p.id = $1
		UNION SELECT account_id FROM scheduled_question_test_account_states WHERE plan_id = $1
	) accounts ORDER BY account_id`

func TestScheduledQuestionTestRepositoryDeleteAccountLocks(t *testing.T) {
	repo, mock := newScheduledQuestionTestTestRepository(t)
	mock.ExpectBegin()
	mock.ExpectQuery("^" + regexp.QuoteMeta(scheduledQuestionDeleteAccountIDsTestQuery) + "$").WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"account_id"}).AddRow(int64(3)).AddRow(int64(1)).AddRow(int64(3))).RowsWillBeClosed()
	expectScheduledQuestionMultiAccountLocks(mock, 1, 3)
	mock.ExpectExec(`DELETE FROM scheduled_question_test_plans WHERE id = \$1`).WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, repo.Delete(context.Background(), 7))
}

func TestScheduledQuestionTestRepositoryDeleteTransactionFailures(t *testing.T) {
	for _, stage := range []string{"begin", "query", "scan", "rows", "advisory", "states", "delete", "commit"} {
		t.Run(stage, func(t *testing.T) {
			repo, mock := newScheduledQuestionTestTestRepository(t)
			failure := errors.New("delete unavailable")
			begin := mock.ExpectBegin()
			if stage == "begin" {
				begin.WillReturnError(failure)
			} else {
				query := mock.ExpectQuery("^" + regexp.QuoteMeta(scheduledQuestionDeleteAccountIDsTestQuery) + "$").WithArgs(int64(7))
				if stage == "query" {
					query.WillReturnError(failure)
				} else {
					rows := sqlmock.NewRows([]string{"account_id"})
					if stage == "scan" {
						rows.AddRow("invalid id")
					} else {
						rows.AddRow(int64(3))
					}
					if stage == "rows" {
						rows.RowError(0, failure)
					}
					query.WillReturnRows(rows).RowsWillBeClosed()
					if stage != "scan" && stage != "rows" {
						advisory := mock.ExpectExec("^" + regexp.QuoteMeta(scheduledQuestionAutoPauseTestLock) + "$").WithArgs(int64(3))
						if stage == "advisory" {
							advisory.WillReturnError(failure)
						} else {
							advisory.WillReturnResult(sqlmock.NewResult(0, 1))
							stateQuery := mock.ExpectQuery("^" + regexp.QuoteMeta(scheduledQuestionAutoPauseTestStates) + "$").WithArgs(int64(3))
							if stage == "states" {
								stateQuery.WillReturnError(failure)
							} else {
								stateQuery.WillReturnRows(sqlmock.NewRows([]string{"plan_id", "auto_paused"})).RowsWillBeClosed()
								del := mock.ExpectExec(`DELETE FROM scheduled_question_test_plans WHERE id = \$1`).WithArgs(int64(7))
								if stage == "delete" {
									del.WillReturnError(failure)
								} else {
									del.WillReturnResult(sqlmock.NewResult(0, 1))
								}
							}
						}
					}
				}
				if stage == "commit" {
					mock.ExpectCommit().WillReturnError(failure)
				} else {
					mock.ExpectRollback()
				}
			}
			err := repo.Delete(context.Background(), 7)
			if stage == "scan" {
				require.Error(t, err)
			} else {
				require.ErrorIs(t, err, failure)
			}
		})
	}
}

func TestScheduledQuestionTestRepositoryRunsAndStates(t *testing.T) {
	repo, mock := newScheduledQuestionTestTestRepository(t)
	now := time.Now().UTC()
	summary := `[{"account_id":3,"account_name":"account","platform":"anthropic","passed":0,"total":1,"judged":0,"ratio":null,"alerting":false,"state_after":"ok","error":"manual"}]`
	mock.ExpectQuery(`FROM scheduled_question_test_runs WHERE plan_id = \$1 ORDER BY started_at DESC, id DESC LIMIT \$2`).
		WithArgs(int64(7), 50).WillReturnRows(sqlmock.NewRows(scheduledQuestionTestRunTestColumns).AddRow(scheduledQuestionTestRunTestValues(now, summary)...)).RowsWillBeClosed()
	runs, err := repo.ListRuns(context.Background(), 7, 50)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Len(t, runs[0].Summary, 1)
	require.Nil(t, runs[0].Summary[0].Ratio)
	require.Equal(t, "ok", runs[0].Summary[0].StateAfter)

	mock.ExpectQuery(`FROM scheduled_question_test_account_states WHERE plan_id = \$1 ORDER BY account_id ASC`).
		WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows(scheduledQuestionTestStateTestColumns).
		AddRow(int64(1), int64(7), int64(1), "ok", nil, now, false, nil).
		AddRow(int64(2), int64(7), int64(3), "alert", now, now, true, now)).RowsWillBeClosed()
	states, err := repo.ListStates(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, states, 2)
	require.Nil(t, states[0].LastAlertAt)
	require.Equal(t, &now, states[1].LastAlertAt)
	require.False(t, states[0].AutoPaused)
	require.Nil(t, states[0].AutoPausedAt)
	require.True(t, states[1].AutoPaused)
	require.Equal(t, &now, states[1].AutoPausedAt)
}

func TestScheduledQuestionTestRepositoryRunNullArrays(t *testing.T) {
	repo, mock := newScheduledQuestionTestTestRepository(t)
	values := scheduledQuestionTestRunTestValues(time.Now().UTC(), "null")
	values[15] = []byte("null")
	mock.ExpectQuery(`FROM scheduled_question_test_runs`).
		WillReturnRows(sqlmock.NewRows(scheduledQuestionTestRunTestColumns).AddRow(values...)).RowsWillBeClosed()
	runs, err := repo.ListRuns(context.Background(), 7, 50)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, []service.ScheduledQuestionTestAccountSummary{}, runs[0].Summary)
	require.Equal(t, []service.ScheduledQuestionTestNotificationResult{}, runs[0].NotificationResults)
	encoded, err := json.Marshal(runs[0])
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"notification_results":[]`)
}

const scheduledQuestionAutoPauseTestLock = `SELECT pg_advisory_xact_lock(hashtextextended('scheduled-question-auto-pause:' || $1::text, 0))`

const scheduledQuestionAutoPauseTestStates = `SELECT plan_id, auto_paused FROM scheduled_question_test_account_states
		WHERE account_id = $1 ORDER BY plan_id FOR UPDATE`

func expectScheduledQuestionAutoPauseLocks(mock sqlmock.Sqlmock, rows *sqlmock.Rows) {
	expectScheduledQuestionAutoPauseLocksForAccount(mock, 3, rows)
}

func expectScheduledQuestionAutoPauseLocksForAccount(mock sqlmock.Sqlmock, accountID int64, rows *sqlmock.Rows) {
	mock.ExpectExec("^" + regexp.QuoteMeta(scheduledQuestionAutoPauseTestLock) + "$").WithArgs(accountID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("^" + regexp.QuoteMeta(scheduledQuestionAutoPauseTestStates) + "$").WithArgs(accountID).WillReturnRows(rows).RowsWillBeClosed()
}

func expectScheduledQuestionMultiAccountLocks(mock sqlmock.Sqlmock, accountIDs ...int64) {
	for _, accountID := range accountIDs {
		mock.ExpectExec("^" + regexp.QuoteMeta(scheduledQuestionAutoPauseTestLock) + "$").WithArgs(accountID).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	for _, accountID := range accountIDs {
		mock.ExpectQuery("^" + regexp.QuoteMeta(scheduledQuestionAutoPauseTestStates) + "$").WithArgs(accountID).
			WillReturnRows(sqlmock.NewRows([]string{"plan_id", "auto_paused"})).RowsWillBeClosed()
	}
}

func TestScheduledQuestionTestRepositorySetAutoPaused(t *testing.T) {
	query := `INSERT INTO scheduled_question_test_account_states
		(plan_id, account_id, state, auto_paused, auto_paused_at) VALUES ($1, $2, 'alert', $3, $4)
		ON CONFLICT (plan_id, account_id) DO UPDATE SET
		auto_paused = EXCLUDED.auto_paused, auto_paused_at = EXCLUDED.auto_paused_at`
	now := time.Now().UTC()
	for _, paused := range []bool{false, true} {
		t.Run(fmt.Sprint(paused), func(t *testing.T) {
			repo, mock := newScheduledQuestionTestTestRepository(t)
			var expectedAt driver.Value
			if paused {
				expectedAt = now
			}
			mock.ExpectBegin()
			expectScheduledQuestionAutoPauseLocks(mock, sqlmock.NewRows([]string{"plan_id", "auto_paused"}))
			mock.ExpectExec("^"+regexp.QuoteMeta(query)+"$").WithArgs(int64(7), int64(3), paused, expectedAt).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
			require.NoError(t, repo.SetAutoPaused(context.Background(), 7, 3, paused, &now))
		})
	}
}

const scheduledQuestionVerdictStateTestQuery = `INSERT INTO scheduled_question_test_account_states
		(plan_id, account_id, state) VALUES ($1, $2, $3)
		ON CONFLICT (plan_id, account_id) DO UPDATE SET state = EXCLUDED.state`

const scheduledQuestionCurrentSchedulingTestQuery = `SELECT schedulable FROM accounts WHERE id = $1 AND deleted_at IS NULL`

const scheduledQuestionResumeCheckTestQuery = `SELECT EXISTS (
		SELECT 1 FROM scheduled_question_test_account_states s
		JOIN scheduled_question_test_plans p ON p.id = s.plan_id
		WHERE s.account_id = $1 AND s.plan_id <> $2 AND s.state = 'alert'
		AND p.enabled = true AND p.auto_pause_enabled = true
	)`

const scheduledQuestionResumeClearTestQuery = `UPDATE scheduled_question_test_account_states
		SET auto_paused = false, auto_paused_at = NULL WHERE account_id = $1`

const scheduledQuestionPauseMarkerTestQuery = `UPDATE scheduled_question_test_account_states
			SET auto_paused = true, auto_paused_at = $3 WHERE plan_id = $1 AND account_id = $2`

func TestScheduledQuestionTestRepositoryApplyAccountVerdict(t *testing.T) {
	for _, test := range []struct {
		name, state, action                      string
		current, pause, resume, blocked, missing bool
	}{
		{"pause", "alert", "paused", true, true, false, false, false},
		{"resume", "ok", "resumed", false, false, true, false, false},
		{"blocked", "ok", "resume_blocked", false, false, true, true, false},
		{"closed_failure_publishes", "alert", "", false, true, false, false, false},
		{"open_passing_publishes", "ok", "", true, false, true, false, false},
		{"switches_off_publishes", "alert", "", true, false, false, false, false},
		{"missing_account_publishes", "alert", "", false, true, false, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, mock := newScheduledQuestionTestTestRepository(t)
			now := time.Now().UTC()
			mock.ExpectBegin()
			expectScheduledQuestionAutoPauseLocks(mock, sqlmock.NewRows([]string{"plan_id", "auto_paused"}))
			mock.ExpectExec("^"+regexp.QuoteMeta(scheduledQuestionVerdictStateTestQuery)+"$").WithArgs(int64(7), int64(3), test.state).WillReturnResult(sqlmock.NewResult(0, 1))
			query := mock.ExpectQuery("^" + regexp.QuoteMeta(scheduledQuestionCurrentSchedulingTestQuery) + "$").WithArgs(int64(3))
			if test.missing {
				query.WillReturnError(sql.ErrNoRows)
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"schedulable"}).AddRow(test.current))
			}
			if test.resume && !test.current && !test.missing {
				mock.ExpectQuery("^"+regexp.QuoteMeta(scheduledQuestionResumeCheckTestQuery)+"$").WithArgs(int64(3), int64(7)).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(test.blocked))
			}
			if test.action == "paused" {
				mock.ExpectExec("^"+regexp.QuoteMeta(scheduledQuestionPauseMarkerTestQuery)+"$").WithArgs(int64(7), int64(3), now).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			if test.action == "resumed" {
				mock.ExpectExec("^" + regexp.QuoteMeta(scheduledQuestionResumeClearTestQuery) + "$").WithArgs(int64(3)).WillReturnResult(sqlmock.NewResult(0, 2))
			}
			mock.ExpectCommit()
			called := false
			action, err := repo.ApplyAccountVerdict(context.Background(), 7, 3, test.state, test.pause, test.resume, now, func(ctx context.Context, value bool) error {
				require.NoError(t, ctx.Err())
				called = true
				require.Equal(t, test.action == "resumed", value)
				return nil
			})
			require.NoError(t, err)
			require.Equal(t, test.action, action)
			require.Equal(t, test.action == "paused" || test.action == "resumed", called)
		})
	}
}

func TestScheduledQuestionTestRepositoryApplyAccountVerdictFailures(t *testing.T) {
	for _, method := range []string{"pause", "resume"} {
		for _, stage := range []string{"begin", "advisory", "rows_query", "rows_scan", "rows_error", "state", "account", "check", "callback", "marker", "commit"} {
			if method == "pause" && stage == "check" {
				continue
			}
			t.Run(method+"_"+stage, func(t *testing.T) {
				repo, mock := newScheduledQuestionTestTestRepository(t)
				failure := errors.New("verdict transaction failed")
				setup := func() {
					begin := mock.ExpectBegin()
					if stage == "begin" {
						begin.WillReturnError(failure)
						return
					}
					lock := mock.ExpectExec("^" + regexp.QuoteMeta(scheduledQuestionAutoPauseTestLock) + "$").WithArgs(int64(3))
					if stage == "advisory" {
						lock.WillReturnError(failure)
						return
					}
					lock.WillReturnResult(sqlmock.NewResult(0, 1))
					rowsQuery := mock.ExpectQuery("^" + regexp.QuoteMeta(scheduledQuestionAutoPauseTestStates) + "$").WithArgs(int64(3))
					if stage == "rows_query" {
						rowsQuery.WillReturnError(failure)
						return
					}
					rows := sqlmock.NewRows([]string{"plan_id", "auto_paused"})
					if stage == "rows_scan" {
						rows.AddRow("invalid", true)
					} else {
						rows.AddRow(int64(7), true)
					}
					if stage == "rows_error" {
						rows.RowError(0, failure)
					}
					rowsQuery.WillReturnRows(rows).RowsWillBeClosed()
					if stage == "rows_scan" || stage == "rows_error" {
						return
					}
					state := mock.ExpectExec(`INSERT INTO scheduled_question_test_account_states`)
					if stage == "state" {
						state.WillReturnError(failure)
						return
					}
					state.WillReturnResult(sqlmock.NewResult(0, 1))
					account := mock.ExpectQuery("^" + regexp.QuoteMeta(scheduledQuestionCurrentSchedulingTestQuery) + "$")
					if stage == "account" {
						account.WillReturnError(failure)
						return
					}
					account.WillReturnRows(sqlmock.NewRows([]string{"schedulable"}).AddRow(method == "pause"))
					if method == "resume" {
						check := mock.ExpectQuery(`SELECT EXISTS`)
						if stage == "check" {
							check.WillReturnError(failure)
							return
						}
						check.WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
					}
					if stage == "callback" {
						return
					}
					marker := mock.ExpectExec(`UPDATE scheduled_question_test_account_states`)
					if stage == "marker" {
						marker.WillReturnError(failure)
						return
					}
					marker.WillReturnResult(sqlmock.NewResult(0, 1))
				}
				setup()
				if stage == "commit" {
					mock.ExpectCommit().WillReturnError(failure)
				} else if stage != "begin" {
					mock.ExpectRollback()
				}
				state := "ok"
				if method == "pause" {
					state = "alert"
				}
				action, err := repo.ApplyAccountVerdict(context.Background(), 7, 3, state, method == "pause", method == "resume", time.Now(), func(context.Context, bool) error {
					if stage == "callback" {
						return failure
					}
					return nil
				})
				require.Empty(t, action)
				if stage == "rows_scan" {
					require.Error(t, err)
				} else {
					require.ErrorIs(t, err, failure)
				}
			})
		}
	}
}

func TestScheduledQuestionTestRepositoryFeishuRequiredConstraint(t *testing.T) {
	for _, method := range []string{"create", "update"} {
		for _, test := range []struct {
			name       string
			failure    error
			validation bool
		}{
			{"required", &pq.Error{Code: "23514", Constraint: scheduledQuestionTestFeishuWebhookRequiredConstraint, Detail: "private database row"}, true},
			{"wrapped", fmt.Errorf("write: %w", &pq.Error{Code: "23514", Constraint: scheduledQuestionTestFeishuWebhookRequiredConstraint}), true},
			{"other_check", &pq.Error{Code: "23514", Constraint: "different_check"}, false},
			{"other_code", &pq.Error{Code: "23505", Constraint: scheduledQuestionTestFeishuWebhookRequiredConstraint}, false},
			{"generic", errors.New("database unavailable"), false},
		} {
			t.Run(method+"_"+test.name, func(t *testing.T) {
				repo, mock := newScheduledQuestionTestTestRepository(t)
				mock.ExpectQuery(`.*`).WillReturnError(test.failure)
				plan := &service.ScheduledQuestionTestPlan{ID: 7, FeishuEnabled: true}
				var err error
				if method == "create" {
					_, err = repo.Create(context.Background(), plan)
				} else {
					_, err = repo.Update(context.Background(), plan, service.ScheduledQuestionTestPlanUpdateMask{FeishuEnabled: true})
				}
				if test.validation {
					var validation *model.ValidationError
					require.ErrorAs(t, err, &validation)
					require.Equal(t, "feishu_webhook_url", validation.Field)
					require.NotContains(t, err.Error(), "private database row")
				} else {
					require.ErrorIs(t, err, test.failure)
				}
			})
		}
	}
}

func TestScheduledQuestionTestRepositoryFinishRunAtomic(t *testing.T) {
	repo, mock := newScheduledQuestionTestTestRepository(t)
	now := time.Now().UTC()
	next := now.Add(time.Hour)
	ratio := 0
	run := &service.ScheduledQuestionTestRun{
		PlanID: 7, Status: "success", StartedAt: now, FinishedAt: now, TotalAccounts: 2, PassedAccounts: 1, AlertedAccounts: 1,
		CronExpression: "0 * * * *", PlanUpdatedAt: now,
		PausedAccounts: 1, ResumedAccounts: 1, AutoPauseSkipped: true, SkippedAccounts: 1,
		NotificationResults: []service.ScheduledQuestionTestNotificationResult{{Kind: "report", Channel: "feishu", Success: true, Detail: "sent"}},
		Summary:             []service.ScheduledQuestionTestAccountSummary{{AccountID: 3, Total: 1, Ratio: &ratio, Alerting: true, StateAfter: "alert", Error: "all failed", ScheduleAction: "paused", AutoPaused: true}},
	}
	summaryData, err := json.Marshal(run.Summary)
	require.NoError(t, err)
	summary := string(summaryData)
	notifications := `[{"kind":"report","channel":"feishu","success":true,"detail":"sent"}]`
	values := scheduledQuestionTestRunTestValues(now, summary)
	values[12], values[13], values[14], values[15], values[16] = 1, 1, true, []byte(notifications), 1
	mock.ExpectBegin()
	expectScheduledQuestionMultiAccountLocks(mock, 1, 3)
	mock.ExpectQuery(`SELECT id FROM scheduled_question_test_plans WHERE id = \$1 FOR NO KEY UPDATE`).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(7)))
	mock.ExpectQuery(`INSERT INTO scheduled_question_test_runs`).
		WithArgs(int64(7), "success", now, now, 2, 1, 1, false, "", summary, 1, 1, true, notifications, 1).
		WillReturnRows(sqlmock.NewRows(scheduledQuestionTestRunTestColumns).AddRow(values...))
	stateQuery := `INSERT INTO scheduled_question_test_account_states
			(plan_id, account_id, state, last_alert_at, updated_at) VALUES ($1, $2, $3, $4, NOW())
			ON CONFLICT (plan_id, account_id) DO UPDATE SET
			state = EXCLUDED.state, last_alert_at = EXCLUDED.last_alert_at, updated_at = NOW()`
	mock.ExpectExec("^"+regexp.QuoteMeta(stateQuery)+"$").
		WithArgs(int64(7), int64(3), "alert", now).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO scheduled_question_test_account_states`).
		WithArgs(int64(7), int64(1), "ok", nil).WillReturnResult(sqlmock.NewResult(0, 1))
	scheduleQuery := `UPDATE scheduled_question_test_plans
		SET last_run_at = $2,
			next_run_at = CASE WHEN enabled = true AND cron_expression = $4 AND updated_at = $5 THEN $3 ELSE next_run_at END,
			updated_at = NOW()
		WHERE id = $1`
	mock.ExpectExec("^"+regexp.QuoteMeta(scheduleQuery)+"$").
		WithArgs(int64(7), now, next, "0 * * * *", now).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	created, err := repo.FinishRun(context.Background(), run, []*service.ScheduledQuestionTestAccountState{
		{PlanID: 7, AccountID: 3, State: "alert", LastAlertAt: &now, AutoPaused: true, AutoPausedAt: &now},
		{PlanID: 7, AccountID: 1, State: "ok", LastAlertAt: nil},
	}, next)
	require.NoError(t, err)
	require.Equal(t, int64(9), created.ID)
	require.Equal(t, run.Summary, created.Summary)
	require.Equal(t, run.NotificationResults, created.NotificationResults)
	require.Equal(t, run.PausedAccounts, created.PausedAccounts)
	require.Equal(t, run.ResumedAccounts, created.ResumedAccounts)
	require.True(t, created.AutoPauseSkipped)
	require.Equal(t, 1, created.SkippedAccounts)
	require.True(t, created.Summary[0].AutoPaused)
}

func TestScheduledQuestionTestRepositoryNotificationsSuppressedPersists(t *testing.T) {
	repo, mock := newScheduledQuestionTestTestRepository(t)
	now := time.Now().UTC()
	run := &service.ScheduledQuestionTestRun{
		PlanID: 7, Status: "failed", StartedAt: now, FinishedAt: now, NotificationsSuppressed: true,
		CronExpression: "0 * * * *", PlanUpdatedAt: now,
	}
	values := scheduledQuestionTestRunTestValues(now, "[]")
	values[2], values[8] = "failed", true
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id FROM scheduled_question_test_plans .* FOR NO KEY UPDATE`).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(7)))
	mock.ExpectQuery(`INSERT INTO scheduled_question_test_runs .* notifications_suppressed, error_message, summary, paused_accounts, resumed_accounts, auto_pause_skipped, notification_results, skipped_accounts, created_at`).
		WithArgs(int64(7), "failed", now, now, 0, 0, 0, true, "", "[]", 0, 0, false, "[]", 0).
		WillReturnRows(sqlmock.NewRows(scheduledQuestionTestRunTestColumns).AddRow(values...))
	mock.ExpectExec(`UPDATE scheduled_question_test_plans`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	created, err := repo.FinishRun(context.Background(), run, nil, now.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, created.NotificationsSuppressed)
	require.Equal(t, []service.ScheduledQuestionTestNotificationResult{}, created.NotificationResults)

	mock.ExpectQuery(`FROM scheduled_question_test_runs WHERE plan_id = \$1`).WithArgs(int64(7), 50).
		WillReturnRows(sqlmock.NewRows(scheduledQuestionTestRunTestColumns).AddRow(values...))
	runs, err := repo.ListRuns(context.Background(), 7, 50)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.True(t, runs[0].NotificationsSuppressed)
}

func TestScheduledQuestionTestRepositoryFinishRunRollback(t *testing.T) {
	for _, stage := range []string{"lock", "missing", "insert", "state", "state_zero", "schedule", "schedule_zero", "rows_affected", "commit"} {
		t.Run(stage, func(t *testing.T) {
			repo, mock := newScheduledQuestionTestTestRepository(t)
			now := time.Now().UTC()
			run := &service.ScheduledQuestionTestRun{PlanID: 7, Status: "success", StartedAt: now, FinishedAt: now}
			failure := errors.New("database unavailable")
			mock.ExpectBegin()
			expectScheduledQuestionAutoPauseLocks(mock, sqlmock.NewRows([]string{"plan_id", "auto_paused"}))
			lock := mock.ExpectQuery(`SELECT id FROM scheduled_question_test_plans .* FOR NO KEY UPDATE`)
			switch stage {
			case "lock":
				lock.WillReturnError(failure)
			case "missing":
				lock.WillReturnError(sql.ErrNoRows)
			default:
				lock.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(7)))
			}
			if stage != "lock" && stage != "missing" {
				insert := mock.ExpectQuery(`INSERT INTO scheduled_question_test_runs`).
					WithArgs(int64(7), "success", now, now, 0, 0, 0, false, "", "[]", 0, 0, false, "[]", 0)
				if stage == "insert" {
					insert.WillReturnError(failure)
				} else {
					insert.WillReturnRows(sqlmock.NewRows(scheduledQuestionTestRunTestColumns).AddRow(scheduledQuestionTestRunTestValues(now, "[]")...))
					state := mock.ExpectExec(`INSERT INTO scheduled_question_test_account_states`)
					switch stage {
					case "state":
						state.WillReturnError(failure)
					case "state_zero":
						state.WillReturnResult(sqlmock.NewResult(0, 0))
					default:
						state.WillReturnResult(sqlmock.NewResult(0, 1))
						schedule := mock.ExpectExec(`UPDATE scheduled_question_test_plans`)
						switch stage {
						case "schedule":
							schedule.WillReturnError(failure)
						case "schedule_zero":
							schedule.WillReturnResult(sqlmock.NewResult(0, 0))
						case "rows_affected":
							schedule.WillReturnResult(sqlmock.NewErrorResult(failure))
						default:
							schedule.WillReturnResult(sqlmock.NewResult(0, 1))
						}
					}
				}
			}
			if stage == "commit" {
				mock.ExpectCommit().WillReturnError(failure)
			} else {
				mock.ExpectRollback()
			}
			_, err := repo.FinishRun(context.Background(), run, []*service.ScheduledQuestionTestAccountState{{AccountID: 3, State: "ok"}}, now.Add(time.Hour))
			if stage == "missing" || stage == "state_zero" || stage == "schedule_zero" {
				require.ErrorIs(t, err, service.ErrScheduledQuestionTestNotFound)
			} else {
				require.ErrorIs(t, err, failure)
			}
		})
	}
}

func TestScheduledQuestionTestRepositoryFinishRunAccountLocks(t *testing.T) {
	for _, stage := range []string{"advisory", "states", "deduplicate"} {
		t.Run(stage, func(t *testing.T) {
			repo, mock := newScheduledQuestionTestTestRepository(t)
			failure := errors.New("account lock unavailable")
			mock.ExpectBegin()
			if stage == "advisory" {
				mock.ExpectExec("^" + regexp.QuoteMeta(scheduledQuestionAutoPauseTestLock) + "$").
					WithArgs(int64(1)).WillReturnError(failure)
			} else if stage == "states" {
				mock.ExpectExec("^" + regexp.QuoteMeta(scheduledQuestionAutoPauseTestLock) + "$").
					WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("^" + regexp.QuoteMeta(scheduledQuestionAutoPauseTestLock) + "$").
					WithArgs(int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectQuery("^" + regexp.QuoteMeta(scheduledQuestionAutoPauseTestStates) + "$").
					WithArgs(int64(1)).WillReturnError(failure)
			} else {
				expectScheduledQuestionMultiAccountLocks(mock, 1, 3)
				mock.ExpectQuery(`SELECT id FROM scheduled_question_test_plans .* FOR NO KEY UPDATE`).WillReturnError(failure)
			}
			mock.ExpectRollback()
			_, err := repo.FinishRun(context.Background(), &service.ScheduledQuestionTestRun{PlanID: 7},
				[]*service.ScheduledQuestionTestAccountState{{AccountID: 3}, {AccountID: 1}, {AccountID: 3}}, time.Now())
			require.ErrorIs(t, err, failure)
		})
	}
}

func TestScheduledQuestionTestRepositoryQueryErrors(t *testing.T) {
	for _, method := range []string{"create", "get", "list", "due", "update", "delete", "runs", "states", "begin"} {
		t.Run(method, func(t *testing.T) {
			repo, mock := newScheduledQuestionTestTestRepository(t)
			failure := errors.New("query unavailable")
			ctx := context.Background()
			var err error
			switch method {
			case "delete":
				mock.ExpectBegin()
				mock.ExpectQuery(`SELECT account_id FROM`).WillReturnError(failure)
				mock.ExpectRollback()
				err = repo.Delete(ctx, 7)
			case "begin":
				mock.ExpectBegin().WillReturnError(failure)
				_, err = repo.FinishRun(ctx, &service.ScheduledQuestionTestRun{}, nil, time.Now())
			default:
				mock.ExpectQuery(`.*`).WillReturnError(failure)
				switch method {
				case "create":
					_, err = repo.Create(ctx, &service.ScheduledQuestionTestPlan{})
				case "get":
					_, err = repo.GetByID(ctx, 7)
				case "list":
					_, err = repo.List(ctx)
				case "due":
					_, err = repo.ListDue(ctx, time.Now())
				case "update":
					_, err = repo.Update(ctx, &service.ScheduledQuestionTestPlan{}, service.ScheduledQuestionTestPlanUpdateMask{})
				case "runs":
					_, err = repo.ListRuns(ctx, 7, 50)
				case "states":
					_, err = repo.ListStates(ctx, 7)
				}
			}
			require.ErrorIs(t, err, failure)
		})
	}
}

func TestScheduledQuestionTestRepositoryScanAndRowErrors(t *testing.T) {
	for _, invalid := range []string{"accounts", "questions", "scan", "rows"} {
		t.Run("plan_"+invalid, func(t *testing.T) {
			repo, mock := newScheduledQuestionTestTestRepository(t)
			values := scheduledQuestionTestPlanTestValues(time.Now(), nil)
			switch invalid {
			case "accounts":
				values[2] = []byte("invalid json")
			case "questions":
				values[3] = []byte("invalid json")
			case "scan":
				values[0] = "invalid id"
			}
			rows := sqlmock.NewRows(scheduledQuestionTestPlanTestColumns).AddRow(values...)
			if invalid == "rows" {
				rows.RowError(0, errors.New("row unavailable"))
			}
			mock.ExpectQuery(`SELECT`).WillReturnRows(rows).RowsWillBeClosed()
			_, err := repo.List(context.Background())
			require.Error(t, err)
		})
	}
	for _, invalid := range []string{"summary", "notifications", "scan", "rows"} {
		t.Run("run_"+invalid, func(t *testing.T) {
			repo, mock := newScheduledQuestionTestTestRepository(t)
			values := scheduledQuestionTestRunTestValues(time.Now(), "[]")
			if invalid == "summary" {
				values[10] = []byte("invalid json")
			} else if invalid == "notifications" {
				values[15] = []byte("invalid json")
			} else if invalid == "scan" {
				values[0] = "invalid id"
			}
			rows := sqlmock.NewRows(scheduledQuestionTestRunTestColumns).AddRow(values...)
			if invalid == "rows" {
				rows.RowError(0, errors.New("row unavailable"))
			}
			mock.ExpectQuery(`SELECT`).WillReturnRows(rows).RowsWillBeClosed()
			_, err := repo.ListRuns(context.Background(), 7, 50)
			require.Error(t, err)
		})
	}
	for _, invalid := range []string{"scan", "rows"} {
		t.Run("state_"+invalid, func(t *testing.T) {
			repo, mock := newScheduledQuestionTestTestRepository(t)
			values := []driver.Value{int64(1), int64(7), int64(3), "ok", nil, time.Now(), false, nil}
			if invalid == "scan" {
				values[0] = "invalid id"
			}
			rows := sqlmock.NewRows(scheduledQuestionTestStateTestColumns).AddRow(values...)
			if invalid == "rows" {
				rows.RowError(0, errors.New("row unavailable"))
			}
			mock.ExpectQuery(`SELECT`).WillReturnRows(rows).RowsWillBeClosed()
			_, err := repo.ListStates(context.Background(), 7)
			require.Error(t, err)
		})
	}
}
