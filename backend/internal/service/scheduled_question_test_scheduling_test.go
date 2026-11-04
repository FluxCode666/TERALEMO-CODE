//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type scheduledQuestionSchedulingRepo struct {
	*scheduledQuestionAccountRepoStub
	actions  []bool
	setErr   error
	deadline time.Time
}

type scheduledQuestionSchedulingNotifier struct {
	webhook, secret string
	calls           int
	success         bool
	detail          string
}

func (n *scheduledQuestionSchedulingNotifier) Send(_ context.Context, webhook, secret string, _ scheduledQuestionNotification) (bool, string) {
	n.calls++
	n.webhook, n.secret = webhook, secret
	return n.success, n.detail
}

func TestScheduledQuestionFeishuTestCredentialResolution(t *testing.T) {
	for _, tc := range []struct {
		name                string
		input               ScheduledQuestionTestFeishuTestInput
		wantURL, wantSecret string
		status              int
	}{
		{name: "missing webhook", status: 400},
		{name: "stored credentials", input: ScheduledQuestionTestFeishuTestInput{PlanID: 1}, wantSecret: "stored-secret"},
		{name: "explicit secret cleared", input: ScheduledQuestionTestFeishuTestInput{PlanID: 1, Secret: scheduledQuestionPtr("")}},
		{name: "custom URL inherits secret", input: ScheduledQuestionTestFeishuTestInput{PlanID: 1, WebhookURL: " https://open.larksuite.com/open-apis/bot/v2/hook/override-token "}, wantURL: "https://open.larksuite.com/open-apis/bot/v2/hook/override-token", wantSecret: "stored-secret"},
		{name: "custom secret trimmed", input: ScheduledQuestionTestFeishuTestInput{PlanID: 1, Secret: scheduledQuestionPtr(" new-secret ")}, wantSecret: "new-secret"},
		{name: "invalid URL", input: ScheduledQuestionTestFeishuTestInput{WebhookURL: "https://evil.invalid/private-token"}, status: 400},
		{name: "secret too long", input: ScheduledQuestionTestFeishuTestInput{PlanID: 1, Secret: scheduledQuestionPtr(strings.Repeat("x", 201))}, status: 400},
		{name: "missing plan", input: ScheduledQuestionTestFeishuTestInput{PlanID: 99}, status: 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			plan := scheduledQuestionValidPlan(1)
			plan.FeishuWebhookURL, plan.FeishuSecret = "https://open.feishu.cn/open-apis/bot/v2/hook/stored-token", "stored-secret"
			repo.plans[1] = plan
			notifier := &scheduledQuestionSchedulingNotifier{success: true}
			svc.feishu = notifier
			err := svc.TestFeishu(context.Background(), tc.input)
			if tc.status != 0 {
				require.Error(t, err)
				if tc.status == 400 {
					var validation *model.ValidationError
					require.ErrorAs(t, err, &validation)
				} else {
					require.Equal(t, tc.status, infraerrors.Code(err))
				}
				require.Zero(t, notifier.calls)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, notifier.calls)
			wantURL := tc.wantURL
			if wantURL == "" {
				wantURL = plan.FeishuWebhookURL
			}
			require.Equal(t, wantURL, notifier.webhook)
			require.Equal(t, tc.wantSecret, notifier.secret)
		})
	}
}

func TestScheduledQuestionChannelsAndReportsAreIndependent(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		notify, report, success bool
		wantKinds               []string
	}{
		{"report without alerts", false, true, true, []string{"report"}},
		{"alert without report", true, false, true, []string{"alert"}},
		{"alert and report", true, true, true, []string{"alert", "report"}},
		{"failed notifications keep round successful", true, true, false, []string{"alert", "report"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			plan := scheduledQuestionValidPlan(1)
			plan.NotifyEnabled, plan.ReportEnabled = tc.notify, tc.report
			plan.EmailEnabled, plan.FeishuEnabled = false, true
			plan.FeishuWebhookURL = "https://open.feishu.cn/open-apis/bot/v2/hook/private-token"
			repo.plans[1] = plan
			notifier := &scheduledQuestionSchedulingNotifier{success: tc.success, detail: "test result"}
			svc.feishu = notifier
			svc.batch = &scheduledQuestionBatchStub{run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
				for _, id := range plan.AccountIDs {
					for _, question := range plan.QuestionIDs {
						if err := emit(AccountBatchTestEvent{Type: "item_result", Data: AccountBatchTestItemResult{AccountID: id, QuestionID: question, Status: "failed", Judged: true}}); err != nil {
							return err
						}
					}
				}
				return nil
			}}
			run, err := svc.run(context.Background(), 1, false)
			require.NoError(t, err)
			require.Equal(t, "success", run.Status)
			require.Len(t, run.NotificationResults, len(tc.wantKinds))
			for i, result := range run.NotificationResults {
				require.Equal(t, tc.wantKinds[i], result.Kind)
				require.Equal(t, "feishu", result.Channel)
				require.Equal(t, tc.success, result.Success)
			}
			for _, state := range repo.finishState {
				if tc.notify && tc.success {
					require.NotNil(t, state.LastAlertAt)
				} else {
					require.Nil(t, state.LastAlertAt)
				}
			}
		})
	}
}

