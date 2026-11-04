//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScheduledQuestionResumeRespectsOtherPlanOwnership(t *testing.T) {
	for _, tc := range []struct {
		name     string
		other    bool
		queryErr error
		action   string
		owned    bool
	}{
		{"other owner only releases this plan", true, nil, "", false},
		{"query failure preserves ownership", false, errors.New("database unavailable"), "resume_failed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, accounts, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			plan := scheduledQuestionValidPlan(1)
			plan.AutoResumeEnabled = true
			repo.plans[1] = plan
			repo.states = []*ScheduledQuestionTestAccountState{{PlanID: 1, AccountID: 2, AutoPaused: true, State: "alert", AutoPausedAt: scheduledQuestionPtr(svc.now())}}
			repo.otherOwner, repo.ownerErr = tc.other, tc.queryErr
			scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
			svc.accounts = scheduling
			run := &ScheduledQuestionTestRun{Summary: []ScheduledQuestionTestAccountSummary{{AccountID: 2, Ratio: scheduledQuestionPtr(100), Judged: 2, Total: 2}}}
			states, err := svc.updateAlertStates(context.Background(), plan, run)
			require.NoError(t, err)
			require.Empty(t, scheduling.actions)
			require.False(t, accounts.accounts[2].Schedulable)
			require.Equal(t, tc.action, run.Summary[0].ScheduleAction)
			require.Equal(t, tc.owned, states[0].AutoPaused)
			require.Equal(t, tc.owned, run.Summary[0].AutoPaused)
			require.Zero(t, run.ResumedAccounts)
		})
	}
}

type scheduledQuestionPersistBeforeActionRepo struct {
	*scheduledQuestionSchedulingRepo
	states *scheduledQuestionRepoStub
	t      *testing.T
}

func (r *scheduledQuestionPersistBeforeActionRepo) SetSchedulable(ctx context.Context, id int64, enabled bool) error {
	if !enabled {
		r.states.mu.Lock()
		owned := false
		for _, state := range r.states.states {
			if state.AccountID == id && state.AutoPaused {
				owned = true
			}
		}
		r.states.mu.Unlock()
		require.True(r.t, owned, "pause ownership must already be durable when SetSchedulable is invoked")
	}
	return r.scheduledQuestionSchedulingRepo.SetSchedulable(ctx, id, enabled)
}

func TestScheduledQuestionPauseOwnershipPersistenceAndRollback(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		persistErr, setErr, rollbackErr error
		calls                           int
		changes                         []bool
	}{
		{"persist before pause", nil, nil, nil, 1, []bool{true}},
		{"persistence failure skips pause", errors.New("database unavailable"), nil, nil, 0, []bool{true}},
		{"pause failure rolls ownership back", nil, errors.New("scheduler unavailable"), nil, 1, []bool{true, false}},
		{"rollback failure is recorded", nil, errors.New("scheduler unavailable"), errors.New("rollback unavailable"), 1, []bool{true, false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, accounts, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			plan := scheduledQuestionValidPlan(1)
			plan.AutoPauseEnabled = true
			repo.plans[1] = plan
			repo.setOwnerErr, repo.rollbackOwnerErr = tc.persistErr, tc.rollbackErr
			accounts.accounts[2].Schedulable = true
			scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts, setErr: tc.setErr}
			svc.accounts = &scheduledQuestionPersistBeforeActionRepo{scheduledQuestionSchedulingRepo: scheduling, states: repo, t: t}
			run := &ScheduledQuestionTestRun{Summary: []ScheduledQuestionTestAccountSummary{{AccountID: 2, Alerting: true, Ratio: scheduledQuestionPtr(0), Total: 2, Judged: 2}}}
			states, err := svc.updateAlertStates(context.Background(), plan, run)
			require.NoError(t, err)
			require.Len(t, scheduling.actions, tc.calls)
			require.Equal(t, tc.changes, repo.ownerChanges)
			if tc.persistErr != nil || tc.setErr != nil {
				require.Equal(t, "pause_failed", run.Summary[0].ScheduleAction)
				require.False(t, states[0].AutoPaused)
			} else {
				require.Equal(t, "paused", run.Summary[0].ScheduleAction)
				require.True(t, states[0].AutoPaused)
			}
		})
	}
}

