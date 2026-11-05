//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestScheduledQuestionTestRepositoryIntegrationLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := NewScheduledQuestionTestRepository(integrationDB)
	now := time.Now().UTC().Truncate(time.Microsecond)
	next := now.Add(time.Hour)
	plan, err := repo.Create(ctx, &service.ScheduledQuestionTestPlan{
		Name: t.Name(), AccountIDs: []int64{910001, 910002, 910003}, QuestionIDs: []int64{920001},
		ModelID: "integration-model", CronExpression: "0 * * * *", Enabled: true,
		MinPassRatio: 80, CooldownMinutes: 360, NotifyEnabled: true,
		ReportEnabled: true, EmailEnabled: false, FeishuEnabled: true,
		FeishuWebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/integration-token-abcd1234",
		FeishuSecret:     "integration-secret", AutoPauseEnabled: true, AutoResumeEnabled: true,
		NextRunAt: &next,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, repo.Delete(context.Background(), plan.ID))
	})
	require.Positive(t, plan.ID)
	require.True(t, plan.ReportEnabled)
	require.False(t, plan.EmailEnabled)
	require.True(t, plan.FeishuEnabled)
	require.True(t, plan.AutoPauseEnabled)
	require.True(t, plan.AutoResumeEnabled)
	require.True(t, plan.FeishuWebhookConfigured)
	require.True(t, plan.FeishuSecretConfigured)
	require.Contains(t, plan.FeishuWebhookHint, "****1234")
	require.Zero(t, plan.SkippedAccounts)

	loaded, err := repo.GetByID(ctx, plan.ID)
	require.NoError(t, err)
	require.Equal(t, plan, loaded)
	encoded, err := json.Marshal(loaded)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "integration-token-abcd1234")
	require.NotContains(t, string(encoded), "integration-secret")

	loaded.ReportEnabled, loaded.EmailEnabled, loaded.FeishuEnabled = false, true, false
	loaded.FeishuWebhookURL, loaded.FeishuSecret = "", ""
	loaded.AutoPauseEnabled, loaded.AutoResumeEnabled = false, false
	updated, err := repo.Update(ctx, loaded, service.ScheduledQuestionTestPlanUpdateMask{
		ReportEnabled: true, EmailEnabled: true, FeishuEnabled: true,
		FeishuWebhookURL: true, FeishuSecret: true, AutoPauseEnabled: true, AutoResumeEnabled: true,
	})
	require.NoError(t, err)
	require.False(t, updated.ReportEnabled)
	require.True(t, updated.EmailEnabled)
	require.False(t, updated.FeishuEnabled)
	require.Empty(t, updated.FeishuWebhookURL)
	require.Empty(t, updated.FeishuSecret)
	require.False(t, updated.FeishuWebhookConfigured)
	require.False(t, updated.FeishuSecretConfigured)
	require.Empty(t, updated.FeishuWebhookHint)
	require.False(t, updated.AutoPauseEnabled)
	require.False(t, updated.AutoResumeEnabled)
	loaded, err = repo.GetByID(ctx, plan.ID)
	require.NoError(t, err)
	require.Equal(t, updated, loaded)

	failedRatio, passedRatio := 0, 100
	finishedAt := now.Add(time.Minute)
	next = now.Add(2 * time.Hour)
	run := &service.ScheduledQuestionTestRun{
		PlanID: plan.ID, CronExpression: loaded.CronExpression, PlanUpdatedAt: loaded.UpdatedAt,
		Status: "success", StartedAt: now, FinishedAt: finishedAt,
		TotalAccounts: 3, PassedAccounts: 1, AlertedAccounts: 1, PausedAccounts: 1, ResumedAccounts: 1, SkippedAccounts: 1,
		Summary: []service.ScheduledQuestionTestAccountSummary{
			{AccountID: 910001, AccountName: "paused-account", Total: 1, Judged: 1, Ratio: &failedRatio, Alerting: true, StateAfter: "alert", ScheduleAction: "paused", AutoPaused: true},
			{AccountID: 910002, AccountName: "resumed-account", Passed: 1, Total: 1, Judged: 1, Ratio: &passedRatio, StateAfter: "ok", ScheduleAction: "resumed"},
			{AccountID: 910003, AccountName: "skipped-account", Result: "skipped", SkipReason: "rate_limited", SkipResetAt: &next},
		},
		NotificationResults: []service.ScheduledQuestionTestNotificationResult{
			{Kind: "alert", Channel: "feishu", Success: true, Detail: "sent"},
			{Kind: "report", Channel: "email", Success: false, Detail: "no verified recipients"},
		},
	}
	require.NoError(t, repo.SetAutoPaused(ctx, plan.ID, 910001, true, &finishedAt))
	createdRun, err := repo.FinishRun(ctx, run, []*service.ScheduledQuestionTestAccountState{
		{AccountID: 910001, State: "alert", LastAlertAt: &finishedAt, AutoPaused: true, AutoPausedAt: &finishedAt},
		{AccountID: 910002, State: "ok"},
	}, next)
	require.NoError(t, err)
	require.Positive(t, createdRun.ID)
	require.Equal(t, run.Summary, createdRun.Summary)
	require.Equal(t, run.NotificationResults, createdRun.NotificationResults)
	require.Equal(t, 1, createdRun.PausedAccounts)
	require.Equal(t, 1, createdRun.ResumedAccounts)
	require.Equal(t, 1, createdRun.SkippedAccounts)
	require.False(t, createdRun.AutoPauseSkipped)

	states, err := repo.ListStates(ctx, plan.ID)
	require.NoError(t, err)
	require.Len(t, states, 2)
	require.Equal(t, int64(910001), states[0].AccountID)
	require.True(t, states[0].AutoPaused)
	require.NotNil(t, states[0].AutoPausedAt)
	require.True(t, states[0].AutoPausedAt.Equal(finishedAt))
	require.NotNil(t, states[0].LastAlertAt)
	require.True(t, states[0].LastAlertAt.Equal(finishedAt))
	require.False(t, states[1].AutoPaused)
	require.Nil(t, states[1].AutoPausedAt)
	loaded, err = repo.GetByID(ctx, plan.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded.LastRunAt)
	require.True(t, loaded.LastRunAt.Equal(finishedAt))
	require.NotNil(t, loaded.NextRunAt)
	require.True(t, loaded.NextRunAt.Equal(next))
	require.Equal(t, "success", loaded.LastStatus)
	require.Equal(t, 1, loaded.PassedAccounts)
	require.Equal(t, 1, loaded.AlertedAccounts)
	require.Equal(t, 1, loaded.SkippedAccounts)
	plans, err := repo.List(ctx)
	require.NoError(t, err)
	for _, listed := range plans {
		if listed.ID == plan.ID {
			require.Equal(t, 1, listed.SkippedAccounts)
		}
	}

	require.NoError(t, repo.SetAutoPaused(ctx, plan.ID, 910001, false, nil))
	emptyRun, err := repo.FinishRun(ctx, &service.ScheduledQuestionTestRun{
		PlanID: plan.ID, CronExpression: loaded.CronExpression, PlanUpdatedAt: loaded.UpdatedAt,
		Status: "failed", StartedAt: finishedAt.Add(time.Minute), FinishedAt: finishedAt.Add(2 * time.Minute),
		NotificationsSuppressed: true, ErrorMessage: "round unavailable", AutoPauseSkipped: true,
	}, []*service.ScheduledQuestionTestAccountState{{AccountID: 910001, State: "ok"}}, next)
	require.NoError(t, err)
	require.NotNil(t, emptyRun.NotificationResults)
	require.Empty(t, emptyRun.NotificationResults)
	states, err = repo.ListStates(ctx, plan.ID)
	require.NoError(t, err)
	require.Len(t, states, 2)
	require.Equal(t, "ok", states[0].State)
	require.False(t, states[0].AutoPaused)
	require.Nil(t, states[0].AutoPausedAt)
	require.Nil(t, states[0].LastAlertAt)

	runs, err := repo.ListRuns(ctx, plan.ID, 50)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	require.Equal(t, emptyRun.ID, runs[0].ID)
	require.True(t, runs[0].AutoPauseSkipped)
	require.True(t, runs[0].NotificationsSuppressed)
	require.Equal(t, "round unavailable", runs[0].ErrorMessage)
	require.Equal(t, []service.ScheduledQuestionTestAccountSummary{}, runs[0].Summary)
	require.Equal(t, []service.ScheduledQuestionTestNotificationResult{}, runs[0].NotificationResults)
	require.Equal(t, createdRun.ID, runs[1].ID)
	require.Equal(t, run.Summary, runs[1].Summary)
	require.Equal(t, run.NotificationResults, runs[1].NotificationResults)
	require.Equal(t, 1, runs[1].PausedAccounts)
	require.Equal(t, 1, runs[1].ResumedAccounts)
	require.Equal(t, 1, runs[1].SkippedAccounts)
	var storedSummary, storedNotifications string
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT summary::text, notification_results::text FROM scheduled_question_test_runs WHERE id = $1`, emptyRun.ID,
	).Scan(&storedSummary, &storedNotifications))
	require.JSONEq(t, "[]", storedSummary)
	require.JSONEq(t, "[]", storedNotifications)
}

func newScheduledQuestionVerdictIntegrationAccount(t *testing.T, schedulable bool) (*service.Account, service.AccountRepository) {
	t.Helper()
	client := testEntClient(t)
	account := mustCreateAccount(t, client, &service.Account{Name: t.Name(), Schedulable: true})
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id = $1", account.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", account.ID)
		require.NoError(t, err)
	})
	_, err := client.Account.UpdateOneID(account.ID).SetSchedulable(schedulable).Save(context.Background())
	require.NoError(t, err)
	account.Schedulable = schedulable
	return account, NewAccountRepository(client, integrationDB, nil)
}

func TestScheduledQuestionTestRepositoryIntegrationResumeRules(t *testing.T) {
	for _, test := range []struct {
		name                                                 string
		enabled, autoPause, otherAlert, otherPaused, blocked bool
	}{
		{"alert_without_marker_blocks", true, true, true, false, true},
		{"alert_with_marker_blocks", true, true, true, true, true},
		{"disabled_plan_does_not_block", false, true, true, true, false},
		{"automatic_pause_off_does_not_block", true, false, true, true, false},
		{"ok_state_does_not_block", true, true, false, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repo, first := newScheduledQuestionTestIntegrationPlan(t)
			_, second := newScheduledQuestionTestIntegrationPlan(t)
			account, accounts := newScheduledQuestionVerdictIntegrationAccount(t, false)
			accountID := account.ID
			now := time.Now().UTC().Truncate(time.Microsecond)
			_, err := integrationDB.ExecContext(ctx, `UPDATE scheduled_question_test_plans
				SET enabled = $2, auto_pause_enabled = $3 WHERE id = $1`, second.ID, test.enabled, test.autoPause)
			require.NoError(t, err)
			require.NoError(t, repo.SetAutoPaused(ctx, first.ID, accountID, true, &now))
			require.NoError(t, repo.SetAutoPaused(ctx, second.ID, accountID, test.otherPaused, &now))
			otherState := "ok"
			if test.otherAlert {
				otherState = "alert"
			}
			_, err = integrationDB.ExecContext(ctx, `UPDATE scheduled_question_test_account_states
				SET state = $3 WHERE plan_id = $1 AND account_id = $2`, second.ID, accountID, otherState)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, `UPDATE scheduled_question_test_account_states
				SET last_alert_at = $3 WHERE plan_id = $1 AND account_id = $2`, first.ID, accountID, now)
			require.NoError(t, err)
			before, err := repo.ListStates(ctx, first.ID)
			require.NoError(t, err)
			called := false
			action, err := repo.ApplyAccountVerdict(ctx, first.ID, accountID, "ok", false, true, now, func(actionCtx context.Context, value bool) error {
				called = true
				return accounts.SetSchedulable(actionCtx, accountID, value)
			})
			require.NoError(t, err)
			require.Equal(t, test.blocked, action == "resume_blocked")
			require.Equal(t, !test.blocked, called)
			states, err := repo.ListStates(ctx, first.ID)
			require.NoError(t, err)
			require.Len(t, states, 1)
			require.Equal(t, "ok", states[0].State, "passing result must be visible even when another plan blocks resume")
			require.Equal(t, before[0].LastAlertAt, states[0].LastAlertAt, "resume must not reset notification cooldown")
			require.True(t, before[0].UpdatedAt.Equal(states[0].UpdatedAt))
			require.Equal(t, test.blocked, states[0].AutoPaused)
			other, err := repo.ListStates(ctx, second.ID)
			require.NoError(t, err)
			require.Equal(t, test.blocked && test.otherPaused, other[0].AutoPaused)
			if !test.blocked {
				require.Nil(t, states[0].AutoPausedAt)
				require.Nil(t, other[0].AutoPausedAt)
			}
		})
	}
}

func TestScheduledQuestionTestRepositoryIntegrationManualPauseResumesWithOutbox(t *testing.T) {
	ctx := context.Background()
	repo, plan := newScheduledQuestionTestIntegrationPlan(t)
	_, other := newScheduledQuestionTestIntegrationPlan(t)
	account, accounts := newScheduledQuestionVerdictIntegrationAccount(t, false)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.SetAutoPaused(ctx, other.ID, account.ID, true, &now))
	action, err := repo.ApplyAccountVerdict(ctx, plan.ID, account.ID, "ok", false, true, now, func(actionCtx context.Context, value bool) error {
		return accounts.SetSchedulable(actionCtx, account.ID, value)
	})
	require.NoError(t, err)
	require.Equal(t, "resumed", action, "manual pause and missing ownership must not block a passing account")
	loaded, err := accounts.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.True(t, loaded.Schedulable)
	var events, markers int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_outbox
		WHERE account_id = $1 AND event_type = $2`, account.ID, service.SchedulerOutboxEventAccountChanged).Scan(&events))
	require.Equal(t, 1, events, "resume callback must use the production account repository/outbox path")
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduled_question_test_account_states
		WHERE account_id = $1 AND (auto_paused OR auto_paused_at IS NOT NULL)`, account.ID).Scan(&markers))
	require.Zero(t, markers, "successful resume clears every plan's historical marker")
	states, err := repo.ListStates(ctx, plan.ID)
	require.NoError(t, err)
	require.Len(t, states, 1)
	require.Equal(t, "ok", states[0].State)
}

func TestScheduledQuestionTestRepositoryIntegrationSchedulingCallbackRollback(t *testing.T) {
	for _, method := range []string{"pause", "resume"} {
		t.Run(method, func(t *testing.T) {
			ctx := context.Background()
			repo, plan := newScheduledQuestionTestIntegrationPlan(t)
			account, _ := newScheduledQuestionVerdictIntegrationAccount(t, method == "pause")
			accountID := account.ID
			now := time.Now().UTC().Truncate(time.Microsecond)
			if method == "resume" {
				require.NoError(t, repo.SetAutoPaused(ctx, plan.ID, accountID, true, &now))
			}
			before, err := repo.ListStates(ctx, plan.ID)
			require.NoError(t, err)
			failure := errors.New("account update failed")
			callback := func(context.Context, bool) error { return failure }
			state := "ok"
			if method == "pause" {
				state = "alert"
			}
			_, err = repo.ApplyAccountVerdict(ctx, plan.ID, accountID, state, method == "pause", method == "resume", now, callback)
			require.ErrorIs(t, err, failure)
			after, err := repo.ListStates(ctx, plan.ID)
			require.NoError(t, err)
			require.Equal(t, before, after, "failed scheduling must roll back the state and markers")
		})
	}
}

func TestScheduledQuestionTestRepositoryIntegrationSchedulingCallbackHoldsAccountLock(t *testing.T) {
	for _, method := range []string{"pause", "resume"} {
		t.Run(method, func(t *testing.T) {
			ctx := context.Background()
			repo, plan := newScheduledQuestionTestIntegrationPlan(t)
			account, accounts := newScheduledQuestionVerdictIntegrationAccount(t, method == "pause")
			accountID := account.ID
			callback := func(actionCtx context.Context, value bool) error {
				probe, err := integrationDB.BeginTx(actionCtx, nil)
				if err != nil {
					return err
				}
				defer func() { _ = probe.Rollback() }()
				var acquired bool
				err = probe.QueryRowContext(actionCtx, `SELECT pg_try_advisory_xact_lock(hashtextextended('scheduled-question-auto-pause:' || $1::text, 0))`, accountID).Scan(&acquired)
				require.NoError(t, err)
				require.False(t, acquired, "decision and account update must share the same account lock")
				if err != nil {
					return err
				}
				return accounts.SetSchedulable(actionCtx, accountID, value)
			}
			state := "ok"
			if method == "pause" {
				state = "alert"
			}
			action, err := repo.ApplyAccountVerdict(ctx, plan.ID, accountID, state, method == "pause", method == "resume", time.Now(), callback)
			require.NoError(t, err)
			if method == "pause" {
				require.Equal(t, "paused", action)
			} else {
				require.Equal(t, "resumed", action)
			}

		})
	}
}

func TestScheduledQuestionTestRepositoryIntegrationVerdictNotificationBarrier(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(fmt.Sprintf("before_%t_current_false", before), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			repo, failing := newScheduledQuestionTestIntegrationPlan(t)
			_, passing := newScheduledQuestionTestIntegrationPlan(t)
			account, accounts := newScheduledQuestionVerdictIntegrationAccount(t, before)
			now := time.Now().UTC().Truncate(time.Microsecond)
			for _, planID := range []int64{failing.ID, passing.ID} {
				_, err := integrationDB.ExecContext(ctx, `UPDATE scheduled_question_test_plans SET auto_pause_enabled = true WHERE id = $1`, planID)
				require.NoError(t, err)
			}
			// Model the admin change during upstream execution, independently of before.
			_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET schedulable = false WHERE id = $1`, account.ID)
			require.NoError(t, err)
			entered, release := make(chan struct{}), make(chan struct{})
			t.Cleanup(func() {
				select {
				case <-release:
				default:
					close(release)
				}
			})
			type result struct {
				action string
				err    error
			}
			done := make(chan result, 1)
			go func() {
				action, verdictErr := repo.ApplyAccountVerdict(ctx, failing.ID, account.ID, "alert", true, false, now, func(actionCtx context.Context, value bool) error {
					return accounts.SetSchedulable(actionCtx, account.ID, value)
				})
				if verdictErr != nil {
					done <- result{action, verdictErr}
					return
				}
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					done <- result{action, ctx.Err()}
					return
				}
				_, finishErr := repo.FinishRun(ctx, &service.ScheduledQuestionTestRun{
					PlanID: failing.ID, Status: "success", StartedAt: now, FinishedAt: now, TotalAccounts: 1,
				}, []*service.ScheduledQuestionTestAccountState{{AccountID: account.ID, State: "alert"}}, now.Add(time.Hour))
				done <- result{action, finishErr}
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("verdict did not reach notification barrier: ", ctx.Err())
			}
			states, err := repo.ListStates(ctx, failing.ID)
			require.NoError(t, err)
			require.Len(t, states, 1)
			require.Equal(t, "alert", states[0].State, "failure without a pause must be committed before notifications")
			action, err := repo.ApplyAccountVerdict(ctx, passing.ID, account.ID, "ok", false, true, now, func(actionCtx context.Context, value bool) error {
				return accounts.SetSchedulable(actionCtx, account.ID, value)
			})
			require.NoError(t, err)
			require.Equal(t, "resume_blocked", action)
			_, err = repo.FinishRun(ctx, &service.ScheduledQuestionTestRun{
				PlanID: passing.ID, Status: "success", StartedAt: now, FinishedAt: now, TotalAccounts: 1,
			}, []*service.ScheduledQuestionTestAccountState{{AccountID: account.ID, State: "ok"}}, now.Add(time.Hour))
			require.NoError(t, err)
			close(release)
			finished := <-done
			require.NoError(t, finished.err)
			require.Empty(t, finished.action)
			loaded, err := accounts.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.False(t, loaded.Schedulable, "another plan's published failure must remain authoritative after both finishes")
		})
	}
}

