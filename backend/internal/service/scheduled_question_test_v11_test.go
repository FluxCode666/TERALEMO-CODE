//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScheduledQuestionV11PublishesVerdictsWithoutSchedulingActions(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      string
		schedulable bool
		pause       bool
		wantState   string
		wantResult  string
	}{
		{name: "pause disabled", status: "failed", schedulable: true, wantState: "alert", wantResult: "failed"},
		{name: "already closed failure", status: "failed", pause: true, wantState: "alert", wantResult: "failed"},
		{name: "resume disabled", status: "passed", wantState: "ok", wantResult: "passed"},
		{name: "unjudged", status: "unjudged", pause: true, wantState: "ok", wantResult: "unjudged"},
		{name: "incomplete", schedulable: true, pause: true, wantState: "alert", wantResult: "incomplete"},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, repo, accounts, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			plan := scheduledQuestionValidPlan(1)
			plan.AccountIDs, plan.QuestionIDs = []int64{1}, []int64{1}
			plan.AutoPauseEnabled = test.pause
			repo.plans[plan.ID] = plan
			alertAt := svc.now().Add(-7 * time.Hour)
			previous := &ScheduledQuestionTestAccountState{PlanID: plan.ID, AccountID: 1, State: "alert", LastAlertAt: &alertAt}
			repo.states = []*ScheduledQuestionTestAccountState{previous}
			accounts.accounts[1].Schedulable = test.schedulable
			scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
			svc.accounts = scheduling
			svc.batch = &scheduledQuestionBatchStub{run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
				if test.status == "" {
					return nil
				}
				return scheduledQuestionV2EmitItems(emit, 1, test.status)
			}}
			run, err := svc.run(context.Background(), plan.ID, false)
			require.NoError(t, err)
			summary := scheduledQuestionV2Summary(t, run, 1)
			require.Equal(t, test.wantResult, summary.Result)
			require.Equal(t, test.wantState, summary.StateAfter)
			require.Empty(t, summary.ScheduleAction)
			require.Empty(t, scheduling.actions)
			require.False(t, summary.AutoPaused)
			require.Zero(t, run.PausedAccounts)
			require.Zero(t, run.ResumedAccounts)
			require.Len(t, repo.verdictCalls, 1)
			published, err := repo.ListStates(context.Background(), plan.ID)
			require.NoError(t, err)
			require.Len(t, published, 1)
			require.Equal(t, test.wantState, published[0].State)
			require.Equal(t, &alertAt, published[0].LastAlertAt, "publishing a verdict does not change notification cooldown")
			require.Len(t, repo.finishState, 1)
			require.Equal(t, published[0].State, repo.finishState[0].State, "FinishRun must preserve the published verdict")
			if test.wantState == "ok" {
				require.Nil(t, repo.finishState[0].LastAlertAt, "FinishRun clears cooldown for a recovered verdict")
			} else {
				require.Equal(t, &alertAt, repo.finishState[0].LastAlertAt, "no delivery means no new cooldown")
			}
			require.Equal(t, "alert", previous.State, "publishing must not mutate the prior snapshot")
		})
	}
}

type scheduledQuestionV11BarrierNotifier struct {
	entered chan struct{}
	release chan struct{}
}

func (n *scheduledQuestionV11BarrierNotifier) Send(ctx context.Context, _, _ string, _ scheduledQuestionNotification) (bool, string) {
	close(n.entered)
	select {
	case <-n.release:
		return true, "sent"
	case <-ctx.Done():
		return false, "cancelled"
	}
}

