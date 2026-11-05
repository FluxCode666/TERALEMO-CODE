//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type scheduledQuestionV2Notifier struct {
	notifications []scheduledQuestionNotification
}

func (n *scheduledQuestionV2Notifier) Send(_ context.Context, _, _ string, notification scheduledQuestionNotification) (bool, string) {
	n.notifications = append(n.notifications, notification)
	return true, "sent"
}

func scheduledQuestionV2EnableNotifications(plan *ScheduledQuestionTestPlan) {
	plan.NotifyEnabled, plan.EmailEnabled, plan.FeishuEnabled = true, false, true
	plan.FeishuWebhookURL = "https://open.feishu.cn/open-apis/bot/v2/hook/test-v2-token"
}

func scheduledQuestionV2Summary(t *testing.T, run *ScheduledQuestionTestRun, accountID int64) ScheduledQuestionTestAccountSummary {
	t.Helper()
	for _, summary := range run.Summary {
		if summary.AccountID == accountID {
			return summary
		}
	}
	t.Fatalf("summary missing for account %d", accountID)
	return ScheduledQuestionTestAccountSummary{}
}

func scheduledQuestionV2EmitItems(emit func(AccountBatchTestEvent) error, accountID int64, statuses ...string) error {
	for i, status := range statuses {
		item := AccountBatchTestItemResult{AccountID: accountID, QuestionID: int64(i + 1), Status: status, Judged: status == "passed" || status == "failed"}
		if status == "error" {
			item.Error = "API returned 503: private upstream body"
		}
		if err := emit(AccountBatchTestEvent{Type: "item_result", Data: item}); err != nil {
			return err
		}
	}
	return nil
}

func TestScheduledQuestionV2RunPreflightFiltersExhaustedAccounts(t *testing.T) {
	svc, repo, accounts, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.AccountIDs, plan.QuestionIDs, plan.ModelID = []int64{1, 2, 3, 4}, []int64{1}, "explicit-model"
	plan.AutoPauseEnabled, plan.AutoResumeEnabled = true, true
	repo.plans[1] = plan
	accounts.accounts[1].Type, accounts.accounts[1].Schedulable = AccountTypeAPIKey, true
	accounts.accounts[1].Extra = map[string]any{"quota_limit": 1, "quota_used": 1}
	accounts.accounts[2].Type = AccountTypeOAuth
	accounts.accounts[2].Extra = map[string]any{"window_cost_limit": 10}
	reset := time.Now().Add(time.Hour)
	accounts.accounts[3].RateLimitResetAt = &reset
	accounts.accounts[4].Schedulable = true
	svc.sessionLimitCache = &scheduledQuestionSkipCache{cost: 10, hit: true}
	scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
	svc.accounts = scheduling
	prepareCalls, runCalls := 0, 0
	svc.batch = &scheduledQuestionBatchStub{
		prepare: func(_ context.Context, input AccountBatchTestInput) (*AccountBatchTestPlan, error) {
			prepareCalls++
			require.Equal(t, []int64{4}, input.AccountIDs, "skip before batch validation or upstream requests")
			require.Equal(t, plan.QuestionIDs, input.QuestionIDs)
			require.Equal(t, plan.ModelID, input.ModelID)
			require.True(t, input.captureStatus, "scheduled rounds alone opt into private HTTP status capture")
			return &AccountBatchTestPlan{}, nil
		},
		run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
			runCalls++
			return scheduledQuestionV2EmitItems(emit, 4, "passed")
		},
	}
	run, err := svc.run(context.Background(), plan.ID, false)
	require.NoError(t, err)
	require.Equal(t, 1, prepareCalls)
	require.Equal(t, 1, runCalls)
	require.Equal(t, "success", run.Status)
	require.Equal(t, 4, run.TotalAccounts)
	require.Equal(t, 3, run.SkippedAccounts)
	require.Equal(t, 1, run.PassedAccounts)
	require.Zero(t, run.AlertedAccounts)
	require.Len(t, repo.finishState, 1)
	require.Equal(t, int64(4), repo.finishState[0].AccountID)
	require.Empty(t, scheduling.actions)
	for id, reason := range map[int64]string{1: "quota_exceeded", 2: "window_cost_exceeded", 3: "rate_limited"} {
		summary := scheduledQuestionV2Summary(t, run, id)
		require.Equal(t, "skipped", summary.Result)
		require.Equal(t, reason, summary.SkipReason)
		require.False(t, summary.Alerting)
		require.Empty(t, summary.ScheduleAction)
		require.NotNil(t, summary.SchedulableBefore)
		require.Equal(t, *summary.SchedulableBefore, *summary.SchedulableAfter)
	}
}