func TestScheduledQuestionPauseSurvivesFinishRunFailureAndRecovers(t *testing.T) {
	svc, repo, accounts, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.AccountIDs, plan.QuestionIDs = []int64{2}, []int64{1}
	plan.AutoPauseEnabled, plan.AutoResumeEnabled = true, true
	repo.plans[1] = plan
	accounts.accounts[2].Schedulable = true
	scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
	svc.accounts = &scheduledQuestionPersistBeforeActionRepo{scheduledQuestionSchedulingRepo: scheduling, states: repo, t: t}
	status := "failed"
	svc.batch = &scheduledQuestionBatchStub{run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
		return emit(AccountBatchTestEvent{Type: "item_result", Data: AccountBatchTestItemResult{AccountID: 2, QuestionID: 1, Status: status, Judged: true}})
	}}
	repo.finishErr = errors.New("injected FinishRun failure")
	_, err := svc.run(context.Background(), 1, false)
	require.Error(t, err)
	require.False(t, accounts.accounts[2].Schedulable)
	require.Len(t, repo.states, 1)
	require.True(t, repo.states[0].AutoPaused)
	repo.finishErr = nil
	status = "passed"
	run, err := svc.run(context.Background(), 1, false)
	require.NoError(t, err)
	require.True(t, accounts.accounts[2].Schedulable)
	require.Equal(t, 1, run.ResumedAccounts)
	require.False(t, repo.finishState[0].AutoPaused)
}

func TestScheduledQuestionManualReleaseDuringRoundCannotBeRestored(t *testing.T) {
	svc, repo, accounts, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.AutoResumeEnabled = true
	repo.plans[1] = plan
	old := &ScheduledQuestionTestAccountState{PlanID: 1, AccountID: 2, State: "alert", AutoPaused: true, AutoPausedAt: scheduledQuestionPtr(svc.now())}
	repo.states = []*ScheduledQuestionTestAccountState{old}
	scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
	svc.accounts = scheduling
	// The round already captured the old state before an administrator clears ownership.
	states := []*ScheduledQuestionTestAccountState{scheduledQuestionPtr(*old)}
	require.NoError(t, repo.SetAutoPaused(context.Background(), 1, 2, false, nil))
	run := &ScheduledQuestionTestRun{Summary: []ScheduledQuestionTestAccountSummary{{AccountID: 2, Ratio: scheduledQuestionPtr(100), AutoPaused: true}}}
	svc.applyScheduledQuestionScheduling(context.Background(), context.Background(), plan, run, states, svc.now())
	require.Empty(t, scheduling.actions)
	require.False(t, states[0].AutoPaused)
	require.False(t, run.Summary[0].AutoPaused)
	require.False(t, accounts.accounts[2].Schedulable)
	require.Zero(t, run.ResumedAccounts)
}

func TestScheduledQuestionConcurrentPlanReleaseResumesExactlyOnce(t *testing.T) {
	repo := &scheduledQuestionRepoStub{states: []*ScheduledQuestionTestAccountState{
		{PlanID: 1, AccountID: 2, State: "alert", AutoPaused: true},
		{PlanID: 2, AccountID: 2, State: "alert", AutoPaused: true},
	}}
	results := make(chan bool, 2)
	for _, id := range []int64{1, 2} {
		go func() {
			resume, err := repo.ReleaseAutoPause(context.Background(), id, 2)
			results <- err == nil && resume
		}()
	}
	count := 0
	for range 2 {
		if <-results {
			count++
		}
	}
	require.Equal(t, 1, count)
}

func TestScheduledQuestionSummaryErrorSanitizesBeforeTruncation(t *testing.T) {
	plan := scheduledQuestionValidPlan(1)
	plan.AccountIDs, plan.QuestionIDs = []int64{2}, []int64{1}
	plan.FeishuWebhookURL = "https://open.feishu.cn/open-apis/bot/v2/hook/private-token-long"
	plan.FeishuSecret = "private-secret-very-sensitive"
	text := strings.Repeat("x", 490) + plan.FeishuSecret + " " + plan.FeishuWebhookURL + " https://user:pass@example.com/open/hook/OTHER_TOKEN?api_key=private-query#private-fragment"
	summary := summarizeScheduledQuestionTests(plan, []AccountBatchTestItemResult{{AccountID: 2, Status: "error", Error: text}}, "")[0]
	require.LessOrEqual(t, len([]rune(summary.Error)), 500)
	require.NotContains(t, summary.Error, "private-se")
	require.NotContains(t, summary.Error, "private-token")
	plan.AccountIDs, plan.QuestionIDs = []int64{2}, []int64{1}
	summary = summarizeScheduledQuestionTests(plan, []AccountBatchTestItemResult{{AccountID: 2, Status: "error", Error: strings.TrimPrefix(text, strings.Repeat("x", 490))}}, "")[0]
	for _, sensitive := range []string{"private-token-long", "private-secret", "OTHER_TOKEN", "user:pass", "private-query", "private-fragment"} {
		require.NotContains(t, summary.Error, sensitive)
	}
}