func TestScheduledQuestionFeishuTestFailureIsSanitized(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.FeishuWebhookURL = "https://open.feishu.cn/open-apis/bot/v2/hook/private-token-1234"
	plan.FeishuSecret = "private-secret"
	repo.plans[1] = plan
	svc.feishu = &scheduledQuestionSchedulingNotifier{detail: "Post " + plan.FeishuWebhookURL + ": private-token-1234 private-secret raw provider body"}
	err := svc.TestFeishu(context.Background(), ScheduledQuestionTestFeishuTestInput{PlanID: 1})
	require.Equal(t, 502, infraerrors.Code(err))
	require.Equal(t, "notification failed", infraerrors.Message(err))
	require.NotContains(t, err.Error(), plan.FeishuWebhookURL)
	require.NotContains(t, err.Error(), "private-token-1234")
	require.NotContains(t, err.Error(), "private-secret")
	require.NotContains(t, err.Error(), "raw provider body")
}

func TestScheduledQuestionSchedulingContinuesAfterAccountFailure(t *testing.T) {
	svc, repo, accounts, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.AutoPauseEnabled = true
	repo.plans[1] = plan
	scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts, setErr: errors.New("storage error")}
	svc.accounts = scheduling
	for _, id := range plan.AccountIDs {
		accounts.accounts[id].Schedulable = true
	}
	run := &ScheduledQuestionTestRun{Summary: []ScheduledQuestionTestAccountSummary{
		{AccountID: 2, Alerting: true, Judged: 2, Total: 2, Ratio: scheduledQuestionPtr(0)},
		{AccountID: 1, Alerting: true, Judged: 2, Total: 2, Ratio: scheduledQuestionPtr(0)},
	}}
	states, err := svc.updateAlertStates(context.Background(), plan, run)
	require.NoError(t, err)
	require.Len(t, scheduling.actions, 2)
	require.Zero(t, run.PausedAccounts)
	for i, summary := range run.Summary {
		require.Equal(t, "pause_failed", summary.ScheduleAction)
		require.False(t, states[i].AutoPaused)
	}
}

func (r *scheduledQuestionSchedulingRepo) SetSchedulable(ctx context.Context, id int64, value bool) error {
	r.actions = append(r.actions, value)
	r.deadline, _ = ctx.Deadline()
	if r.setErr == nil {
		r.accounts[id].Schedulable = value
	}
	return r.setErr
}