func TestScheduledQuestionTestRepositoryIntegrationVerdictCurrentSchedulingWins(t *testing.T) {
	ctx := context.Background()
	repo, plan := newScheduledQuestionTestIntegrationPlan(t)
	account, accounts := newScheduledQuestionVerdictIntegrationAccount(t, true)
	// This account may have been closed at round start; only current scheduling
	// participates in the repository decision after upstream work finishes.
	action, err := repo.ApplyAccountVerdict(ctx, plan.ID, account.ID, "alert", true, false, time.Now(), func(actionCtx context.Context, value bool) error {
		return accounts.SetSchedulable(actionCtx, account.ID, value)
	})
	require.NoError(t, err)
	require.Equal(t, "paused", action)
	loaded, err := accounts.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.False(t, loaded.Schedulable)
}

func TestScheduledQuestionTestRepositoryIntegrationVerdictAtomicRollback(t *testing.T) {
	for _, stage := range []string{"pause_marker", "resume_clear", "pause_outbox", "resume_outbox", "callback_after_outbox", "pause_commit", "resume_commit"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			repo, plan := newScheduledQuestionTestIntegrationPlan(t)
			pause := stage == "pause_marker" || stage == "pause_outbox" || stage == "callback_after_outbox" || stage == "pause_commit"
			account, accounts := newScheduledQuestionVerdictIntegrationAccount(t, pause)
			now := time.Now().UTC().Truncate(time.Microsecond)
			require.NoError(t, repo.SetAutoPaused(ctx, plan.ID, account.ID, !pause, &now))
			initialState := "alert"
			if pause {
				initialState = "ok"
			}
			_, err := integrationDB.ExecContext(ctx, `UPDATE scheduled_question_test_account_states SET state = $3, last_alert_at = $4 WHERE plan_id = $1 AND account_id = $2`, plan.ID, account.ID, initialState, now)
			require.NoError(t, err)
			before, err := repo.ListStates(ctx, plan.ID)
			require.NoError(t, err)
			if stage != "callback_after_outbox" {
				functionName := fmt.Sprintf("sqt_verdict_fail_%d", plan.ID)
				table, operation := "scheduled_question_test_account_states", "UPDATE"
				condition := fmt.Sprintf("NEW.plan_id = %d AND NEW.account_id = %d AND NEW.auto_paused IS DISTINCT FROM OLD.auto_paused", plan.ID, account.ID)
				if stage == "pause_outbox" || stage == "resume_outbox" {
					table, operation = "scheduler_outbox", "INSERT"
					condition = fmt.Sprintf("NEW.account_id = %d", account.ID)
				}
				_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN IF %s THEN RAISE EXCEPTION 'injected verdict failure'; END IF; RETURN NEW; END $$`, functionName, condition))
				require.NoError(t, err)
				t.Cleanup(func() {
					_, cleanupErr := integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP FUNCTION %s() CASCADE", functionName))
					require.NoError(t, cleanupErr)
				})
				trigger := fmt.Sprintf(`CREATE TRIGGER %s_trigger BEFORE %s ON %s FOR EACH ROW EXECUTE FUNCTION %s()`, functionName, operation, table, functionName)
				if stage == "pause_commit" || stage == "resume_commit" {
					trigger = fmt.Sprintf(`CREATE CONSTRAINT TRIGGER %s_trigger AFTER UPDATE ON %s DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION %s()`, functionName, table, functionName)
				}
				_, err = integrationDB.ExecContext(ctx, trigger)
				require.NoError(t, err)
			}
			state := "ok"
			if pause {
				state = "alert"
			}
			action, err := repo.ApplyAccountVerdict(ctx, plan.ID, account.ID, state, pause, !pause, now, func(actionCtx context.Context, value bool) error {
				if err := accounts.SetSchedulable(actionCtx, account.ID, value); err != nil {
					return err
				}
				if stage == "callback_after_outbox" {
					return errors.New("injected failure after account and outbox writes")
				}
				return nil
			})
			require.Error(t, err)
			require.Empty(t, action)
			loaded, err := accounts.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.Equal(t, pause, loaded.Schedulable, "account update must roll back with marker/outbox failure")
			after, err := repo.ListStates(ctx, plan.ID)
			require.NoError(t, err)
			require.Equal(t, before, after, "state, cooldown and marker must all roll back")
			var events int
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id = $1", account.ID).Scan(&events))
			require.Zero(t, events, "outbox must roll back with account and state")
		})
	}
}

func TestScheduledQuestionTestRepositoryIntegrationFinishRunFirstClaimNoDeadlock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo, plan := newScheduledQuestionTestIntegrationPlan(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	gateKey := -930000000000 - plan.ID
	gate, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = gate.Rollback() }()
	var gatePID int
	require.NoError(t, gate.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&gatePID))
	_, err = gate.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, gateKey)
	require.NoError(t, err)
	functionName := fmt.Sprintf("sqt_finish_claim_%d", plan.ID)
	triggerName := functionName + "_trigger"
	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
	BEGIN
		IF NEW.plan_id = %d THEN PERFORM pg_advisory_xact_lock(%d); END IF;
		RETURN NEW;
	END $$`, functionName, plan.ID, gateKey))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := integrationDB.ExecContext(context.Background(), fmt.Sprintf(`DROP FUNCTION %s() CASCADE`, functionName))
		require.NoError(t, cleanupErr)
	})
	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON scheduled_question_test_runs
		FOR EACH ROW EXECUTE FUNCTION %s()`, triggerName, functionName))
	require.NoError(t, err)
	finished := make(chan error, 1)
	go func() {
		_, finishErr := repo.FinishRun(ctx, &service.ScheduledQuestionTestRun{
			PlanID: plan.ID, CronExpression: plan.CronExpression, PlanUpdatedAt: plan.UpdatedAt,
			Status: "success", StartedAt: now, FinishedAt: now.Add(time.Minute), TotalAccounts: 1,
		}, []*service.ScheduledQuestionTestAccountState{{AccountID: 910001, State: "alert"}}, now.Add(time.Hour))
		finished <- finishErr
	}()
	// Pause FinishRun after its account and parent locks, before its first state insert.
	require.Eventually(t, func() bool {
		var blocked bool
		queryErr := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity WHERE $1::integer = ANY(pg_blocking_pids(pid)) AND wait_event = 'advisory'
		)`, gatePID).Scan(&blocked)
		return queryErr == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
	var finishPID int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT pid FROM pg_stat_activity
		WHERE $1::integer = ANY(pg_blocking_pids(pid)) AND wait_event = 'advisory'`, gatePID).Scan(&finishPID))
	claimed := make(chan error, 1)
	go func() { claimed <- repo.SetAutoPaused(ctx, plan.ID, 910001, true, &now) }()
	require.Eventually(t, func() bool {
		var waiting bool
		err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE $1::integer = ANY(pg_blocking_pids(pid)) AND wait_event = 'advisory')`, finishPID).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond, "first claim must wait behind FinishRun's account lock")
	require.NoError(t, gate.Commit())
	require.NoError(t, <-finished)
	require.NoError(t, <-claimed)
	states, err := repo.ListStates(ctx, plan.ID)
	require.NoError(t, err)
	require.Len(t, states, 1)
	require.True(t, states[0].AutoPaused)
	runs, err := repo.ListRuns(ctx, plan.ID, 50)
	require.NoError(t, err)
	require.Len(t, runs, 1)
}

