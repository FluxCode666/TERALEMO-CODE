//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScheduledQuestionSequentialPlansShareAutoPauseUntilBothPass(t *testing.T) {
	svc, repo, accounts, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	first, second := scheduledQuestionValidPlan(1), scheduledQuestionValidPlan(2)
	for _, plan := range []*ScheduledQuestionTestPlan{first, second} {
		plan.AccountIDs, plan.QuestionIDs = []int64{2}, []int64{1}
		plan.AutoPauseEnabled, plan.AutoResumeEnabled = true, true
		repo.plans[plan.ID] = plan
	}
	accounts.accounts[2].Schedulable = true
	scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
	svc.accounts = scheduling
	status := "failed"
	svc.batch = &scheduledQuestionBatchStub{run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
		return emit(AccountBatchTestEvent{Type: "item_result", Data: AccountBatchTestItemResult{AccountID: 2, QuestionID: 1, Status: status, Judged: true}})
	}}
	run, err := svc.run(context.Background(), first.ID, false)
	require.NoError(t, err)
	require.Equal(t, 1, run.PausedAccounts)
	require.True(t, run.Summary[0].AutoPaused)
	run, err = svc.run(context.Background(), second.ID, false)
	require.NoError(t, err)
	require.Zero(t, run.PausedAccounts)
	require.Empty(t, run.Summary[0].ScheduleAction)
	require.True(t, run.Summary[0].AutoPaused)
	require.Equal(t, []bool{false}, scheduling.actions, "joining must not call SetSchedulable again")
	status = "passed"
	run, err = svc.run(context.Background(), first.ID, false)
	require.NoError(t, err)
	require.Zero(t, run.ResumedAccounts)
	require.False(t, run.Summary[0].AutoPaused)
	require.False(t, accounts.accounts[2].Schedulable)
	run, err = svc.run(context.Background(), second.ID, false)
	require.NoError(t, err)
	require.Equal(t, 1, run.ResumedAccounts)
	require.True(t, accounts.accounts[2].Schedulable)
	require.False(t, run.Summary[0].AutoPaused)
	require.Equal(t, []bool{false, true}, scheduling.actions)
}

func TestScheduledQuestionJoiningAutoPauseDoesNotClaimManualPause(t *testing.T) {
	for _, tc := range []struct {
		name       string
		other      bool
		failure    error
		incomplete bool
		action     string
		claimed    bool
	}{
		{name: "manual pause remains unowned"},
		{name: "other holder permits joining", other: true, claimed: true},
		{name: "claim query failure", failure: errors.New("database unavailable"), action: "pause_failed"},
		{name: "incomplete does not join", other: true, incomplete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, accounts, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			plan := scheduledQuestionValidPlan(1)
			plan.AutoPauseEnabled = true
			repo.plans[1] = plan
			repo.otherOwner, repo.ownerErr = tc.other, tc.failure
			scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
			svc.accounts = scheduling
			run := &ScheduledQuestionTestRun{Summary: []ScheduledQuestionTestAccountSummary{{AccountID: 2, Alerting: true, Ratio: scheduledQuestionPtr(0), Judged: 1, Total: 1, Incomplete: tc.incomplete}}}
			states, err := svc.updateAlertStates(context.Background(), plan, run)
			require.NoError(t, err)
			require.Empty(t, scheduling.actions)
			require.Equal(t, tc.action, run.Summary[0].ScheduleAction)
			require.Equal(t, tc.claimed, states[0].AutoPaused)
			require.Zero(t, run.PausedAccounts)
		})
	}
}

type scheduledQuestionReleaseDuringMail struct {
	repo   *scheduledQuestionRepoStub
	planID int64
}

func (e scheduledQuestionReleaseDuringMail) SendEmail(context.Context, string, string, string) error {
	return e.repo.SetAutoPaused(context.Background(), e.planID, 2, false, nil)
}

func TestScheduledQuestionSummaryOwnershipRefreshAfterNotificationRelease(t *testing.T) {
	svc, repo, accounts, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.AccountIDs, plan.QuestionIDs = []int64{2}, []int64{1}
	plan.AutoPauseEnabled, plan.NotifyEnabled, plan.ReportEnabled = true, true, true
	repo.plans[1] = plan
	accounts.accounts[2].Schedulable = true
	svc.accounts = &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
	svc.email = scheduledQuestionReleaseDuringMail{repo: repo, planID: plan.ID}
	svc.settings = &scheduledQuestionSettingsStub{values: map[string]string{SettingKeyAccountQuotaNotifyEmails: `[{"email":"admin@example.com","verified":true}]`}}
	svc.batch = &scheduledQuestionBatchStub{run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
		return emit(AccountBatchTestEvent{Type: "item_result", Data: AccountBatchTestItemResult{AccountID: 2, QuestionID: 1, Status: "failed", Judged: true}})
	}}
	run, err := svc.run(context.Background(), plan.ID, false)
	require.NoError(t, err)
	require.Equal(t, "paused", run.Summary[0].ScheduleAction)
	require.False(t, run.Summary[0].AutoPaused, "refresh must observe manual release during slow notifications")
}

func TestScheduledQuestionSummaryOwnershipRefreshFailurePreservesMemory(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	repo.stateErr = errors.New("read unavailable")
	run := &ScheduledQuestionTestRun{Summary: []ScheduledQuestionTestAccountSummary{{AccountID: 2, AutoPaused: true}}}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	svc.refreshScheduledQuestionSummaryOwnership(ctx, 1, run)
	require.True(t, run.Summary[0].AutoPaused)
	require.Contains(t, logs.String(), "scheduled_question_test_summary_ownership_refresh_failed")
	require.NotContains(t, logs.String(), "read unavailable")
}