func TestScheduledQuestionV2RunAllSkippedLeavesExistingStateUntouched(t *testing.T) {
	svc, repo, accounts, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.AutoPauseEnabled, plan.AutoResumeEnabled = true, true
	scheduledQuestionV2EnableNotifications(plan)
	repo.plans[1] = plan
	reset, alertAt := time.Now().Add(time.Hour), svc.now().Add(-2*time.Hour)
	previous := &ScheduledQuestionTestAccountState{PlanID: 1, AccountID: 1, State: "alert", LastAlertAt: &alertAt, AutoPaused: true, AutoPausedAt: &alertAt}
	previousCopy := *previous
	repo.states = []*ScheduledQuestionTestAccountState{previous}
	for _, id := range plan.AccountIDs {
		accounts.accounts[id].RateLimitResetAt = &reset
	}
	scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
	svc.accounts = scheduling
	notifier := &scheduledQuestionV2Notifier{}
	svc.feishu = notifier
	svc.batch = &scheduledQuestionBatchStub{
		prepare: func(context.Context, AccountBatchTestInput) (*AccountBatchTestPlan, error) {
			t.Error("all skipped rounds must not call Prepare")
			return nil, errors.New("unexpected Prepare")
		},
		run: func(context.Context, *AccountBatchTestPlan, func(AccountBatchTestEvent) error) error {
			t.Error("all skipped rounds must not call Run")
			return errors.New("unexpected Run")
		},
	}
	run, err := svc.run(context.Background(), plan.ID, false)
	require.NoError(t, err)
	require.Equal(t, "success", run.Status)
	require.Equal(t, 2, run.SkippedAccounts)
	require.Zero(t, run.PassedAccounts)
	require.Zero(t, run.AlertedAccounts)
	require.Empty(t, repo.finishState, "skipped accounts must not even upsert existing state")
	require.Equal(t, previousCopy, *previous)
	require.Equal(t, []*ScheduledQuestionTestAccountState{previous}, repo.states)
	require.Empty(t, repo.ownerChanges)
	require.Empty(t, repo.verdictCalls, "skipped accounts never publish a verdict")
	require.Empty(t, scheduling.actions)
	require.Empty(t, notifier.notifications, "a skipped former alert must not trigger recovered notification")
	require.Empty(t, run.NotificationResults)
	require.False(t, run.AutoPauseSkipped)
	require.True(t, scheduledQuestionV2Summary(t, run, 1).AutoPaused, "skipping suppresses writes but still reads existing ownership for the summary")
	for _, summary := range run.Summary {
		require.Equal(t, "skipped", summary.Result)
		require.False(t, summary.Incomplete)
		require.Empty(t, summary.ScheduleAction)
	}
}

func TestScheduledQuestionV2RunSkippedAccountDoesNotShiftNotificationStateIndexes(t *testing.T) {
	svc, repo, accounts, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.AccountIDs, plan.QuestionIDs = []int64{1, 2, 3}, []int64{1}
	scheduledQuestionV2EnableNotifications(plan)
	repo.plans[1] = plan
	accounts.accounts[1].Type = AccountTypeAPIKey
	accounts.accounts[1].Extra = map[string]any{"quota_limit": 1, "quota_used": 1}
	alertAt := svc.now().Add(-time.Hour)
	skippedState := &ScheduledQuestionTestAccountState{PlanID: 1, AccountID: 1, State: "alert", LastAlertAt: &alertAt}
	repo.states = []*ScheduledQuestionTestAccountState{skippedState, {PlanID: 1, AccountID: 3, State: "alert", LastAlertAt: &alertAt}}
	notifier := &scheduledQuestionV2Notifier{}
	svc.feishu = notifier
	svc.batch = &scheduledQuestionBatchStub{run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
		if err := scheduledQuestionV2EmitItems(emit, 2, "failed"); err != nil {
			return err
		}
		return scheduledQuestionV2EmitItems(emit, 3, "passed")
	}}
	run, err := svc.run(context.Background(), plan.ID, false)
	require.NoError(t, err)
	require.Equal(t, 1, run.SkippedAccounts)
	require.Len(t, repo.finishState, 2)
	require.Equal(t, int64(2), repo.finishState[0].AccountID)
	require.Equal(t, "alert", repo.finishState[0].State)
	require.Equal(t, scheduledQuestionPtr(svc.now()), repo.finishState[0].LastAlertAt)
	require.Equal(t, int64(3), repo.finishState[1].AccountID)
	require.Equal(t, "ok", repo.finishState[1].State)
	require.Nil(t, repo.finishState[1].LastAlertAt)
	require.True(t, alertAt.Equal(*skippedState.LastAlertAt), "skip preserves old alert cooldown")
	require.Equal(t, "alert", skippedState.State)
	require.Len(t, notifier.notifications, 2)
	require.Contains(t, notifier.notifications[0].Title, "Scheduled test alert")
	require.Len(t, notifier.notifications[0].Rows, 1)
	require.Contains(t, notifier.notifications[0].Rows[0].Account, "(2)")
	require.Contains(t, notifier.notifications[1].Title, "Scheduled test recovered")
	require.Len(t, notifier.notifications[1].Rows, 1)
	require.Contains(t, notifier.notifications[1].Rows[0].Account, "(3)")
}