func TestScheduledQuestionTestRepositoryIntegrationFinishRunDeleteLockOrder(t *testing.T) {
	for _, first := range []string{"finish", "delete"} {
		t.Run(first+"_first", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			repo := NewScheduledQuestionTestRepository(integrationDB)
			plan, err := repo.Create(ctx, &service.ScheduledQuestionTestPlan{
				Name: t.Name(), AccountIDs: []int64{910001}, QuestionIDs: []int64{920001},
				CronExpression: "0 * * * *", Enabled: true,
			})
			require.NoError(t, err)
			t.Cleanup(func() {
				_, cleanupErr := integrationDB.ExecContext(context.Background(), "DELETE FROM scheduled_question_test_plans WHERE id = $1", plan.ID)
				require.NoError(t, cleanupErr)
			})
			now := time.Now().UTC().Truncate(time.Microsecond)
			require.NoError(t, repo.SetAutoPaused(ctx, plan.ID, 910001, true, &now))
			gate, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = gate.Rollback() }()
			gateKey := -940000000000 - plan.ID
			var gatePID int
			require.NoError(t, gate.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&gatePID))
			_, err = gate.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", gateKey)
			require.NoError(t, err)
			functionName := fmt.Sprintf("sqt_delete_order_%d", plan.ID)
			table, operation, condition, returned := "scheduled_question_test_runs", "INSERT", "NEW.plan_id", "NEW"
			if first == "delete" {
				table, operation, condition, returned = "scheduled_question_test_plans", "DELETE", "OLD.id", "OLD"
			}
			_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN
				IF %s = %d THEN PERFORM pg_advisory_xact_lock(%d); END IF;
				RETURN %s;
			END $$`, functionName, condition, plan.ID, gateKey, returned))
			require.NoError(t, err)
			t.Cleanup(func() {
				_, cleanupErr := integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP FUNCTION %s() CASCADE", functionName))
				require.NoError(t, cleanupErr)
			})
			_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`CREATE TRIGGER %s_trigger BEFORE %s ON %s
				FOR EACH ROW EXECUTE FUNCTION %s()`, functionName, operation, table, functionName))
			require.NoError(t, err)
			type finishResult struct {
				run *service.ScheduledQuestionTestRun
				err error
			}
			finished, deleted := make(chan finishResult, 1), make(chan error, 1)
			finish := func() {
				run, finishErr := repo.FinishRun(ctx, &service.ScheduledQuestionTestRun{
					PlanID: plan.ID, CronExpression: plan.CronExpression, PlanUpdatedAt: plan.UpdatedAt,
					Status: "success", StartedAt: now, FinishedAt: now, TotalAccounts: 1,
				}, []*service.ScheduledQuestionTestAccountState{{AccountID: 910001, State: "ok"}}, now.Add(time.Hour))
				finished <- finishResult{run, finishErr}
			}
			del := func() { deleted <- repo.Delete(ctx, plan.ID) }
			if first == "finish" {
				go finish()
			} else {
				go del()
			}
			var firstPID int
			require.Eventually(t, func() bool {
				err := integrationDB.QueryRowContext(ctx, `SELECT pid FROM pg_stat_activity
					WHERE $1::integer = ANY(pg_blocking_pids(pid)) AND wait_event = 'advisory'`, gatePID).Scan(&firstPID)
				return err == nil
			}, 3*time.Second, 10*time.Millisecond)
			if first == "finish" {
				go del()
			} else {
				go finish()
			}
			require.Eventually(t, func() bool {
				var waiting bool
				err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
					WHERE $1::integer = ANY(pg_blocking_pids(pid)) AND wait_event = 'advisory')`, firstPID).Scan(&waiting)
				return err == nil && waiting
			}, 3*time.Second, 10*time.Millisecond, "second transaction must wait at the account advisory lock, not form a parent/state cycle")
			require.NoError(t, gate.Commit())
			require.NoError(t, <-deleted)
			result := <-finished
			if first == "finish" {
				require.NoError(t, result.err)
				require.Positive(t, result.run.ID, "finish must save its round before deletion cascades")
			} else {
				require.ErrorIs(t, result.err, service.ErrScheduledQuestionTestNotFound)
				require.Nil(t, result.run)
			}
			for _, childTable := range []string{"scheduled_question_test_runs", "scheduled_question_test_account_states"} {
				var remaining int
				require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+childTable+" WHERE plan_id = $1", plan.ID).Scan(&remaining))
				require.Zero(t, remaining, "deletion must leave no orphan records")
			}
		})
	}
}