func TestScheduledQuestionRunHistorySanitizesLegacySummaryErrors(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.FeishuWebhookURL = "https://open.feishu.cn/open-apis/bot/v2/hook/history-private-token"
	plan.FeishuSecret = "history-private-secret"
	repo.plans[1] = plan
	repo.runs = []*ScheduledQuestionTestRun{{ErrorMessage: "internal error remains visible", Summary: []ScheduledQuestionTestAccountSummary{{Error: "request failed " + plan.FeishuWebhookURL + " " + plan.FeishuSecret + " https://user:password@example.com/hook/history-other-token?key=private-query#private-fragment"}}}}
	runs, err := svc.ListRuns(context.Background(), 1, 50)
	require.NoError(t, err)
	require.Equal(t, "internal error remains visible", runs[0].ErrorMessage)
	for _, sensitive := range []string{"history-private-token", "history-private-secret", "history-other-token", "private-query", "private-fragment", "user:password"} {
		require.NotContains(t, runs[0].Summary[0].Error, sensitive)
	}
	require.Contains(t, runs[0].Summary[0].Error, "request failed")
}

type scheduledQuestionBudgetEmail struct{ delay time.Duration }

func (e scheduledQuestionBudgetEmail) SendEmail(ctx context.Context, _, _, _ string) error {
	timer := time.NewTimer(e.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func TestScheduledQuestionNotificationsOutliveSchedulingBudget(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.NotifyEnabled, plan.ReportEnabled = true, true
	repo.plans[1] = plan
	svc.settings = &scheduledQuestionSettingsStub{values: map[string]string{SettingKeyAccountQuotaNotifyEmails: `[{"email":"admin@example.com","verified":true}]`}}
	svc.email = scheduledQuestionBudgetEmail{delay: 20 * time.Millisecond}
	persistCtx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	run := &ScheduledQuestionTestRun{Summary: []ScheduledQuestionTestAccountSummary{{AccountID: 2, Alerting: true, Ratio: scheduledQuestionPtr(0)}}}
	states, err := svc.updateAlertStates(persistCtx, plan, run, context.Background())
	require.NoError(t, err)
	require.Len(t, run.NotificationResults, 2)
	for _, result := range run.NotificationResults {
		require.True(t, result.Success)
	}
	require.NotNil(t, states[0].LastAlertAt)
}

func TestScheduledQuestionNotificationStageTimeoutRecordsRemainingKinds(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.NotifyEnabled, plan.ReportEnabled, plan.FeishuEnabled = true, true, true
	plan.FeishuWebhookURL = "https://open.feishu.cn/open-apis/bot/v2/hook/stage-budget-token"
	repo.plans[1] = plan
	svc.settings = &scheduledQuestionSettingsStub{values: map[string]string{SettingKeyAccountQuotaNotifyEmails: `[{"email":"admin@example.com","verified":true}]`}}
	svc.email = scheduledQuestionBudgetEmail{delay: time.Second}
	svc.feishu = &scheduledQuestionSchedulingNotifier{success: true, detail: "sent"}
	svc.notificationTimeouts = scheduledQuestionNotificationTimeouts{Stage: 10 * time.Millisecond}
	run := &ScheduledQuestionTestRun{Summary: []ScheduledQuestionTestAccountSummary{{AccountID: 2, Alerting: true, Ratio: scheduledQuestionPtr(0)}}}
	_, err := svc.updateAlertStates(context.Background(), plan, run)
	require.NoError(t, err)
	require.Len(t, run.NotificationResults, 4)
	require.Equal(t, "report", run.NotificationResults[2].Kind)
	require.Equal(t, "report", run.NotificationResults[3].Kind)
	require.False(t, run.NotificationResults[2].Success)
	require.False(t, run.NotificationResults[3].Success)
}
