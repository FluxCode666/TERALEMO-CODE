//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestScheduledQuestionTestRepositoryIntegrationLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := NewScheduledQuestionTestRepository(integrationDB)
	now := time.Now().UTC().Truncate(time.Microsecond)
	next := now.Add(time.Hour)
	plan, err := repo.Create(ctx, &service.ScheduledQuestionTestPlan{
		Name: t.Name(), AccountIDs: []int64{910001, 910002}, QuestionIDs: []int64{920001},
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
		TotalAccounts: 2, PassedAccounts: 1, AlertedAccounts: 1, PausedAccounts: 1, ResumedAccounts: 1,
		Summary: []service.ScheduledQuestionTestAccountSummary{
			{AccountID: 910001, AccountName: "paused-account", Total: 1, Judged: 1, Ratio: &failedRatio, Alerting: true, StateAfter: "alert", ScheduleAction: "paused", AutoPaused: true},
			{AccountID: 910002, AccountName: "resumed-account", Passed: 1, Total: 1, Judged: 1, Ratio: &passedRatio, StateAfter: "ok", ScheduleAction: "resumed"},
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

	resume, err := repo.ReleaseAutoPause(ctx, plan.ID, 910001)
	require.NoError(t, err)
	require.True(t, resume)
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
	var storedSummary, storedNotifications string
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT summary::text, notification_results::text FROM scheduled_question_test_runs WHERE id = $1`, emptyRun.ID,
	).Scan(&storedSummary, &storedNotifications))
	require.JSONEq(t, "[]", storedSummary)
	require.JSONEq(t, "[]", storedNotifications)
}

func TestScheduledQuestionTestRepositoryIntegrationAutoPauseOwnership(t *testing.T) {
	ctx := context.Background()
	repo, first := newScheduledQuestionTestIntegrationPlan(t)
	_, second := newScheduledQuestionTestIntegrationPlan(t)
	const accountID int64 = 910001
	now := time.Now().UTC().Truncate(time.Microsecond)
	resume, err := repo.ReleaseAutoPause(ctx, first.ID, accountID)
	require.NoError(t, err)
	require.False(t, resume)
	require.NoError(t, repo.SetAutoPaused(ctx, first.ID, accountID, true, &now))
	states, err := repo.ListStates(ctx, first.ID)
	require.NoError(t, err)
	require.Len(t, states, 1)
	require.Equal(t, "alert", states[0].State)
	require.Nil(t, states[0].LastAlertAt)
	require.True(t, states[0].AutoPaused)
	require.NotNil(t, states[0].AutoPausedAt)
	require.True(t, states[0].AutoPausedAt.Equal(now))

	lastAlertAt := now.Add(-time.Hour)
	_, err = integrationDB.ExecContext(ctx, `UPDATE scheduled_question_test_account_states
		SET state = 'ok', last_alert_at = $3 WHERE plan_id = $1 AND account_id = $2`, first.ID, accountID, lastAlertAt)
	require.NoError(t, err)
	states, err = repo.ListStates(ctx, first.ID)
	require.NoError(t, err)
	updatedAt := states[0].UpdatedAt
	newPauseAt := now.Add(time.Minute)
	require.NoError(t, repo.SetAutoPaused(ctx, first.ID, accountID, true, &newPauseAt))
	states, err = repo.ListStates(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, "ok", states[0].State)
	require.NotNil(t, states[0].LastAlertAt)
	require.True(t, states[0].LastAlertAt.Equal(lastAlertAt))
	require.True(t, states[0].UpdatedAt.Equal(updatedAt))
	require.True(t, states[0].AutoPausedAt.Equal(newPauseAt))

	require.NoError(t, repo.SetAutoPaused(ctx, second.ID, accountID, true, &now))
	resume, err = repo.ReleaseAutoPause(ctx, first.ID, accountID)
	require.NoError(t, err)
	require.False(t, resume, "another plan still owns the pause")
	states, err = repo.ListStates(ctx, first.ID)
	require.NoError(t, err)
	require.False(t, states[0].AutoPaused)
	require.Nil(t, states[0].AutoPausedAt)
	require.Equal(t, "ok", states[0].State)
	require.True(t, states[0].LastAlertAt.Equal(lastAlertAt))
	require.True(t, states[0].UpdatedAt.Equal(updatedAt))
	resume, err = repo.ReleaseAutoPause(ctx, second.ID, accountID)
	require.NoError(t, err)
	require.True(t, resume, "the last owner restores scheduling")
	resume, err = repo.ReleaseAutoPause(ctx, second.ID, accountID)
	require.NoError(t, err)
	require.False(t, resume, "an already released marker must not restore scheduling again")
	require.NoError(t, repo.SetAutoPaused(ctx, first.ID, accountID, true, &now))
	require.NoError(t, repo.SetAutoPaused(ctx, first.ID, accountID, false, &now))
	states, err = repo.ListStates(ctx, first.ID)
	require.NoError(t, err)
	require.False(t, states[0].AutoPaused)
	require.Nil(t, states[0].AutoPausedAt)
}

func TestScheduledQuestionTestRepositoryIntegrationJoinOtherPause(t *testing.T) {
	ctx := context.Background()
	repo, first := newScheduledQuestionTestIntegrationPlan(t)
	_, second := newScheduledQuestionTestIntegrationPlan(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	claimed, err := repo.ClaimAutoPauseIfOtherHeld(ctx, second.ID, 910001, &now)
	require.NoError(t, err)
	require.False(t, claimed, "a manual pause with no holder must not be claimed")
	states, err := repo.ListStates(ctx, second.ID)
	require.NoError(t, err)
	require.Empty(t, states)
	require.NoError(t, repo.SetAutoPaused(ctx, first.ID, 910001, true, &now))
	claimed, err = repo.ClaimAutoPauseIfOtherHeld(ctx, second.ID, 910001, &now)
	require.NoError(t, err)
	require.True(t, claimed)
	states, err = repo.ListStates(ctx, second.ID)
	require.NoError(t, err)
	require.Len(t, states, 1)
	require.True(t, states[0].AutoPaused)
	require.Equal(t, "alert", states[0].State)
	firstPauseAt := *states[0].AutoPausedAt
	later := now.Add(time.Hour)
	claimed, err = repo.ClaimAutoPauseIfOtherHeld(ctx, second.ID, 910001, &later)
	require.NoError(t, err)
	require.True(t, claimed)
	states, err = repo.ListStates(ctx, second.ID)
	require.NoError(t, err)
	require.True(t, states[0].AutoPausedAt.Equal(firstPauseAt), "an existing claim is not rewritten")
	resume, err := repo.ReleaseAutoPause(ctx, first.ID, 910001)
	require.NoError(t, err)
	require.False(t, resume)
	resume, err = repo.ReleaseAutoPause(ctx, second.ID, 910001)
	require.NoError(t, err)
	require.True(t, resume)
}

func TestScheduledQuestionTestRepositoryIntegrationJoinAfterManualClear(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo, first := newScheduledQuestionTestIntegrationPlan(t)
	_, second := newScheduledQuestionTestIntegrationPlan(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.SetAutoPaused(ctx, first.ID, 910001, true, &now))
	clearTx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = clearTx.Rollback() }()
	var clearPID int
	require.NoError(t, clearTx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&clearPID))
	manual := &accountRepository{sql: clearTx}
	require.NoError(t, manual.ClearScheduledQuestionAutoPause(ctx, 910001))
	type joinResult struct {
		claimed bool
		err     error
	}
	done := make(chan joinResult, 1)
	go func() {
		claimed, joinErr := repo.ClaimAutoPauseIfOtherHeld(ctx, second.ID, 910001, &now)
		done <- joinResult{claimed: claimed, err: joinErr}
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		queryErr := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity WHERE $1::integer = ANY(pg_blocking_pids(pid)) AND wait_event = 'advisory'
		)`, clearPID).Scan(&blocked)
		return queryErr == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, clearTx.Commit())
	result := <-done
	require.NoError(t, result.err)
	require.False(t, result.claimed, "joining must see the committed manual release")
	states, err := repo.ListStates(ctx, second.ID)
	require.NoError(t, err)
	require.Empty(t, states)
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
	// Pause FinishRun after it has locked the parent but before its first state insert.
	require.Eventually(t, func() bool {
		var blocked bool
		queryErr := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity WHERE $1::integer = ANY(pg_blocking_pids(pid)) AND wait_event = 'advisory'
		)`, gatePID).Scan(&blocked)
		return queryErr == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
	claimCtx, claimCancel := context.WithTimeout(ctx, 2*time.Second)
	defer claimCancel()
	require.NoError(t, repo.SetAutoPaused(claimCtx, plan.ID, 910001, true, &now), "the parent's lock must allow the first claim's foreign-key KEY SHARE")
	require.NoError(t, gate.Commit())
	require.NoError(t, <-finished)
	states, err := repo.ListStates(ctx, plan.ID)
	require.NoError(t, err)
	require.Len(t, states, 1)
	require.True(t, states[0].AutoPaused)
	runs, err := repo.ListRuns(ctx, plan.ID, 50)
	require.NoError(t, err)
	require.Len(t, runs, 1)
}

func TestScheduledQuestionTestRepositoryIntegrationConcurrentRelease(t *testing.T) {
	ctx := context.Background()
	repo, first := newScheduledQuestionTestIntegrationPlan(t)
	_, second := newScheduledQuestionTestIntegrationPlan(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	start := make(chan struct{})
	claims := make(chan error, 2)
	for _, planID := range []int64{first.ID, second.ID} {
		go func(id int64) {
			<-start
			claims <- repo.SetAutoPaused(ctx, id, 910001, true, &now)
		}(planID)
	}
	close(start)
	for range 2 {
		require.NoError(t, <-claims)
	}
	type result struct {
		resume bool
		err    error
	}
	releases := make(chan result, 2)
	start = make(chan struct{})
	for _, planID := range []int64{first.ID, second.ID} {
		go func(id int64) {
			<-start
			resume, err := repo.ReleaseAutoPause(ctx, id, 910001)
			releases <- result{resume, err}
		}(planID)
	}
	close(start)
	resumes := 0
	for range 2 {
		got := <-releases
		require.NoError(t, got.err)
		if got.resume {
			resumes++
		}
	}
	require.Equal(t, 1, resumes)
	for _, planID := range []int64{first.ID, second.ID} {
		states, err := repo.ListStates(ctx, planID)
		require.NoError(t, err)
		require.Len(t, states, 1)
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