func TestScheduledQuestionV2RunPostflightConvertsOnlyCompleteAllErrorAccounts(t *testing.T) {
	for _, test := range []struct {
		name       string
		statuses   []string
		quotaAfter bool
		code       int
		errorText  string
		wantReason string
		incomplete bool
	}{
		{name: "quota written during upstream request", statuses: []string{"error", "error"}, quotaAfter: true, wantReason: "quota_exceeded"},
		{name: "structured 429 without limit state", statuses: []string{"error", "error"}, code: http.StatusTooManyRequests, errorText: "private error", wantReason: "rate_limited"},
		{name: "legacy 429 without limit state", statuses: []string{"error", "error"}, errorText: "API returned 429: usage_limit_reached", wantReason: "rate_limited"},
		{name: "structured non429 overrides text", statuses: []string{"error", "error"}, code: http.StatusInternalServerError, errorText: "API returned 429: fake body"},
		{name: "mixed unjudged and error stays tested", statuses: []string{"unjudged", "error"}, quotaAfter: true},
		{name: "mixed judged and error stays tested", statuses: []string{"passed", "error"}, quotaAfter: true},
		{name: "incomplete 429 stays incomplete", statuses: []string{"error"}, code: http.StatusTooManyRequests, incomplete: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, repo, accounts, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			plan := scheduledQuestionValidPlan(1)
			plan.AccountIDs = []int64{1}
			scheduledQuestionV2EnableNotifications(plan)
			repo.plans[1] = plan
			account := accounts.accounts[1]
			account.Type, account.Schedulable = AccountTypeAPIKey, true
			account.Extra = map[string]any{"quota_limit": 1, "quota_used": 0}
			scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
			svc.accounts = scheduling
			notifier := &scheduledQuestionV2Notifier{}
			svc.feishu = notifier
			previous := &ScheduledQuestionTestAccountState{PlanID: 1, AccountID: 1, State: "alert", AutoPaused: true}
			repo.states = []*ScheduledQuestionTestAccountState{previous}
			svc.batch = &scheduledQuestionBatchStub{run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
				if test.quotaAfter {
					account.Extra["quota_used"] = 1
				}
				for i, status := range test.statuses {
					item := AccountBatchTestItemResult{AccountID: 1, QuestionID: int64(i + 1), Status: status, Judged: status == "passed", StatusCode: test.code, Error: test.errorText}
					if err := emit(AccountBatchTestEvent{Type: "item_result", Data: item}); err != nil {
						return err
					}
				}
				return nil
			}}
			run, err := svc.run(context.Background(), plan.ID, false)
			require.NoError(t, err)
			summary := scheduledQuestionV2Summary(t, run, 1)
			require.Equal(t, test.incomplete, summary.Incomplete)
			if test.wantReason == "" {
				require.NotEqual(t, "skipped", summary.Result)
				require.Zero(t, run.SkippedAccounts)
				require.Len(t, repo.finishState, 1)
				return
			}
			require.Equal(t, "skipped", summary.Result)
			require.Equal(t, test.wantReason, summary.SkipReason)
			require.Nil(t, summary.SkipResetAt, "total quota and pure 429 have no known automatic reset")
			require.Equal(t, 1, run.SkippedAccounts)
			require.Zero(t, run.AlertedAccounts)
			require.False(t, summary.Alerting)
			require.Nil(t, summary.Ratio)
			require.Empty(t, summary.Error)
			require.Empty(t, repo.finishState)
			require.Equal(t, "alert", previous.State)
			require.True(t, previous.AutoPaused)
			require.Empty(t, repo.verdictCalls, "postflight skips must not publish an ok or alert verdict")
			require.Empty(t, notifier.notifications)
			require.Empty(t, scheduling.actions)
		})
	}
}