func TestScheduledQuestionV11AlertIsPublishedBeforeNotificationBarrier(t *testing.T) {
	for _, schedulableBefore := range []bool{false, true} {
		name := "already closed before testing"
		if schedulableBefore {
			name = "administrator closes during testing"
		}
		t.Run(name, func(t *testing.T) {
			svc, repo, accounts, _ := newScheduledQuestionFixture()
			first, second := scheduledQuestionValidPlan(1), scheduledQuestionValidPlan(2)
			for _, plan := range []*ScheduledQuestionTestPlan{first, second} {
				plan.AccountIDs, plan.QuestionIDs = []int64{1}, []int64{1}
				plan.AutoPauseEnabled, plan.AutoResumeEnabled = true, true
				repo.plans[plan.ID] = plan
			}
			first.ModelID, second.ModelID = "failing-plan", "passing-plan"
			scheduledQuestionV2EnableNotifications(first)
			alertAt := svc.now().Add(-7 * time.Hour)
			repo.states = []*ScheduledQuestionTestAccountState{
				{PlanID: first.ID, AccountID: 1, State: "ok", LastAlertAt: &alertAt},
				{PlanID: second.ID, AccountID: 1, State: "ok"},
			}
			account := accounts.accounts[1]
			account.Schedulable = schedulableBefore
			scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
			svc.accounts = scheduling
			notifier := &scheduledQuestionV11BarrierNotifier{entered: make(chan struct{}), release: make(chan struct{})}
			svc.feishu = notifier
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(notifier.release) }) }
			defer func() { release(); svc.Stop() }()
			svc.batch = &scheduledQuestionBatchStub{
				prepare: func(_ context.Context, input AccountBatchTestInput) (*AccountBatchTestPlan, error) {
					return &AccountBatchTestPlan{modelID: input.ModelID}, nil
				},
				run: func(_ context.Context, batch *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
					if batch.modelID == first.ModelID {
						account.Schedulable = false
						return scheduledQuestionV2EmitItems(emit, 1, "failed")
					}
					return scheduledQuestionV2EmitItems(emit, 1, "passed")
				},
			}
			type outcome struct {
				run *ScheduledQuestionTestRun
				err error
			}
			firstDone := make(chan outcome, 1)
			go func() {
				run, err := svc.run(context.Background(), first.ID, false)
				firstDone <- outcome{run: run, err: err}
			}()
			waitScheduledQuestionSignal(t, notifier.entered, "failing plan did not reach notification barrier")
			published, err := repo.ListStates(context.Background(), first.ID)
			require.NoError(t, err)
			require.Len(t, published, 1)
			require.Equal(t, "alert", published[0].State, "the passing plan must see the failure before notification returns")
			require.Equal(t, &alertAt, published[0].LastAlertAt)
			require.False(t, published[0].AutoPaused, "a currently closed account does not acquire a pause marker")
			require.False(t, account.Schedulable)
			secondRun, err := svc.run(context.Background(), second.ID, false)
			require.NoError(t, err)
			require.Equal(t, "success", secondRun.Status)
			require.Equal(t, "resume_blocked", secondRun.Summary[0].ScheduleAction)
			require.Zero(t, secondRun.ResumedAccounts)
			require.False(t, account.Schedulable)
			require.Len(t, repo.finishState, 1)
			require.Equal(t, "ok", repo.finishState[0].State)
			release()
			var firstResult outcome
			select {
			case firstResult = <-firstDone:
			case <-time.After(5 * time.Second):
				t.Fatal("failing plan did not finish after notification release")
			}
			require.NoError(t, firstResult.err)
			firstSummary := scheduledQuestionV2Summary(t, firstResult.run, 1)
			require.Equal(t, &schedulableBefore, firstSummary.SchedulableBefore)
			require.Equal(t, scheduledQuestionPtr(false), firstSummary.SchedulableAfter)
			require.Empty(t, firstSummary.ScheduleAction)
			require.Zero(t, firstResult.run.PausedAccounts)
			require.Empty(t, scheduling.actions)
			require.Len(t, repo.finishState, 1)
			require.Equal(t, "alert", repo.finishState[0].State)
			require.Equal(t, scheduledQuestionPtr(svc.now()), repo.finishState[0].LastAlertAt, "FinishRun advances cooldown only after successful notification")
			for planID, want := range map[int64]string{first.ID: "alert", second.ID: "ok"} {
				states, err := repo.ListStates(context.Background(), planID)
				require.NoError(t, err)
				require.Len(t, states, 1)
				require.Equal(t, want, states[0].State)
			}
			require.False(t, account.Schedulable)
		})
	}
}

func TestScheduledQuestionV11VerdictFailureSuppressesEveryNotification(t *testing.T) {
	for _, kind := range []string{"publish", "pause", "resume", "pause lock", "resume lock"} {
		t.Run(kind, func(t *testing.T) {
			svc, repo, accounts, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			plan := scheduledQuestionValidPlan(1)
			plan.AccountIDs, plan.QuestionIDs = []int64{1}, []int64{1}
			scheduledQuestionV2EnableNotifications(plan)
			plan.ReportEnabled = true
			repo.plans[plan.ID] = plan
			alertAt := svc.now().Add(-7 * time.Hour)
			previous := &ScheduledQuestionTestAccountState{PlanID: plan.ID, AccountID: 1, State: "ok", LastAlertAt: &alertAt}
			failure := errors.New("injected verdict failure")
			status, wantAction := "failed", ""
			scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
			switch kind {
			case "publish":
				repo.verdictErr = failure
			case "pause", "pause lock":
				plan.AutoPauseEnabled, accounts.accounts[1].Schedulable = true, true
				wantAction = "pause_failed"
				if kind == "pause lock" {
					repo.setOwnerErr = failure
				} else {
					scheduling.setErr = failure
				}
			case "resume", "resume lock":
				plan.AutoResumeEnabled, previous.AutoPaused = true, true
				previous.State, previous.AutoPausedAt = "alert", &alertAt
				status, wantAction = "passed", "resume_failed"
				if kind == "resume lock" {
					repo.ownerErr = failure
				} else {
					scheduling.setErr = failure
				}
			}
			before, previousCopy := accounts.accounts[1].Schedulable, *previous
			repo.states = []*ScheduledQuestionTestAccountState{previous}
			svc.accounts = scheduling
			notifier := &scheduledQuestionV2Notifier{}
			svc.feishu = notifier
			svc.batch = &scheduledQuestionBatchStub{run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
				return scheduledQuestionV2EmitItems(emit, 1, status)
			}}
			run, err := svc.run(context.Background(), plan.ID, false)
			require.NoError(t, err, "a failed verdict is returned as a persisted failed round")
			require.Equal(t, "failed", run.Status)
			require.Contains(t, run.ErrorMessage, failure.Error())
			require.True(t, run.NotificationsSuppressed)
			require.Empty(t, run.NotificationResults)
			require.Empty(t, notifier.notifications, "neither alerts, recovery nor reports may follow a verdict failure")
			require.Nil(t, repo.finishState)
			require.Len(t, repo.finished, 1)
			require.Zero(t, run.PausedAccounts)
			require.Zero(t, run.ResumedAccounts)
			require.Equal(t, wantAction, run.Summary[0].ScheduleAction)
			require.Equal(t, before, accounts.accounts[1].Schedulable)
			require.Equal(t, &before, run.Summary[0].SchedulableAfter)
			require.Equal(t, previousCopy, *repo.states[0], "failed transactions preserve verdict, marker and cooldown")
			require.Equal(t, previousCopy.AutoPaused, run.Summary[0].AutoPaused)
		})
	}
}