func TestScheduledQuestionTestRepositoryIntegrationConcurrentResume(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo, first := newScheduledQuestionTestIntegrationPlan(t)
	_, second := newScheduledQuestionTestIntegrationPlan(t)
	account, accounts := newScheduledQuestionVerdictIntegrationAccount(t, false)
	accountID := account.ID
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, planID := range []int64{first.ID, second.ID} {
		_, err := integrationDB.ExecContext(ctx, `UPDATE scheduled_question_test_plans SET auto_pause_enabled = true WHERE id = $1`, planID)
		require.NoError(t, err)
		require.NoError(t, repo.SetAutoPaused(ctx, planID, accountID, true, &now))
	}
	type result struct {
		blocked bool
		err     error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for _, planID := range []int64{first.ID, second.ID} {
		go func(id int64) {
			<-start
			action, err := repo.ApplyAccountVerdict(ctx, id, accountID, "ok", false, true, now, func(actionCtx context.Context, value bool) error {
				return accounts.SetSchedulable(actionCtx, accountID, value)
			})
			results <- result{action == "resume_blocked", err}
		}(planID)
	}
	close(start)
	blockedCount := 0
	for range 2 {
		got := <-results
		require.NoError(t, got.err)
		if got.blocked {
			blockedCount++
		}
	}
	require.Equal(t, 1, blockedCount, "first passing plan remains blocked; second sees both committed passing states")
	for _, planID := range []int64{first.ID, second.ID} {
		states, err := repo.ListStates(ctx, planID)
		require.NoError(t, err)
		require.Len(t, states, 1)
		require.Equal(t, "ok", states[0].State)
		require.False(t, states[0].AutoPaused)
		require.Nil(t, states[0].AutoPausedAt)
	}
}

func TestScheduledQuestionTestRepositoryIntegrationFinishRunPreservesOwnership(t *testing.T) {
	ctx := context.Background()
	repo, plan := newScheduledQuestionTestIntegrationPlan(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.SetAutoPaused(ctx, plan.ID, 910001, true, &now))
	_, err := integrationDB.ExecContext(ctx, `UPDATE scheduled_question_test_account_states
		SET auto_paused = false, auto_paused_at = NULL WHERE account_id = $1`, int64(910001))
	require.NoError(t, err)
	require.NoError(t, repo.SetAutoPaused(ctx, plan.ID, 910002, true, &now))
	_, err = repo.FinishRun(ctx, &service.ScheduledQuestionTestRun{
		PlanID: plan.ID, CronExpression: plan.CronExpression, PlanUpdatedAt: plan.UpdatedAt,
		Status: "success", StartedAt: now, FinishedAt: now.Add(time.Minute), TotalAccounts: 3,
		Summary: []service.ScheduledQuestionTestAccountSummary{{AccountID: 910001, AutoPaused: true}},
	}, []*service.ScheduledQuestionTestAccountState{
		{AccountID: 910001, State: "alert", AutoPaused: true, AutoPausedAt: &now},
		{AccountID: 910002, State: "ok", AutoPaused: false},
		{AccountID: 910003, State: "alert", AutoPaused: true, AutoPausedAt: &now},
	}, now.Add(time.Hour))
	require.NoError(t, err)
	states, err := repo.ListStates(ctx, plan.ID)
	require.NoError(t, err)
	require.Len(t, states, 3)
	require.False(t, states[0].AutoPaused, "an old round must not revive a manually released marker")
	require.Nil(t, states[0].AutoPausedAt)
	require.True(t, states[1].AutoPaused, "an old round must not clear a new claim")
	require.NotNil(t, states[1].AutoPausedAt)
	require.True(t, states[1].AutoPausedAt.Equal(now))
	require.False(t, states[2].AutoPaused, "new alert states use the database defaults")
	require.Nil(t, states[2].AutoPausedAt)
	runs, err := repo.ListRuns(ctx, plan.ID, 50)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.True(t, runs[0].Summary[0].AutoPaused, "the summary remains an execution-time snapshot")
}

func TestScheduledQuestionTestRepositoryIntegrationSkippedStateUntouched(t *testing.T) {
	ctx := context.Background()
	repo, plan := newScheduledQuestionTestIntegrationPlan(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.SetAutoPaused(ctx, plan.ID, 910001, true, &now))
	_, err := integrationDB.ExecContext(ctx, `UPDATE scheduled_question_test_account_states
		SET last_alert_at = $3 WHERE plan_id = $1 AND account_id = $2`, plan.ID, int64(910001), now)
	require.NoError(t, err)
	before, err := repo.ListStates(ctx, plan.ID)
	require.NoError(t, err)
	_, err = repo.FinishRun(ctx, &service.ScheduledQuestionTestRun{
		PlanID: plan.ID, CronExpression: plan.CronExpression, PlanUpdatedAt: plan.UpdatedAt,
		Status: "success", StartedAt: now, FinishedAt: now.Add(time.Minute), TotalAccounts: 1, SkippedAccounts: 1,
		Summary: []service.ScheduledQuestionTestAccountSummary{{AccountID: 910001, Result: "skipped", SkipReason: "quota_exceeded"}},
	}, nil, now.Add(time.Hour))
	require.NoError(t, err)
	after, err := repo.ListStates(ctx, plan.ID)
	require.NoError(t, err)
	require.Equal(t, before, after, "skipped account omitted from states must retain state, cooldown, timestamps, and pause markers")
}

func TestScheduledQuestionTestRepositoryIntegrationOwnershipSurvivesFinishRunFailure(t *testing.T) {
	for _, stage := range []string{"run_insert", "state_upsert"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			repo, plan := newScheduledQuestionTestIntegrationPlan(t)
			now := time.Now().UTC().Truncate(time.Microsecond)
			require.NoError(t, repo.SetAutoPaused(ctx, plan.ID, 910001, true, &now))
			run := &service.ScheduledQuestionTestRun{
				PlanID: plan.ID, CronExpression: plan.CronExpression, PlanUpdatedAt: plan.UpdatedAt,
				Status: "success", StartedAt: now, FinishedAt: now.Add(time.Minute), TotalAccounts: 1,
			}
			states := []*service.ScheduledQuestionTestAccountState{{AccountID: 910001, State: "ok"}}
			if stage == "run_insert" {
				run.Status = "invalid"
			} else {
				// Fail after an earlier alert-state update in the same transaction.
				states = append(states, &service.ScheduledQuestionTestAccountState{AccountID: 0, State: "ok"})
			}
			_, err := repo.FinishRun(ctx, run, states, now.Add(time.Hour))
			require.Error(t, err)
			storedStates, err := repo.ListStates(ctx, plan.ID)
			require.NoError(t, err)
			require.Len(t, storedStates, 1)
			require.True(t, storedStates[0].AutoPaused)
			require.NotNil(t, storedStates[0].AutoPausedAt)
			require.True(t, storedStates[0].AutoPausedAt.Equal(now))
			require.Equal(t, "alert", storedStates[0].State)
			runs, err := repo.ListRuns(ctx, plan.ID, 50)
			require.NoError(t, err)
			require.Empty(t, runs)
			storedPlan, err := repo.GetByID(ctx, plan.ID)
			require.NoError(t, err)
			require.Nil(t, storedPlan.LastRunAt)
			require.Nil(t, storedPlan.NextRunAt)
		})
	}
}