func TestScheduledQuestionV2RunAllErrorsProtectionCountsOnlyActiveAccounts(t *testing.T) {
	for _, activeCount := range []int{1, 2} {
		t.Run(string(rune('0'+activeCount))+" active", func(t *testing.T) {
			svc, repo, accounts, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			plan := scheduledQuestionValidPlan(1)
			plan.AccountIDs, plan.QuestionIDs = []int64{1, 2, 3}[:activeCount+1], []int64{1}
			plan.AutoPauseEnabled = true
			repo.plans[1] = plan
			accounts.accounts[1].Type = AccountTypeAPIKey
			accounts.accounts[1].Extra = map[string]any{"quota_limit": 1, "quota_used": 1}
			for _, id := range plan.AccountIDs {
				accounts.accounts[id].Schedulable = true
			}
			scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
			svc.accounts = scheduling
			svc.batch = &scheduledQuestionBatchStub{run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
				for _, id := range plan.AccountIDs[1:] {
					if err := scheduledQuestionV2EmitItems(emit, id, "error"); err != nil {
						return err
					}
				}
				return nil
			}}
			run, err := svc.run(context.Background(), plan.ID, false)
			require.NoError(t, err)
			require.Equal(t, 1, run.SkippedAccounts)
			require.Equal(t, activeCount, run.AlertedAccounts)
			require.Equal(t, activeCount >= 2, run.AutoPauseSkipped)
			require.Len(t, repo.verdictCalls, activeCount, "all-errors protection suppresses pauses but still publishes active verdicts")
			if activeCount == 1 {
				require.Equal(t, []bool{false}, scheduling.actions, "one active error is not a system-wide failure")
				require.Equal(t, 1, run.PausedAccounts)
			} else {
				require.Empty(t, scheduling.actions)
				require.Zero(t, run.PausedAccounts)
			}
			require.True(t, accounts.accounts[1].Schedulable, "never pause a skipped account")
		})
	}
}

func TestScheduledQuestionV2RunRestoredQuotaAutomaticallyRejoinsNextRound(t *testing.T) {
	svc, repo, accounts, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.AccountIDs, plan.QuestionIDs = []int64{1}, []int64{1}
	repo.plans[1] = plan
	account := accounts.accounts[1]
	account.Type = AccountTypeAPIKey
	account.Extra = map[string]any{"quota_daily_limit": 1, "quota_daily_used": 1, "quota_daily_start": time.Now().Format(time.RFC3339)}
	prepareCalls := 0
	svc.batch = &scheduledQuestionBatchStub{
		prepare: func(_ context.Context, input AccountBatchTestInput) (*AccountBatchTestPlan, error) {
			prepareCalls++
			require.Equal(t, []int64{1}, input.AccountIDs)
			return &AccountBatchTestPlan{}, nil
		},
		run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
			return scheduledQuestionV2EmitItems(emit, 1, "passed")
		},
	}
	first, err := svc.run(context.Background(), plan.ID, false)
	require.NoError(t, err)
	require.Equal(t, 1, first.SkippedAccounts)
	require.Zero(t, prepareCalls)
	account.Extra["quota_daily_start"] = time.Now().Add(-25 * time.Hour).Format(time.RFC3339)
	second, err := svc.run(context.Background(), plan.ID, false)
	require.NoError(t, err)
	require.Equal(t, 1, prepareCalls)
	require.Zero(t, second.SkippedAccounts)
	require.Equal(t, 1, second.PassedAccounts)
	require.Equal(t, "passed", second.Summary[0].Result)
	require.Empty(t, second.Summary[0].SkipReason)
	require.Nil(t, second.Summary[0].SkipResetAt)
	require.Equal(t, 1, first.SkippedAccounts, "a later round must not mutate historical summaries")
}

type scheduledQuestionV2AfterLookupRepo struct {
	*scheduledQuestionSchedulingRepo
	lookups map[int64]int
	failID  int64
	failErr error
}

func (r *scheduledQuestionV2AfterLookupRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	r.lookups[id]++
	if id == r.failID && r.lookups[id] >= 2 && len(r.actions) > 0 {
		if r.failErr != nil {
			return nil, r.failErr
		}
		return nil, errors.New("post-action lookup unavailable")
	}
	return r.scheduledQuestionAccountRepoStub.GetByID(ctx, id)
}