type scheduledQuestionV11AccountFailureRepo struct {
	*scheduledQuestionSchedulingRepo
	failID int64
	fail   error
	calls  []int64
}

func (r *scheduledQuestionV11AccountFailureRepo) SetSchedulable(ctx context.Context, id int64, value bool) error {
	r.calls = append(r.calls, id)
	if id == r.failID {
		return r.fail
	}
	return r.scheduledQuestionSchedulingRepo.SetSchedulable(ctx, id, value)
}

func TestScheduledQuestionV11CountsOnlyCommittedActionsAndContinuesAfterFailure(t *testing.T) {
	svc, repo, accounts, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.AccountIDs, plan.QuestionIDs = []int64{1, 2, 3}, []int64{1}
	plan.AutoPauseEnabled, plan.AutoResumeEnabled, plan.ReportEnabled = true, true, true
	scheduledQuestionV2EnableNotifications(plan)
	repo.plans[plan.ID] = plan
	alertAt := svc.now().Add(-7 * time.Hour)
	repo.states = []*ScheduledQuestionTestAccountState{
		{PlanID: plan.ID, AccountID: 1, State: "ok", LastAlertAt: &alertAt},
		{PlanID: plan.ID, AccountID: 2, State: "alert", LastAlertAt: &alertAt, AutoPaused: true, AutoPausedAt: &alertAt},
		{PlanID: plan.ID, AccountID: 3, State: "alert", LastAlertAt: &alertAt},
	}
	accounts.accounts[1].Schedulable = true
	failure := errors.New("account two transaction failed")
	scheduling := &scheduledQuestionV11AccountFailureRepo{
		scheduledQuestionSchedulingRepo: &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts},
		failID:                          2,
		fail:                            failure,
	}
	svc.accounts = scheduling
	notifier := &scheduledQuestionV2Notifier{}
	svc.feishu = notifier
	svc.batch = &scheduledQuestionBatchStub{run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
		for i, status := range []string{"failed", "passed", "passed"} {
			if err := scheduledQuestionV2EmitItems(emit, int64(i+1), status); err != nil {
				return err
			}
		}
		return nil
	}}
	run, err := svc.run(context.Background(), plan.ID, false)
	require.NoError(t, err)
	require.Equal(t, "failed", run.Status)
	require.Contains(t, run.ErrorMessage, failure.Error())
	require.True(t, run.NotificationsSuppressed)
	require.Empty(t, run.NotificationResults)
	require.Empty(t, notifier.notifications)
	require.Nil(t, repo.finishState, "FinishRun must not overwrite the rolled-back verdict or advance any cooldown")
	require.Equal(t, []int64{1, 2, 3}, scheduling.calls, "later accounts still receive a transaction after a failure")
	require.Equal(t, 1, run.PausedAccounts)
	require.Equal(t, 1, run.ResumedAccounts)
	for id, want := range map[int64]struct {
		state       string
		action      string
		schedulable bool
		owned       bool
	}{
		1: {state: "alert", action: "paused", owned: true},
		2: {state: "alert", action: "resume_failed", owned: true},
		3: {state: "ok", action: "resumed", schedulable: true},
	} {
		summary := scheduledQuestionV2Summary(t, run, id)
		require.Equal(t, want.action, summary.ScheduleAction)
		require.Equal(t, want.owned, summary.AutoPaused)
		require.Equal(t, want.schedulable, accounts.accounts[id].Schedulable)
		require.Equal(t, &want.schedulable, summary.SchedulableAfter)
		stored := repo.states[id-1]
		require.Equal(t, want.state, stored.State)
		require.Equal(t, want.owned, stored.AutoPaused)
		require.Equal(t, &alertAt, stored.LastAlertAt, "no account gains cooldown when the notification stage is suppressed")
	}
}