func TestScheduledQuestionTestRepositoryIntegrationSkippedAccountsConstraint(t *testing.T) {
	ctx := context.Background()
	_, plan := newScheduledQuestionTestIntegrationPlan(t)
	var runID int64
	var skipped int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO scheduled_question_test_runs
		(plan_id, status, started_at, finished_at, total_accounts, passed_accounts, alerted_accounts)
		VALUES ($1, 'success', NOW(), NOW(), 0, 0, 0)
		RETURNING id, skipped_accounts`, plan.ID).Scan(&runID, &skipped))
	require.Zero(t, skipped, "old writers must receive the migration's zero default")
	_, err := integrationDB.ExecContext(ctx, `UPDATE scheduled_question_test_runs SET skipped_accounts = -1 WHERE id = $1`, runID)
	var pgError *pq.Error
	require.ErrorAs(t, err, &pgError)
	require.Equal(t, pq.ErrorCode("23514"), pgError.Code)
}

func TestScheduledQuestionTestRepositoryIntegrationFeishuRequiredConstraint(t *testing.T) {
	ctx := context.Background()
	repo := NewScheduledQuestionTestRepository(integrationDB)
	_, err := repo.Create(ctx, &service.ScheduledQuestionTestPlan{
		Name: t.Name(), AccountIDs: []int64{910001}, QuestionIDs: []int64{920001},
		CronExpression: "0 * * * *", FeishuEnabled: true,
	})
	var validation *model.ValidationError
	require.ErrorAs(t, err, &validation)
	require.Equal(t, "feishu_webhook_url", validation.Field)

	for _, firstUpdate := range []string{"clear", "enable", "simultaneous"} {
		t.Run(firstUpdate+"_first", func(t *testing.T) {
			repo, plan := newScheduledQuestionTestIntegrationPlan(t)
			plan.FeishuWebhookURL = "https://open.feishu.cn/open-apis/bot/v2/hook/concurrent-token"
			plan, err := repo.Update(ctx, plan, service.ScheduledQuestionTestPlanUpdateMask{FeishuWebhookURL: true})
			require.NoError(t, err)
			clearSnapshot, err := repo.GetByID(ctx, plan.ID)
			require.NoError(t, err)
			enableSnapshot, err := repo.GetByID(ctx, plan.ID)
			require.NoError(t, err)
			clearSnapshot.FeishuWebhookURL = ""
			enableSnapshot.FeishuEnabled = true
			// Both requests validate the same old row; force each commit order.
			firstDone := make(chan struct{})
			if firstUpdate == "simultaneous" {
				close(firstDone)
			}
			results := make(chan error, 2)
			go func() {
				if firstUpdate != "clear" {
					<-firstDone
				}
				_, updateErr := repo.Update(ctx, clearSnapshot, service.ScheduledQuestionTestPlanUpdateMask{FeishuWebhookURL: true})
				if firstUpdate == "clear" {
					close(firstDone)
				}
				results <- updateErr
			}()
			go func() {
				if firstUpdate != "enable" {
					<-firstDone
				}
				_, updateErr := repo.Update(ctx, enableSnapshot, service.ScheduledQuestionTestPlanUpdateMask{FeishuEnabled: true})
				if firstUpdate == "enable" {
					close(firstDone)
				}
				results <- updateErr
			}()
			var successes, failures int
			for range 2 {
				updateErr := <-results
				if updateErr == nil {
					successes++
					continue
				}
				failures++
				var fieldError *model.ValidationError
				require.ErrorAs(t, updateErr, &fieldError)
				require.Equal(t, "feishu_webhook_url", fieldError.Field)
			}
			require.Equal(t, 1, successes)
			require.Equal(t, 1, failures)
			stored, err := repo.GetByID(ctx, plan.ID)
			require.NoError(t, err)
			if firstUpdate == "clear" {
				require.False(t, stored.FeishuEnabled)
				require.Empty(t, stored.FeishuWebhookURL)
			} else if firstUpdate == "enable" {
				require.True(t, stored.FeishuEnabled)
				require.Equal(t, plan.FeishuWebhookURL, stored.FeishuWebhookURL)
			} else {
				require.True(t, !stored.FeishuEnabled || stored.FeishuWebhookURL != "")
			}
		})
	}
}

func newScheduledQuestionTestIntegrationPlan(t *testing.T) (service.ScheduledQuestionTestRepository, *service.ScheduledQuestionTestPlan) {
	t.Helper()
	repo := NewScheduledQuestionTestRepository(integrationDB)
	plan, err := repo.Create(context.Background(), &service.ScheduledQuestionTestPlan{
		Name: t.Name(), AccountIDs: []int64{910001}, QuestionIDs: []int64{920001},
		CronExpression: "0 * * * *", Enabled: true, MinPassRatio: 80,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, repo.Delete(context.Background(), plan.ID))
	})
	return repo, plan
}