func TestScheduledQuestionV2RunRecordsSchedulingBeforeAndAfter(t *testing.T) {
	for _, failReadback := range []bool{false, true} {
		t.Run(map[bool]string{false: "readback", true: "action fallback"}[failReadback], func(t *testing.T) {
			svc, repo, accounts, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			plan := scheduledQuestionValidPlan(1)
			plan.AccountIDs, plan.QuestionIDs = []int64{1, 2, 3}, []int64{1}
			plan.AutoPauseEnabled, plan.AutoResumeEnabled = true, true
			repo.plans[1] = plan
			accounts.accounts[1].Schedulable = true
			accounts.accounts[2].Schedulable = false
			delete(accounts.accounts, 3)
			scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
			svc.accounts = scheduling
			if failReadback {
				svc.accounts = &scheduledQuestionV2AfterLookupRepo{scheduledQuestionSchedulingRepo: scheduling, lookups: make(map[int64]int), failID: 2}
			}
			svc.batch = &scheduledQuestionBatchStub{run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
				for i, status := range []string{"failed", "passed", "error"} {
					if err := scheduledQuestionV2EmitItems(emit, int64(i+1), status); err != nil {
						return err
					}
				}
				return nil
			}}
			run, err := svc.run(context.Background(), plan.ID, false)
			require.NoError(t, err)
			paused, resumed, missing := scheduledQuestionV2Summary(t, run, 1), scheduledQuestionV2Summary(t, run, 2), scheduledQuestionV2Summary(t, run, 3)
			require.Equal(t, "paused", paused.ScheduleAction)
			require.Equal(t, scheduledQuestionPtr(true), paused.SchedulableBefore)
			require.Equal(t, scheduledQuestionPtr(false), paused.SchedulableAfter)
			require.Equal(t, "resumed", resumed.ScheduleAction, "passing a manually disabled account must now open scheduling")
			require.Equal(t, scheduledQuestionPtr(false), resumed.SchedulableBefore)
			require.Equal(t, scheduledQuestionPtr(true), resumed.SchedulableAfter)
			require.Nil(t, missing.SchedulableBefore)
			require.Nil(t, missing.SchedulableAfter)
			require.Equal(t, 1, run.PausedAccounts)
			require.Equal(t, 1, run.ResumedAccounts)
			require.Equal(t, []bool{false, true}, scheduling.actions)
		})
	}
}

func TestScheduledQuestionV2RunAfterLookupMissingAccountHasUnknownScheduling(t *testing.T) {
	svc, repo, accounts, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.AccountIDs, plan.QuestionIDs = []int64{1}, []int64{1}
	plan.AutoResumeEnabled = true
	repo.plans[1] = plan
	accounts.accounts[1].Schedulable = false
	scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
	svc.accounts = &scheduledQuestionV2AfterLookupRepo{
		scheduledQuestionSchedulingRepo: scheduling, lookups: make(map[int64]int), failID: 1,
		failErr: fmt.Errorf("account disappeared during the round: %w", ErrAccountNotFound),
	}
	svc.batch = &scheduledQuestionBatchStub{run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
		return scheduledQuestionV2EmitItems(emit, 1, "passed")
	}}
	run, err := svc.run(context.Background(), plan.ID, false)
	require.NoError(t, err)
	summary := scheduledQuestionV2Summary(t, run, 1)
	require.Equal(t, "resumed", summary.ScheduleAction)
	require.Equal(t, scheduledQuestionPtr(false), summary.SchedulableBefore)
	require.Nil(t, summary.SchedulableAfter, "a deleted account must not inherit the action-based fallback")
	require.Equal(t, []bool{true}, scheduling.actions)
}

func TestScheduledQuestionV2RunPausesAccountCurrentlyOpenAfterManualChange(t *testing.T) {
	svc, repo, accounts, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.AccountIDs, plan.QuestionIDs = []int64{1}, []int64{1}
	plan.AutoPauseEnabled = true
	repo.plans[1] = plan
	account := accounts.accounts[1]
	account.Schedulable = false
	scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
	svc.accounts = scheduling
	svc.batch = &scheduledQuestionBatchStub{run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
		account.Schedulable = true
		return scheduledQuestionV2EmitItems(emit, 1, "failed")
	}}
	run, err := svc.run(context.Background(), plan.ID, false)
	require.NoError(t, err)
	summary := scheduledQuestionV2Summary(t, run, 1)
	require.Equal(t, "failed", summary.Result)
	require.Equal(t, scheduledQuestionPtr(false), summary.SchedulableBefore)
	require.Equal(t, scheduledQuestionPtr(false), summary.SchedulableAfter)
	require.Equal(t, "paused", summary.ScheduleAction)
	require.True(t, summary.AutoPaused)
	require.Equal(t, 1, run.PausedAccounts)
	require.Equal(t, []bool{false}, scheduling.actions, "U1 pause eligibility uses current scheduling inside the verdict lock")
	require.Equal(t, []bool{true}, repo.ownerChanges)
	require.False(t, account.Schedulable)
}
