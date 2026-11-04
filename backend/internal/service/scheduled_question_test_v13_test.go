//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

type scheduledQuestionRefreshDeadlineRepo struct {
	*scheduledQuestionRepoStub
	listCalls    int
	finishOK     bool
	finishBudget time.Duration
}

func (r *scheduledQuestionRefreshDeadlineRepo) ListStates(ctx context.Context, planID int64) ([]*ScheduledQuestionTestAccountState, error) {
	r.listCalls++
	if r.listCalls == 1 {
		return r.scheduledQuestionRepoStub.ListStates(ctx, planID)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (r *scheduledQuestionRefreshDeadlineRepo) FinishRun(ctx context.Context, run *ScheduledQuestionTestRun, states []*ScheduledQuestionTestAccountState, next time.Time) (*ScheduledQuestionTestRun, error) {
	if ctx.Err() == nil {
		r.finishOK = true
	}
	deadline, _ := ctx.Deadline()
	r.finishBudget = time.Until(deadline)
	return r.scheduledQuestionRepoStub.FinishRun(ctx, run, states, next)
}

func TestScheduledQuestionRefreshTimeoutDoesNotConsumeFinishContext(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.AccountIDs, plan.QuestionIDs = []int64{2}, []int64{1}
	repo.plans[1] = plan
	svc.ownershipRefreshTimeout = 20 * time.Millisecond
	blockingRepo := &scheduledQuestionRefreshDeadlineRepo{scheduledQuestionRepoStub: repo}
	svc.repo = blockingRepo
	svc.batch = &scheduledQuestionBatchStub{run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
		return emit(AccountBatchTestEvent{Type: "item_result", Data: AccountBatchTestItemResult{AccountID: 2, QuestionID: 1, Status: "passed", Judged: true}})
	}}
	run, err := svc.run(context.Background(), plan.ID, false)
	require.NoError(t, err)
	require.True(t, blockingRepo.finishOK, "FinishRun must receive a fresh, unexpired context")
	require.Greater(t, blockingRepo.finishBudget, 29*time.Second)
	require.Len(t, repo.finished, 1)
	require.Equal(t, run, repo.finished[0])
}

func TestScheduledQuestionErrorSanitizerRepeatsDecodedURLs(t *testing.T) {
	for _, tc := range []struct{ input, expected string }{
		{
			input:    "API failed https://one.example/path%2Chttps%3A%2F%2Fsecond-user%3Asecond-password%40two.example/api?api_key=OUTER_QUERY#FRAGMENT",
			expected: "API failed https://one.example/path,https://two.example/api",
		},
		{
			input:    "https://provider.example/safe/%25ZZ/%2568ook%252FUNKNOWN_TOKEN?key=PRIVATE_QUERY",
			expected: "https://provider.example/safe/%25ZZ/hook/****",
		},
	} {
		got := sanitizeScheduledQuestionErrorText(tc.input, nil, 1000)
		require.Equal(t, tc.expected, got)
		require.Equal(t, got, sanitizeScheduledQuestionErrorText(got, nil, 1000), "sanitization must be idempotent")
		for _, secret := range []string{"second-user", "second-password", "UNKNOWN_TOKEN", "PRIVATE_QUERY", "OUTER_QUERY", "FRAGMENT"} {
			require.NotContains(t, got, secret)
		}
	}
}

func TestScheduledQuestionErrorSanitizerProducesValidUTF8(t *testing.T) {
	for _, text := range []string{
		"https://provider.example/safe/%FF%FE?api_key=PRIVATE_QUERY",
		"https://provider.example/safe/%2525FF?api_key=PRIVATE_QUERY",
		"https://provider.example/safe/%FF%FE?api_key=PRIVATE_QUERY" + strings.Repeat("x", 400),
	} {
		for _, limit := range []int{1000, 300, 500} {
			got := sanitizeScheduledQuestionErrorText(text, nil, limit)
			require.True(t, utf8.ValidString(got))
			require.NotContains(t, got, "PRIVATE_QUERY")
		}
	}
	for _, limit := range []int{1000, 300, 500} {
		got := truncateScheduledQuestionText(strings.Repeat("x", 299)+"\xff\xfe"+strings.Repeat("y", 250), limit)
		require.True(t, utf8.ValidString(got))
		require.LessOrEqual(t, utf8.RuneCountInString(got), limit)
	}
}

func TestScheduledQuestionDecodedURLSanitizationReachesAllSurfaces(t *testing.T) {
	for _, raw := range []string{
		"API failed https://one.example/path%2Chttps%3A%2F%2Fsecond-user%3Asecond-password%40two.example/api?api_key=OUTER_QUERY#FRAGMENT",
		"https://provider.example/safe/%25ZZ/%2568ook%252FUNKNOWN_TOKEN?key=PRIVATE_QUERY",
	} {
		svc, repo, _, _ := newScheduledQuestionFixture()
		plan := scheduledQuestionValidPlan(1)
		plan.AccountIDs, plan.QuestionIDs = []int64{1}, []int64{1}
		repo.plans[1] = plan
		summaries := summarizeScheduledQuestionTests(plan, []AccountBatchTestItemResult{{AccountID: 1, Status: "error", Error: raw}}, "")
		repo.runs = []*ScheduledQuestionTestRun{{Status: "failed", ErrorMessage: raw, Summary: []ScheduledQuestionTestAccountSummary{{AccountID: 1, Error: raw}}}}
		history, err := svc.ListRuns(context.Background(), 1, 1)
		require.NoError(t, err)
		notification := buildScheduledQuestionNotification("Site", plan, repo.runs[0], "report", nil, time.Now())
		payload, err := scheduledQuestionFeishuPayload(notification, "", time.Now())
		require.NoError(t, err)
		for _, value := range []string{summaries[0].Error, history[0].Summary[0].Error, strings.Join(notification.Lines, "\n"), string(payload)} {
			for _, sensitive := range []string{"second-user", "second-password", "UNKNOWN_TOKEN", "PRIVATE_QUERY", "OUTER_QUERY", "FRAGMENT"} {
				require.NotContains(t, value, sensitive)
			}
		}
		svc.Stop()
	}
}

type scheduledQuestionResumeRaceRepo struct {
	*scheduledQuestionRepoStub
	account *Account
	resume  bool
}

func (r *scheduledQuestionResumeRaceRepo) ClaimAutoPauseIfOtherHeld(ctx context.Context, planID, accountID int64, at *time.Time) (bool, error) {
	claimed, err := r.scheduledQuestionRepoStub.ClaimAutoPauseIfOtherHeld(ctx, planID, accountID, at)
	if err == nil && !claimed && r.resume {
		r.account.Schedulable = true
	}
	return claimed, err
}

func TestScheduledQuestionJoinRechecksSchedulabilityAfterNoHolder(t *testing.T) {
	for _, tc := range []struct {
		name         string
		resume, fail bool
	}{
		{name: "completed other resume", resume: true},
		{name: "manual remains paused"},
		{name: "repause fails", resume: true, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, accounts, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			plan := scheduledQuestionValidPlan(2)
			plan.AutoPauseEnabled = true
			repo.plans[plan.ID] = plan
			account := accounts.accounts[2]
			account.Schedulable = false
			racingRepo := &scheduledQuestionResumeRaceRepo{scheduledQuestionRepoStub: repo, account: account, resume: tc.resume}
			svc.repo = racingRepo
			scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
			if tc.fail {
				scheduling.setErr = errors.New("injected scheduling failure")
			}
			svc.accounts = scheduling
			run := &ScheduledQuestionTestRun{Summary: []ScheduledQuestionTestAccountSummary{{
				AccountID: account.ID, Alerting: true, Ratio: scheduledQuestionPtr(0), Judged: 1, Total: 1,
			}}}
			states, err := svc.updateAlertStates(context.Background(), plan, run)
			require.NoError(t, err)
			if !tc.resume {
				require.Empty(t, scheduling.actions)
				require.Empty(t, run.Summary[0].ScheduleAction)
				require.False(t, states[0].AutoPaused)
				return
			}
			require.Equal(t, []bool{false}, scheduling.actions)
			if tc.fail {
				require.True(t, account.Schedulable)
				require.Equal(t, "pause_failed", run.Summary[0].ScheduleAction)
				require.False(t, states[0].AutoPaused)
				require.Equal(t, []bool{true, false}, repo.ownerChanges)
				return
			}
			require.False(t, account.Schedulable)
			require.Equal(t, "paused", run.Summary[0].ScheduleAction)
			require.True(t, states[0].AutoPaused)
			require.Equal(t, 1, run.PausedAccounts)
		})
	}
}