func TestScheduledQuestionSchedulingOwnership(t *testing.T) {
	for _, tc := range []struct {
		name                                                                          string
		pause, resume, owned, schedulable, alert, incomplete, unjudged, missing, fail bool
		action                                                                        string
		ownedAfter                                                                    bool
		calls                                                                         int
	}{
		{name: "pause schedulable failure", pause: true, schedulable: true, alert: true, action: "paused", ownedAfter: true, calls: 1},
		{name: "manual pause never claimed", pause: true, alert: true},
		{name: "other plan pause never resumed", resume: true},
		{name: "resume owned", resume: true, owned: true, action: "resumed", calls: 1},
		{name: "manual resume clears ownership", resume: true, owned: true, schedulable: true},
		{name: "resume off preserves ownership", owned: true, ownedAfter: true},
		{name: "unjudged preserves ownership", resume: true, owned: true, unjudged: true, ownedAfter: true},
		{name: "incomplete cannot pause", pause: true, schedulable: true, alert: true, incomplete: true},
		{name: "incomplete cannot resume", resume: true, owned: true, incomplete: true, ownedAfter: true},
		{name: "missing account ignored", pause: true, schedulable: true, alert: true, missing: true},
		{name: "pause failure unowned", pause: true, schedulable: true, alert: true, fail: true, action: "pause_failed", calls: 1},
		{name: "resume failure stays owned", resume: true, owned: true, fail: true, action: "resume_failed", ownedAfter: true, calls: 1},
		{name: "pause off", schedulable: true, alert: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, accounts, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			plan := scheduledQuestionValidPlan(1)
			plan.AutoPauseEnabled, plan.AutoResumeEnabled = tc.pause, tc.resume
			repo.plans[1] = plan
			accounts.accounts[2].Schedulable = tc.schedulable
			if tc.missing {
				delete(accounts.accounts, 2)
			}
			scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
			if tc.fail {
				scheduling.setErr = errors.New("scheduler unavailable")
			}
			svc.accounts = scheduling
			previous := &ScheduledQuestionTestAccountState{PlanID: 1, AccountID: 2, AutoPaused: tc.owned, State: "ok"}
			if tc.owned {
				previous.AutoPausedAt = scheduledQuestionPtr(svc.now().Add(-time.Hour))
			}
			repo.states = []*ScheduledQuestionTestAccountState{previous}
			summary := ScheduledQuestionTestAccountSummary{AccountID: 2, Total: 2, Judged: 2, Ratio: scheduledQuestionPtr(100), Alerting: tc.alert, Incomplete: tc.incomplete}
			if tc.unjudged {
				summary.Ratio, summary.Judged = nil, 0
			}
			run := &ScheduledQuestionTestRun{Summary: []ScheduledQuestionTestAccountSummary{summary}}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			states, err := svc.updateAlertStates(ctx, plan, run)
			require.NoError(t, err)
			require.Equal(t, tc.action, run.Summary[0].ScheduleAction)
			require.Equal(t, tc.ownedAfter, states[0].AutoPaused)
			require.Equal(t, tc.ownedAfter, run.Summary[0].AutoPaused)
			require.Equal(t, tc.owned, previous.AutoPaused, "do not mutate previous snapshot")
			require.Len(t, scheduling.actions, tc.calls)
			if tc.calls > 0 {
				require.False(t, scheduling.deadline.IsZero())
			}
			if tc.action == "paused" {
				require.Equal(t, 1, run.PausedAccounts)
				require.True(t, states[0].AutoPausedAt.Equal(svc.now()))
			}
			if tc.action == "resumed" {
				require.Equal(t, 1, run.ResumedAccounts)
				require.Nil(t, states[0].AutoPausedAt)
			}
		})
	}
}

func TestScheduledQuestionAllErrorsProtection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		count    int
		statuses []string
		skipped  bool
		paused   int
	}{
		{"all accounts errored", 2, []string{"error", "error"}, true, 0},
		{"single account allowed", 1, []string{"error"}, false, 1},
		{"one unjudged prevents protection", 2, []string{"error", "unjudged"}, false, 1},
		{"judged failure allows pause", 2, []string{"error", "failed"}, false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, accounts, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			plan := scheduledQuestionValidPlan(1)
			plan.AutoPauseEnabled = true
			plan.AccountIDs, plan.QuestionIDs = []int64{1, 2}[:tc.count], []int64{1}
			repo.plans[1] = plan
			scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
			svc.accounts = scheduling
			items := []AccountBatchTestItemResult{}
			for i, status := range tc.statuses {
				id := int64(i + 1)
				accounts.accounts[id].Schedulable = true
				items = append(items, AccountBatchTestItemResult{AccountID: id, Status: status, Judged: status == "failed", Error: "upstream private body"})
			}
			run := &ScheduledQuestionTestRun{Summary: summarizeScheduledQuestionTests(plan, items, "")}
			_, err := svc.updateAlertStates(context.Background(), plan, run)
			require.NoError(t, err)
			require.Equal(t, tc.skipped, run.AutoPauseSkipped)
			require.Equal(t, tc.paused, run.PausedAccounts)
		})
	}
}

