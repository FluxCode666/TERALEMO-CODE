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

func TestScheduledQuestionSequentialPlansWaitForOtherAlertToPass(t *testing.T) {
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
	persistFinishedStates := func() {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		for _, state := range repo.finishState {
			found := false
			for i, previous := range repo.states {
				if previous.PlanID == state.PlanID && previous.AccountID == state.AccountID {
					repo.states[i], found = scheduledQuestionPtr(*state), true
					break
				}
			}
			if !found {
				repo.states = append(repo.states, scheduledQuestionPtr(*state))
			}
		}
	}
	run, err := svc.run(context.Background(), first.ID, false)
	require.NoError(t, err)
	require.Equal(t, 1, run.PausedAccounts)
	require.True(t, run.Summary[0].AutoPaused)
	persistFinishedStates()
	run, err = svc.run(context.Background(), second.ID, false)
	require.NoError(t, err)
	require.Zero(t, run.PausedAccounts)
	require.Empty(t, run.Summary[0].ScheduleAction)
	require.False(t, run.Summary[0].AutoPaused, "already paused accounts do not join pause ownership")
	require.Equal(t, []bool{false}, scheduling.actions)
	persistFinishedStates()
	status = "passed"
	run, err = svc.run(context.Background(), first.ID, false)
	require.NoError(t, err)
	require.Zero(t, run.ResumedAccounts)
	require.Equal(t, "resume_blocked", run.Summary[0].ScheduleAction)
	require.True(t, run.Summary[0].AutoPaused, "blocked resume keeps its existing marker")
	require.False(t, accounts.accounts[2].Schedulable)
	persistFinishedStates()
	run, err = svc.run(context.Background(), second.ID, false)
	require.NoError(t, err)
	require.Equal(t, 1, run.ResumedAccounts)
	require.True(t, accounts.accounts[2].Schedulable)
	require.False(t, run.Summary[0].AutoPaused)
	for _, state := range repo.states {
		require.False(t, state.AutoPaused, "resume clears all plans' markers")
	}
	require.Equal(t, []bool{false, true}, scheduling.actions)
}

func TestScheduledQuestionAlreadyPausedAccountsNeverJoinAutoPause(t *testing.T) {
	for _, tc := range []struct {
		name       string
		other      bool
		failure    error
		incomplete bool
	}{
		{name: "manual pause remains unowned"},
		{name: "other plan marker does not cause joining", other: true},
		{name: "publishes verdict without a pause claim", failure: errors.New("database unavailable")},
		{name: "incomplete remains unowned", other: true, incomplete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, accounts, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			plan := scheduledQuestionValidPlan(1)
			plan.AutoPauseEnabled = true
			repo.plans[1] = plan
			repo.setOwnerErr = tc.failure
			if tc.other {
				other := scheduledQuestionValidPlan(2)
				other.AutoPauseEnabled = true
				repo.plans[2] = other
				repo.states = []*ScheduledQuestionTestAccountState{{PlanID: 2, AccountID: 2, State: "alert", AutoPaused: true}}
			}
			scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
			svc.accounts = scheduling
			run := &ScheduledQuestionTestRun{Summary: []ScheduledQuestionTestAccountSummary{{AccountID: 2, Alerting: true, Ratio: scheduledQuestionPtr(0), Judged: 1, Total: 1, Incomplete: tc.incomplete}}}
			states, err := svc.updateAlertStates(context.Background(), plan, run)
			require.NoError(t, err)
			require.Empty(t, scheduling.actions)
			require.Empty(t, run.Summary[0].ScheduleAction)
			require.False(t, states[0].AutoPaused)
			require.Empty(t, repo.ownerChanges)
			require.Zero(t, run.PausedAccounts)
			published, err := repo.ListStates(context.Background(), plan.ID)
			require.NoError(t, err)
			require.Len(t, published, 1)
			require.Equal(t, "alert", published[0].State, "a closed failing account still publishes its verdict")
			require.Equal(t, states[0].State, published[0].State)
			require.Len(t, repo.verdictCalls, 1)
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