func TestScheduledQuestionIncompleteAndSuppression(t *testing.T) {
	for _, reason := range []string{"incomplete", "state read failed", "disabled current", "cancelled", "current pause off"} {
		t.Run(reason, func(t *testing.T) {
			svc, repo, accounts, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			plan := scheduledQuestionValidPlan(1)
			plan.AutoPauseEnabled, plan.ReportEnabled, plan.NotifyEnabled = true, true, true
			current := *plan
			repo.plans[1] = &current
			scheduling := &scheduledQuestionSchedulingRepo{scheduledQuestionAccountRepoStub: accounts}
			svc.accounts = scheduling
			accounts.accounts[2].Schedulable = true
			mail := &scheduledQuestionEmailStub{}
			svc.email, svc.settings = mail, &scheduledQuestionSettingsStub{values: map[string]string{SettingKeyAccountQuotaNotifyEmails: `[{"email":"admin@example.com","verified":true}]`}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			items := []AccountBatchTestItemResult{{AccountID: 2, Status: "failed", Judged: true}}
			if reason != "incomplete" {
				items = append(items, items[0])
			}
			if reason == "state read failed" {
				repo.stateErr = errors.New("state database unavailable")
			}
			if reason == "disabled current" {
				current.Enabled = false
			}
			if reason == "cancelled" {
				cancel()
			}
			if reason == "current pause off" {
				current.AutoPauseEnabled = false
			}
			run := &ScheduledQuestionTestRun{Summary: summarizeScheduledQuestionTests(plan, items, "round interrupted")}
			states, err := svc.updateAlertStates(context.Background(), plan, run, ctx)
			if reason == "state read failed" {
				require.Error(t, err)
				require.Nil(t, states)
			} else {
				require.NoError(t, err)
			}
			require.Empty(t, scheduling.actions)
			if reason != "incomplete" && reason != "current pause off" {
				require.Empty(t, mail.mails)
			}
			if reason == "incomplete" {
				require.True(t, run.Summary[0].Incomplete)
			}
		})
	}
}

func TestScheduledQuestionNotificationFieldsDefaultsAndUpdates(t *testing.T) {
	svc, repo, _, questions := newScheduledQuestionFixture()
	defer svc.Stop()
	plan, err := svc.Create(context.Background(), scheduledQuestionValidInput())
	require.NoError(t, err)
	require.True(t, plan.ReportEnabled)
	require.True(t, plan.EmailEnabled)
	require.False(t, plan.AutoPauseEnabled)
	require.False(t, plan.AutoResumeEnabled)
	require.False(t, plan.FeishuEnabled)
	webhook := "https://open.feishu.cn/open-apis/bot/v2/hook/12345678ab12"
	updated, err := svc.Update(context.Background(), plan.ID, ScheduledQuestionTestPlanInput{
		FeishuWebhookURL: scheduledQuestionPtr(" " + webhook + " "), FeishuSecret: scheduledQuestionPtr(" secret "), FeishuEnabled: scheduledQuestionPtr(true), AutoPauseEnabled: scheduledQuestionPtr(true),
	})
	require.NoError(t, err)
	require.Equal(t, webhook, updated.FeishuWebhookURL)
	require.Equal(t, "secret", updated.FeishuSecret)
	require.Equal(t, "open.feishu.cn/…/hook/****ab12", updated.FeishuWebhookHint)
	require.True(t, updated.FeishuWebhookConfigured)
	require.True(t, updated.FeishuSecretConfigured)
	require.True(t, repo.mask.FeishuWebhookURL)
	require.True(t, repo.mask.AutoPauseEnabled)
	updated, err = svc.Update(context.Background(), plan.ID, ScheduledQuestionTestPlanInput{Name: scheduledQuestionPtr("rename")})
	require.NoError(t, err)
	require.Equal(t, webhook, updated.FeishuWebhookURL)
	require.False(t, repo.mask.FeishuWebhookURL)
	_, err = svc.Update(context.Background(), plan.ID, ScheduledQuestionTestPlanInput{FeishuWebhookURL: scheduledQuestionPtr("")})
	var validation *model.ValidationError
	require.ErrorAs(t, err, &validation)
	require.Equal(t, "feishu_webhook_url", validation.Field)
	updated, err = svc.Update(context.Background(), plan.ID, ScheduledQuestionTestPlanInput{FeishuEnabled: scheduledQuestionPtr(false), FeishuWebhookURL: scheduledQuestionPtr(""), FeishuSecret: scheduledQuestionPtr(""), EmailEnabled: scheduledQuestionPtr(false)})
	require.NoError(t, err)
	require.False(t, updated.FeishuWebhookConfigured)
	require.False(t, updated.FeishuSecretConfigured)
	questions.err = errors.New("dependency unavailable")
	_, err = svc.Update(context.Background(), plan.ID, ScheduledQuestionTestPlanInput{FeishuWebhookURL: scheduledQuestionPtr("http://evil.invalid/secret")})
	require.ErrorAs(t, err, &validation)
	require.Equal(t, "feishu_webhook_url", validation.Field)
	_, err = svc.Update(context.Background(), plan.ID, ScheduledQuestionTestPlanInput{FeishuSecret: scheduledQuestionPtr(strings.Repeat("a", 201))})
	require.ErrorAs(t, err, &validation)
	require.Equal(t, "feishu_secret", validation.Field)
	plan.FeishuWebhookURL, plan.FeishuSecret = webhook, "never-expose-secret"
	repo.due = []*ScheduledQuestionTestPlan{plan}
	listed, err := svc.List(context.Background())
	require.NoError(t, err)
	payload, err := json.Marshal(listed)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "12345678ab12")
	require.NotContains(t, string(payload), "never-expose-secret")
}
