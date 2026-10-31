//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/stretchr/testify/require"
)

type scheduledQuestionRepoStub struct {
	ScheduledQuestionTestRepository
	mu           sync.Mutex
	plans        map[int64]*ScheduledQuestionTestPlan
	states       []*ScheduledQuestionTestAccountState
	runs         []*ScheduledQuestionTestRun
	due          []*ScheduledQuestionTestPlan
	created      int
	updated      int
	deleted      int64
	mask         ScheduledQuestionTestPlanUpdateMask
	runLimit     int
	finished     []*ScheduledQuestionTestRun
	finishState  []*ScheduledQuestionTestAccountState
	nextRun      time.Time
	finishCtx    context.Context
	dueCalls     int
	getErr       error
	stateErr     error
	finishErr    error
	dueErr       error
	beforeFinish func(context.Context, *ScheduledQuestionTestRun)
}

func (r *scheduledQuestionRepoStub) GetByID(_ context.Context, id int64) (*ScheduledQuestionTestPlan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.plans[id], r.getErr
}

func (r *scheduledQuestionRepoStub) Create(_ context.Context, plan *ScheduledQuestionTestPlan) (*ScheduledQuestionTestPlan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.created++
	plan.ID = int64(len(r.plans) + 1)
	r.plans[plan.ID] = plan
	return plan, nil
}

func (r *scheduledQuestionRepoStub) Update(_ context.Context, plan *ScheduledQuestionTestPlan, mask ScheduledQuestionTestPlanUpdateMask) (*ScheduledQuestionTestPlan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updated++
	r.mask = mask
	r.plans[plan.ID] = plan
	return plan, nil
}

func (r *scheduledQuestionRepoStub) List(_ context.Context) ([]*ScheduledQuestionTestPlan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*ScheduledQuestionTestPlan{}, r.due...), nil
}

func (r *scheduledQuestionRepoStub) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = id
	return nil
}

func (r *scheduledQuestionRepoStub) ListRuns(_ context.Context, _ int64, limit int) ([]*ScheduledQuestionTestRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runLimit = limit
	return append([]*ScheduledQuestionTestRun{}, r.runs...), nil
}

func (r *scheduledQuestionRepoStub) ListDue(ctx context.Context, _ time.Time) ([]*ScheduledQuestionTestPlan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dueCalls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.due, r.dueErr
}

func (r *scheduledQuestionRepoStub) ListStates(_ context.Context, _ int64) ([]*ScheduledQuestionTestAccountState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*ScheduledQuestionTestAccountState{}, r.states...), r.stateErr
}

func (r *scheduledQuestionRepoStub) FinishRun(ctx context.Context, run *ScheduledQuestionTestRun, states []*ScheduledQuestionTestAccountState, next time.Time) (*ScheduledQuestionTestRun, error) {
	if r.beforeFinish != nil {
		r.beforeFinish(ctx, run)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.finished = append(r.finished, run)
	r.finishState, r.nextRun, r.finishCtx = states, next, ctx
	return run, r.finishErr
}

type scheduledQuestionAccountRepoStub struct {
	AccountRepository
	accounts map[int64]*Account
	err      error
}

func (r *scheduledQuestionAccountRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	return r.accounts[id], r.err
}

type scheduledQuestionQuestionRepoStub struct {
	AccountTestQuestionRepository
	questions map[int64]*model.AccountTestQuestion
	err       error
}

func (r *scheduledQuestionQuestionRepoStub) GetByID(_ context.Context, id int64) (*model.AccountTestQuestion, error) {
	return r.questions[id], r.err
}

type scheduledQuestionBatchStub struct {
	prepare func(context.Context, AccountBatchTestInput) (*AccountBatchTestPlan, error)
	run     func(context.Context, *AccountBatchTestPlan, func(AccountBatchTestEvent) error) error
}

func (b *scheduledQuestionBatchStub) Prepare(ctx context.Context, input AccountBatchTestInput) (*AccountBatchTestPlan, error) {
	if b.prepare != nil {
		return b.prepare(ctx, input)
	}
	return &AccountBatchTestPlan{}, nil
}

func (b *scheduledQuestionBatchStub) Run(ctx context.Context, plan *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
	if b.run != nil {
		return b.run(ctx, plan, emit)
	}
	return nil
}

type scheduledQuestionSettingsStub struct {
	SettingRepository
	values map[string]string
	errors map[string]error
	keys   []string
}

func (r *scheduledQuestionSettingsStub) GetValue(_ context.Context, key string) (string, error) {
	r.keys = append(r.keys, key)
	return r.values[key], r.errors[key]
}

type scheduledQuestionMail struct {
	to, subject, body string
}

type scheduledQuestionEmailStub struct {
	mails     []scheduledQuestionMail
	errors    map[string]error
	deadlines []time.Time
}

func (s *scheduledQuestionEmailStub) SendEmail(ctx context.Context, to, subject, body string) error {
	s.mails = append(s.mails, scheduledQuestionMail{to, subject, body})
	if deadline, ok := ctx.Deadline(); ok {
		s.deadlines = append(s.deadlines, deadline)
	}
	return s.errors[to]
}

func scheduledQuestionPtr[T any](v T) *T { return &v }

func newScheduledQuestionFixture() (*ScheduledQuestionTestService, *scheduledQuestionRepoStub, *scheduledQuestionAccountRepoStub, *scheduledQuestionQuestionRepoStub) {
	repo := &scheduledQuestionRepoStub{plans: make(map[int64]*ScheduledQuestionTestPlan)}
	accounts := &scheduledQuestionAccountRepoStub{accounts: make(map[int64]*Account)}
	questions := &scheduledQuestionQuestionRepoStub{questions: make(map[int64]*model.AccountTestQuestion)}
	for id := int64(1); id <= 200; id++ {
		accounts.accounts[id] = &Account{ID: id, Name: fmt.Sprintf("account-%d", id), Platform: PlatformAnthropic}
	}
	for id := int64(1); id <= 20; id++ {
		questions.questions[id] = &model.AccountTestQuestion{ID: id, Enabled: true}
	}
	svc := NewScheduledQuestionTestService(repo, accounts, NewAccountTestQuestionService(questions), nil, nil, nil)
	svc.batch = &scheduledQuestionBatchStub{}
	svc.now = func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) }
	return svc, repo, accounts, questions
}

func scheduledQuestionValidInput() ScheduledQuestionTestPlanInput {
	return ScheduledQuestionTestPlanInput{
		Name: scheduledQuestionPtr(" identity test "), AccountIDs: scheduledQuestionPtr([]int64{2, 1, 2}),
		QuestionIDs: scheduledQuestionPtr([]int64{2, 1, 2}), CronExpression: scheduledQuestionPtr(" */5 * * * * "),
	}
}

func scheduledQuestionValidPlan(id int64) *ScheduledQuestionTestPlan {
	return &ScheduledQuestionTestPlan{
		ID: id, Name: "identity test", AccountIDs: []int64{2, 1}, QuestionIDs: []int64{2, 1},
		CronExpression: "*/5 * * * *", Enabled: true, NotifyEnabled: false, MinPassRatio: 80, CooldownMinutes: 360,
	}
}

func TestScheduledQuestionSummaries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		statuses   []string
		threshold  int
		passed     int
		judged     int
		ratio      *int
		alerting   bool
		roundError string
		errorText  string
	}{
		{name: "two thirds rounded", statuses: []string{"passed", "passed", "failed"}, threshold: 80, passed: 2, judged: 3, ratio: scheduledQuestionPtr(67), alerting: true},
		{name: "exact threshold passes", statuses: []string{"passed", "passed", "failed"}, threshold: 67, passed: 2, judged: 3, ratio: scheduledQuestionPtr(67)},
		{name: "rounded threshold misses", statuses: []string{"passed", "passed", "failed"}, threshold: 68, passed: 2, judged: 3, ratio: scheduledQuestionPtr(67), alerting: true},
		{name: "one third rounds down", statuses: []string{"passed", "failed", "failed"}, threshold: 34, passed: 1, judged: 3, ratio: scheduledQuestionPtr(33), alerting: true},
		{name: "zero threshold", statuses: []string{"failed", "failed", "failed"}, threshold: 0, judged: 3, ratio: scheduledQuestionPtr(0)},
		{name: "hundred threshold", statuses: []string{"passed", "passed", "passed"}, threshold: 100, passed: 3, judged: 3, ratio: scheduledQuestionPtr(100)},
		{name: "hundred threshold fails", statuses: []string{"passed", "failed", "passed"}, threshold: 100, passed: 2, judged: 3, ratio: scheduledQuestionPtr(67), alerting: true},
		{name: "all unjudged", statuses: []string{"unjudged", "unjudged", "unjudged"}, threshold: 100},
		{name: "unjudged mixed error", statuses: []string{"unjudged", "error", "unjudged"}, threshold: 80, errorText: "upstream unavailable"},
		{name: "errors excluded from judged", statuses: []string{"passed", "error", "failed"}, threshold: 80, passed: 1, judged: 2, ratio: scheduledQuestionPtr(50), alerting: true, errorText: "upstream unavailable"},
		{name: "all errors even zero threshold", statuses: []string{"error", "error", "error"}, threshold: 0, ratio: scheduledQuestionPtr(0), alerting: true, errorText: "upstream unavailable"},
		{name: "missing all items", threshold: 80, ratio: scheduledQuestionPtr(0), alerting: true, errorText: "Account test did not complete"},
		{name: "missing from failed round", threshold: 80, ratio: scheduledQuestionPtr(0), alerting: true, roundError: "batch cancelled", errorText: "batch cancelled"},
		{name: "partial judged missing", statuses: []string{"passed"}, threshold: 80, passed: 1, judged: 1, ratio: scheduledQuestionPtr(100), roundError: "batch cancelled", errorText: "batch cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := &ScheduledQuestionTestPlan{AccountIDs: []int64{7}, QuestionIDs: []int64{1, 2, 3}, MinPassRatio: tc.threshold}
			items := make([]AccountBatchTestItemResult, 0, len(tc.statuses))
			for _, status := range tc.statuses {
				item := AccountBatchTestItemResult{AccountID: 7, AccountName: "claude-07", Platform: PlatformAnthropic, Status: status, Judged: status != "unjudged"}
				if status == "error" {
					item.Error = "upstream unavailable"
				}
				items = append(items, item)
			}
			summaries := summarizeScheduledQuestionTests(plan, items, tc.roundError)
			require.Len(t, summaries, 1)
			summary := summaries[0]
			require.Equal(t, 3, summary.Total)
			require.Equal(t, tc.passed, summary.Passed)
			require.Equal(t, tc.judged, summary.Judged)
			require.Equal(t, tc.ratio, summary.Ratio)
			require.Equal(t, tc.alerting, summary.Alerting)
			require.Equal(t, tc.errorText, summary.Error)
			state := "ok"
			if tc.alerting {
				state = "alert"
			}
			require.Equal(t, state, summary.StateAfter)
			encoded, err := json.Marshal(summary)
			require.NoError(t, err)
			if tc.ratio == nil {
				require.Contains(t, string(encoded), `"ratio":null`)
			}
		})
	}
}

func TestScheduledQuestionSummariesStableAccountOrder(t *testing.T) {
	plan := &ScheduledQuestionTestPlan{AccountIDs: []int64{7, 2, 9}, QuestionIDs: []int64{1}, MinPassRatio: 80}
	items := []AccountBatchTestItemResult{
		{AccountID: 9, AccountName: "last", Platform: PlatformAnthropic, Status: "passed", Judged: true},
		{AccountID: 2, AccountName: "middle", Platform: PlatformAnthropic, Status: "error"},
		{AccountID: 7, AccountName: "first", Platform: PlatformAnthropic, Status: "unjudged"},
	}
	result := summarizeScheduledQuestionTests(plan, items, "")
	require.Equal(t, []int64{7, 2, 9}, []int64{result[0].AccountID, result[1].AccountID, result[2].AccountID})
	require.Equal(t, "first", result[0].AccountName)
	require.Equal(t, "All account tests failed", result[1].Error)
	require.Equal(t, "last", result[2].AccountName)
}

func TestScheduledQuestionAlertTransition(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name         string
		state        string
		lastAgo      *time.Duration
		alerting     bool
		cooldown     time.Duration
		notification string
	}{
		{name: "new alert", alerting: true, cooldown: time.Hour, notification: "alert"},
		{name: "new healthy", cooldown: time.Hour},
		{name: "ok to alert", state: "ok", alerting: true, cooldown: time.Hour, notification: "alert"},
		{name: "ok ignores old send timestamp", state: "ok", lastAgo: scheduledQuestionPtr(time.Minute), alerting: true, cooldown: time.Hour, notification: "alert"},
		{name: "ok ignores future send timestamp", state: "ok", lastAgo: scheduledQuestionPtr(-time.Hour), alerting: true, cooldown: time.Hour, notification: "alert"},
		{name: "ok stays ok", state: "ok", cooldown: time.Hour},
		{name: "alert missing timestamp retries", state: "alert", alerting: true, cooldown: time.Hour, notification: "alert"},
		{name: "inside cooldown", state: "alert", lastAgo: scheduledQuestionPtr(time.Hour - time.Nanosecond), alerting: true, cooldown: time.Hour},
		{name: "exact cooldown", state: "alert", lastAgo: scheduledQuestionPtr(time.Hour), alerting: true, cooldown: time.Hour, notification: "alert"},
		{name: "expired cooldown", state: "alert", lastAgo: scheduledQuestionPtr(2 * time.Hour), alerting: true, cooldown: time.Hour, notification: "alert"},
		{name: "future timestamp", state: "alert", lastAgo: scheduledQuestionPtr(-time.Hour), alerting: true, cooldown: time.Hour},
		{name: "zero cooldown same instant", state: "alert", lastAgo: scheduledQuestionPtr(time.Duration(0)), alerting: true, notification: "alert"},
		{name: "zero cooldown future timestamp", state: "alert", lastAgo: scheduledQuestionPtr(-time.Nanosecond), alerting: true},
		{name: "recovery inside cooldown", state: "alert", lastAgo: scheduledQuestionPtr(time.Minute), cooldown: time.Hour, notification: "recovered"},
		{name: "recovery future timestamp", state: "alert", lastAgo: scheduledQuestionPtr(-time.Hour), cooldown: time.Hour, notification: "recovered"},
		{name: "recovery without previous mail", state: "alert", cooldown: time.Hour, notification: "recovered"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var previous *ScheduledQuestionTestAccountState
			if tc.state != "" {
				previous = &ScheduledQuestionTestAccountState{ID: 3, PlanID: 4, AccountID: 5, State: tc.state}
				if tc.lastAgo != nil {
					previous.LastAlertAt = scheduledQuestionPtr(now.Add(-*tc.lastAgo))
				}
			}
			next, notification := scheduledQuestionAlertTransition(previous, tc.alerting, now, tc.cooldown)
			require.Equal(t, tc.notification, notification)
			require.True(t, next.UpdatedAt.Equal(now))
			if tc.alerting {
				require.Equal(t, "alert", next.State)
				if previous != nil {
					require.Equal(t, previous.LastAlertAt, next.LastAlertAt, "the decision must not advance the timestamp before email succeeds")
				}
			} else {
				require.Equal(t, "ok", next.State)
				require.Nil(t, next.LastAlertAt)
			}
			if previous != nil {
				require.Equal(t, tc.state, previous.State, "the stored state must not be mutated before persistence")
				require.Equal(t, int64(3), next.ID)
			}
		})
	}
}

func TestScheduledQuestionNotificationRecipients(t *testing.T) {
	for _, tc := range []struct {
		name        string
		raw         string
		recipients  []string
		settingsErr bool
		smtpErrors  map[string]error
		sent        bool
	}{
		{name: "verified filtered trimmed deduplicated", raw: `[{"email":" Admin@example.com ","verified":true},{"email":"admin@EXAMPLE.com","verified":true},{"email":"disabled@example.com","verified":true,"disabled":true},{"email":"unverified@example.com"},{"email":" ","verified":true},{"email":"other@example.com","verified":true}]`, recipients: []string{"Admin@example.com", "other@example.com"}, sent: true},
		{name: "legacy arrays remain unverified", raw: `["legacy@example.com","other@example.com"]`},
		{name: "raw address is not valid settings JSON", raw: `legacy@example.com`},
		{name: "empty setting"},
		{name: "empty JSON array", raw: `[]`},
		{name: "null JSON", raw: `null`},
		{name: "all filtered", raw: `[{"email":"a@example.com","verified":false},{"email":"b@example.com","verified":true,"disabled":true}]`},
		{name: "read failed", raw: `[{"email":"a@example.com","verified":true}]`, settingsErr: true},
		{name: "SMTP fails", raw: `[{"email":"a@example.com","verified":true}]`, recipients: []string{"a@example.com"}, smtpErrors: map[string]error{"a@example.com": errors.New("smtp unavailable")}},
		{name: "one SMTP failure continues", raw: `[{"email":"a@example.com","verified":true},{"email":"b@example.com","verified":true}]`, recipients: []string{"a@example.com", "b@example.com"}, smtpErrors: map[string]error{"a@example.com": errors.New("smtp unavailable")}, sent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, _ := newScheduledQuestionFixture()
			settings := &scheduledQuestionSettingsStub{values: map[string]string{SettingKeyAccountQuotaNotifyEmails: tc.raw, SettingKeySiteName: "Example Site"}, errors: make(map[string]error)}
			if tc.settingsErr {
				settings.errors[SettingKeyAccountQuotaNotifyEmails] = errors.New("settings unavailable")
			}
			mail := &scheduledQuestionEmailStub{errors: tc.smtpErrors}
			svc.settings, svc.email = settings, mail
			plan := scheduledQuestionValidPlan(1)
			run := &ScheduledQuestionTestRun{StartedAt: svc.now(), Summary: []ScheduledQuestionTestAccountSummary{{AccountID: 1, AccountName: "claude-01", Passed: 2, Judged: 3, Ratio: scheduledQuestionPtr(67)}}}
			var logs bytes.Buffer
			previousLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			defer slog.SetDefault(previousLogger)
			sent := svc.sendScheduledQuestionNotification(context.Background(), plan, run, "alert", []int{0})
			require.Equal(t, tc.sent, sent)
			var recipients []string
			for _, message := range mail.mails {
				recipients = append(recipients, message.to)
				require.Contains(t, message.subject, "[Example Site]")
			}
			require.Equal(t, tc.recipients, recipients)
			require.Contains(t, settings.keys, SettingKeyAccountQuotaNotifyEmails)
			if !tc.sent {
				require.Contains(t, logs.String(), "level=WARN")
			}
			for _, deadline := range mail.deadlines {
				require.LessOrEqual(t, time.Until(deadline), emailSendTimeout)
			}
		})
	}
}

func TestScheduledQuestionNotificationEscapesAndOmitsCredentials(t *testing.T) {
	svc, _, _, _ := newScheduledQuestionFixture()
	svc.settings = &scheduledQuestionSettingsStub{values: map[string]string{SettingKeyAccountQuotaNotifyEmails: `[{"email":"admin@example.com","verified":true}]`}}
	mail := &scheduledQuestionEmailStub{}
	svc.email = mail
	plan := scheduledQuestionValidPlan(1)
	plan.Name = `<script>alert("plan")</script>`
	run := &ScheduledQuestionTestRun{StartedAt: svc.now(), Summary: []ScheduledQuestionTestAccountSummary{
		{AccountID: 42, AccountName: `<img src=x onerror="steal()"> & account`, Passed: 2, Judged: 3, Ratio: scheduledQuestionPtr(67), Error: "Authorization: Bearer sk-secret-password"},
	}}
	for _, kind := range []string{"alert", "recovered"} {
		require.True(t, svc.sendScheduledQuestionNotification(context.Background(), plan, run, kind, []int{0}))
	}
	require.Len(t, mail.mails, 2)
	for _, message := range mail.mails {
		require.Contains(t, message.subject, "["+defaultSiteName+"]")
		require.Contains(t, message.body, "&lt;script&gt;")
		require.Contains(t, message.body, "&lt;img")
		require.Contains(t, message.body, "&amp; account")
		require.NotContains(t, message.body, "<script>")
		require.NotContains(t, message.body, "<img")
		require.NotContains(t, message.body, "sk-secret-password")
		require.NotContains(t, message.body, "Authorization")
		require.Contains(t, message.body, "2/3 judged passed")
		require.Contains(t, message.body, "67%")
		require.Contains(t, message.body, "80%")
		require.Contains(t, message.body, svc.now().Format(time.RFC3339))
	}
	require.Contains(t, mail.mails[0].subject, "Scheduled test alert")
	require.Contains(t, mail.mails[1].subject, "Scheduled test recovered")
	for _, deps := range []struct{ settingsMissing, emailMissing bool }{{true, false}, {false, true}} {
		copy := &ScheduledQuestionTestService{settings: svc.settings, email: svc.email}
		if deps.settingsMissing {
			copy.settings = nil
		}
		if deps.emailMissing {
			copy.email = nil
		}
		require.False(t, copy.sendScheduledQuestionNotification(context.Background(), plan, run, "alert", []int{0}))
	}
}

func TestScheduledQuestionCRUDDefaultsAndPresence(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	input := scheduledQuestionValidInput()
	input.ModelID = scheduledQuestionPtr(" custom-model ")
	plan, err := svc.Create(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, 1, repo.created)
	require.Equal(t, "identity test", plan.Name)
	require.Equal(t, "custom-model", plan.ModelID)
	require.Equal(t, "*/5 * * * *", plan.CronExpression)
	require.Equal(t, []int64{2, 1}, plan.AccountIDs)
	require.Equal(t, []int64{2, 1}, plan.QuestionIDs)
	require.True(t, plan.Enabled)
	require.True(t, plan.NotifyEnabled)
	require.Equal(t, 80, plan.MinPassRatio)
	require.Equal(t, 360, plan.CooldownMinutes)
	require.NotNil(t, plan.NextRunAt)
	require.True(t, plan.NextRunAt.Equal(svc.now().Add(5*time.Minute)))
	(*input.AccountIDs)[0] = 199
	(*input.QuestionIDs)[0] = 20
	require.Equal(t, int64(2), plan.AccountIDs[0], "caller-owned slices must not mutate saved plans")
	require.Equal(t, int64(2), plan.QuestionIDs[0])
	oldNext := plan.NextRunAt
	updated, err := svc.Update(context.Background(), plan.ID, ScheduledQuestionTestPlanInput{Enabled: scheduledQuestionPtr(false)})
	require.NoError(t, err)
	require.False(t, updated.Enabled)
	require.Equal(t, plan.Name, updated.Name)
	require.Equal(t, oldNext, updated.NextRunAt)
	require.Equal(t, ScheduledQuestionTestPlanUpdateMask{Enabled: true}, repo.mask)
	updated, err = svc.Update(context.Background(), plan.ID, ScheduledQuestionTestPlanInput{
		MinPassRatio: scheduledQuestionPtr(0), CooldownMinutes: scheduledQuestionPtr(0), NotifyEnabled: scheduledQuestionPtr(false), ModelID: scheduledQuestionPtr(" "),
	})
	require.NoError(t, err)
	require.Zero(t, updated.MinPassRatio)
	require.Zero(t, updated.CooldownMinutes)
	require.False(t, updated.NotifyEnabled)
	require.Empty(t, updated.ModelID)
	require.Equal(t, ScheduledQuestionTestPlanUpdateMask{MinPassRatio: true, CooldownMinutes: true, NotifyEnabled: true, ModelID: true}, repo.mask)
	updated, err = svc.Update(context.Background(), plan.ID, ScheduledQuestionTestPlanInput{Enabled: scheduledQuestionPtr(true), CronExpression: scheduledQuestionPtr("*/10 * * * *")})
	require.NoError(t, err)
	require.True(t, updated.Enabled)
	require.True(t, updated.NextRunAt.Equal(svc.now().Add(10*time.Minute)))
	require.Equal(t, ScheduledQuestionTestPlanUpdateMask{Enabled: true, CronExpression: true, NextRunAt: true}, repo.mask)
	require.True(t, plan.Enabled, "partial updates must not mutate the previously loaded object")
	_, err = svc.Update(context.Background(), plan.ID, ScheduledQuestionTestPlanInput{Name: scheduledQuestionPtr(" renamed ")})
	require.NoError(t, err)
	require.Equal(t, "renamed", repo.plans[plan.ID].Name)
	require.Equal(t, ScheduledQuestionTestPlanUpdateMask{Name: true}, repo.mask)

	input = scheduledQuestionValidInput()
	input.Enabled, input.NotifyEnabled = scheduledQuestionPtr(false), scheduledQuestionPtr(false)
	input.MinPassRatio, input.CooldownMinutes = scheduledQuestionPtr(0), scheduledQuestionPtr(0)
	created, err := svc.Create(context.Background(), input)
	require.NoError(t, err)
	require.False(t, created.Enabled)
	require.False(t, created.NotifyEnabled)
	require.Zero(t, created.MinPassRatio)
	require.Zero(t, created.CooldownMinutes)
}

func TestScheduledQuestionCRUDValidation(t *testing.T) {
	ids := func(n int) []int64 {
		result := make([]int64, n)
		for i := range result {
			result[i] = int64(i + 1)
		}
		return result
	}
	for _, tc := range []struct {
		name   string
		field  string
		change func(*ScheduledQuestionTestPlanInput, *scheduledQuestionAccountRepoStub, *scheduledQuestionQuestionRepoStub)
	}{
		{name: "blank name", field: "name", change: func(in *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			in.Name = scheduledQuestionPtr(" ")
		}},
		{name: "name too long", field: "name", change: func(in *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			in.Name = scheduledQuestionPtr(strings.Repeat("n", 101))
		}},
		{name: "model too long", field: "model_id", change: func(in *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			in.ModelID = scheduledQuestionPtr(strings.Repeat("m", 101))
		}},
		{name: "ratio negative", field: "min_pass_ratio", change: func(in *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			in.MinPassRatio = scheduledQuestionPtr(-1)
		}},
		{name: "ratio over hundred", field: "min_pass_ratio", change: func(in *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			in.MinPassRatio = scheduledQuestionPtr(101)
		}},
		{name: "cooldown negative", field: "cooldown_minutes", change: func(in *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			in.CooldownMinutes = scheduledQuestionPtr(-1)
		}},
		{name: "cooldown over week", field: "cooldown_minutes", change: func(in *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			in.CooldownMinutes = scheduledQuestionPtr(10081)
		}},
		{name: "empty cron", field: "cron_expression", change: func(in *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			in.CronExpression = scheduledQuestionPtr("")
		}},
		{name: "invalid cron", field: "cron_expression", change: func(in *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			in.CronExpression = scheduledQuestionPtr("61 * * * *")
		}},
		{name: "cron seconds rejected", field: "cron_expression", change: func(in *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			in.CronExpression = scheduledQuestionPtr("0 */5 * * * *")
		}},
		{name: "cron too long", field: "cron_expression", change: func(in *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			in.CronExpression = scheduledQuestionPtr(strings.Repeat("* ", 51))
		}},
		{name: "empty accounts", field: "account_ids", change: func(in *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			in.AccountIDs = scheduledQuestionPtr([]int64{})
		}},
		{name: "negative account", field: "account_ids", change: func(in *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			in.AccountIDs = scheduledQuestionPtr([]int64{-1})
		}},
		{name: "too many accounts", field: "account_ids", change: func(in *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			in.AccountIDs = scheduledQuestionPtr(ids(201))
		}},
		{name: "empty questions", field: "question_ids", change: func(in *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			in.QuestionIDs = scheduledQuestionPtr([]int64{})
		}},
		{name: "zero question", field: "question_ids", change: func(in *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			in.QuestionIDs = scheduledQuestionPtr([]int64{0})
		}},
		{name: "too many questions", field: "question_ids", change: func(in *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			in.QuestionIDs = scheduledQuestionPtr(ids(21))
		}},
		{name: "missing account", field: "account_ids", change: func(_ *ScheduledQuestionTestPlanInput, repo *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			delete(repo.accounts, 2)
		}},
		{name: "unavailable account", field: "account_ids", change: func(_ *ScheduledQuestionTestPlanInput, repo *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			repo.err = errors.New("lookup failed")
		}},
		{name: "cross platform", field: "account_ids", change: func(_ *ScheduledQuestionTestPlanInput, repo *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
			repo.accounts[2].Platform = PlatformOpenAI
		}},
		{name: "missing question", field: "question_ids", change: func(_ *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, repo *scheduledQuestionQuestionRepoStub) {
			delete(repo.questions, 2)
		}},
		{name: "disabled question", field: "question_ids", change: func(_ *ScheduledQuestionTestPlanInput, _ *scheduledQuestionAccountRepoStub, repo *scheduledQuestionQuestionRepoStub) {
			repo.questions[2].Enabled = false
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, update := range []bool{false, true} {
				svc, repo, accounts, questions := newScheduledQuestionFixture()
				repo.plans[1] = scheduledQuestionValidPlan(1)
				input := scheduledQuestionValidInput()
				tc.change(&input, accounts, questions)
				var err error
				if update {
					_, err = svc.Update(context.Background(), 1, input)
				} else {
					_, err = svc.Create(context.Background(), input)
				}
				require.Error(t, err, "update=%v", update)
				var validation *model.ValidationError
				require.ErrorAs(t, err, &validation)
				require.Equal(t, tc.field, validation.Field)
				require.Zero(t, repo.created)
				require.Zero(t, repo.updated)
			}
		})
	}
	for _, field := range []string{"name", "account_ids", "question_ids", "cron_expression"} {
		t.Run("missing "+field, func(t *testing.T) {
			svc, _, _, _ := newScheduledQuestionFixture()
			input := scheduledQuestionValidInput()
			switch field {
			case "name":
				input.Name = nil
			case "account_ids":
				input.AccountIDs = nil
			case "question_ids":
				input.QuestionIDs = nil
			case "cron_expression":
				input.CronExpression = nil
			}
			_, err := svc.Create(context.Background(), input)
			var validation *model.ValidationError
			require.ErrorAs(t, err, &validation)
			require.Equal(t, field, validation.Field)
		})
	}
	// Inclusive limits are accepted, and duplicate IDs do not consume capacity.
	svc, _, _, _ := newScheduledQuestionFixture()
	input := scheduledQuestionValidInput()
	input.Name = scheduledQuestionPtr(strings.Repeat("界", 100))
	input.ModelID = scheduledQuestionPtr(strings.Repeat("m", 100))
	input.MinPassRatio, input.CooldownMinutes = scheduledQuestionPtr(100), scheduledQuestionPtr(10080)
	input.AccountIDs = scheduledQuestionPtr(append(ids(200), ids(200)...))
	input.QuestionIDs = scheduledQuestionPtr(append(ids(20), ids(20)...))
	plan, err := svc.Create(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, plan.AccountIDs, 200)
	require.Len(t, plan.QuestionIDs, 20)
	require.Equal(t, 100, plan.MinPassRatio)
	require.Equal(t, 10080, plan.CooldownMinutes)
}

func TestScheduledQuestionCRUDDelegationAndErrors(t *testing.T) {
	svc, repo, _, questions := newScheduledQuestionFixture()
	plan := scheduledQuestionValidPlan(4)
	repo.plans[4] = plan
	repo.due = []*ScheduledQuestionTestPlan{plan}
	plans, err := svc.List(context.Background())
	require.NoError(t, err)
	require.Equal(t, repo.due, plans)
	got, err := svc.GetByID(context.Background(), 4)
	require.NoError(t, err)
	require.Equal(t, plan, got)
	require.NotSame(t, plan, got)
	require.False(t, got.Running)
	_, err = svc.GetByID(context.Background(), 5)
	require.ErrorIs(t, err, ErrScheduledQuestionTestNotFound)
	_, err = svc.Update(context.Background(), 5, ScheduledQuestionTestPlanInput{})
	require.ErrorIs(t, err, ErrScheduledQuestionTestNotFound)
	repo.getErr = errors.New("plan lookup unavailable")
	_, err = svc.GetByID(context.Background(), 4)
	require.ErrorIs(t, err, repo.getErr)
	repo.getErr = nil
	questions.err = errors.New("question lookup unavailable")
	_, err = svc.Create(context.Background(), scheduledQuestionValidInput())
	require.ErrorIs(t, err, questions.err)
	for _, tc := range []struct{ requested, expected int }{{0, 50}, {-1, 50}, {1, 1}, {50, 50}, {201, 200}} {
		_, err = svc.ListRuns(context.Background(), 4, tc.requested)
		require.NoError(t, err)
		require.Equal(t, tc.expected, repo.runLimit)
	}
	_, err = svc.ListRuns(context.Background(), 5, 50)
	require.ErrorIs(t, err, ErrScheduledQuestionTestNotFound)
	require.NoError(t, svc.Delete(context.Background(), 4))
	require.Equal(t, int64(4), repo.deleted)
}

func TestScheduledQuestionExecutionReusesBatchAndPersists(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	plan := scheduledQuestionValidPlan(1)
	plan.AccountIDs, plan.QuestionIDs, plan.ModelID = []int64{7, 2, 9}, []int64{1, 2, 3}, "custom-model"
	plan.UpdatedAt = svc.now().Add(-time.Hour)
	repo.plans[1] = plan
	prepared := &AccountBatchTestPlan{concurrency: 3}
	var executionCtx context.Context
	var prepareCalls, runCalls int
	svc.batch = &scheduledQuestionBatchStub{
		prepare: func(ctx context.Context, input AccountBatchTestInput) (*AccountBatchTestPlan, error) {
			prepareCalls++
			executionCtx = ctx
			require.Equal(t, plan.AccountIDs, input.AccountIDs)
			require.Equal(t, plan.QuestionIDs, input.QuestionIDs)
			require.Equal(t, plan.ModelID, input.ModelID)
			require.Nil(t, input.Concurrency, "the batch default remains three")
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.Positive(t, time.Until(deadline))
			require.LessOrEqual(t, time.Until(deadline), 30*time.Minute)
			return prepared, nil
		},
		run: func(ctx context.Context, actual *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
			runCalls++
			require.Same(t, prepared, actual)
			require.Same(t, executionCtx, ctx)
			require.NoError(t, emit(AccountBatchTestEvent{Type: "batch_start", Data: AccountBatchTestStart{}}))
			// Upstream workers finish out of order; summaries retain plan order.
			for _, id := range []int64{9, 2, 7} {
				for _, question := range plan.QuestionIDs {
					item := AccountBatchTestItemResult{AccountID: id, AccountName: fmt.Sprintf("account-%d", id), Platform: PlatformAnthropic, QuestionID: question}
					switch id {
					case 9:
						item.Status, item.Judged = "passed", true
					case 2:
						item.Status, item.Judged, item.Error = "error", true, "account was deleted"
					case 7:
						item.Status = "unjudged"
					}
					require.NoError(t, emit(AccountBatchTestEvent{Type: "item_result", Data: item}))
				}
			}
			return emit(AccountBatchTestEvent{Type: "batch_complete", Data: AccountBatchTestSummary{}})
		},
	}
	run, err := svc.run(context.Background(), 1, false)
	require.NoError(t, err)
	require.Equal(t, 1, prepareCalls)
	require.Equal(t, 1, runCalls)
	require.Equal(t, "success", run.Status, "account errors do not make a successfully executed round fail")
	require.Equal(t, plan.CronExpression, run.CronExpression)
	require.True(t, run.PlanUpdatedAt.Equal(plan.UpdatedAt))
	require.Equal(t, 3, run.TotalAccounts)
	require.Equal(t, 1, run.PassedAccounts, "ratio=null does not count as a passed account")
	require.Equal(t, 1, run.AlertedAccounts)
	require.Equal(t, []int64{7, 2, 9}, []int64{run.Summary[0].AccountID, run.Summary[1].AccountID, run.Summary[2].AccountID})
	require.Nil(t, run.Summary[0].Ratio)
	require.Equal(t, "ok", run.Summary[0].StateAfter)
	require.Equal(t, "alert", run.Summary[1].StateAfter)
	require.Contains(t, run.Summary[1].Error, "deleted")
	require.Equal(t, 0, *run.Summary[1].Ratio)
	require.Equal(t, 100, *run.Summary[2].Ratio)
	require.Len(t, repo.finished, 1)
	require.Len(t, repo.finishState, 3)
	require.True(t, repo.nextRun.Equal(svc.now().Add(5*time.Minute)))
	require.True(t, run.StartedAt.Equal(svc.now()))
	require.True(t, run.FinishedAt.Equal(svc.now()))
	require.ErrorIs(t, executionCtx.Err(), context.Canceled, "the round deadline context is released after completion")
	require.Empty(t, svc.running)
}

func TestScheduledQuestionExecutionFailuresAndLockCleanup(t *testing.T) {
	for _, tc := range []struct {
		name       string
		prepareErr error
		runErr     error
		badEvent   bool
	}{
		{name: "prepare failure", prepareErr: errors.New("test question is disabled")},
		{name: "run failure", runErr: errors.New("batch execution failed")},
		{name: "invalid event data", badEvent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, _ := newScheduledQuestionFixture()
			repo.plans[1] = scheduledQuestionValidPlan(1)
			var ran bool
			svc.batch = &scheduledQuestionBatchStub{
				prepare: func(context.Context, AccountBatchTestInput) (*AccountBatchTestPlan, error) {
					return &AccountBatchTestPlan{}, tc.prepareErr
				},
				run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
					ran = true
					if tc.badEvent {
						return emit(AccountBatchTestEvent{Type: "item_result", Data: "invalid data"})
					}
					return tc.runErr
				},
			}
			run, err := svc.run(context.Background(), 1, false)
			require.NoError(t, err, "an executed failure is returned as a persisted failed run")
			require.Equal(t, "failed", run.Status)
			require.NotEmpty(t, run.ErrorMessage)
			require.Equal(t, tc.prepareErr == nil, ran)
			require.Len(t, repo.finished, 1)
			require.Equal(t, 2, run.AlertedAccounts)
			require.Zero(t, run.PassedAccounts)
			for _, summary := range run.Summary {
				require.Equal(t, 0, summary.Judged)
				require.Equal(t, 0, *summary.Ratio)
				require.True(t, summary.Alerting)
				require.Contains(t, summary.Error, run.ErrorMessage)
			}
			require.Empty(t, svc.running)
			_, err = svc.run(context.Background(), 1, false)
			require.NoError(t, err, "failed rounds must release the per-plan lock")
		})
	}
	for _, tc := range []struct {
		name      string
		missing   bool
		stateErr  bool
		finishErr bool
		persisted bool
	}{
		{name: "missing plan", missing: true},
		{name: "state lookup fails", stateErr: true, persisted: true}, {name: "persist fails", finishErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, _ := newScheduledQuestionFixture()
			if !tc.missing {
				repo.plans[1] = scheduledQuestionValidPlan(1)
			}
			if tc.stateErr {
				repo.stateErr = errors.New("state lookup failed")
			}
			if tc.finishErr {
				repo.finishErr = errors.New("persist failed")
			}
			run, err := svc.run(context.Background(), 1, false)
			if tc.persisted {
				require.NoError(t, err)
				require.NotNil(t, run)
				require.Equal(t, "failed", run.Status)
				require.Contains(t, run.ErrorMessage, "failed to update alert states")
				require.Len(t, repo.finished, 1)
			} else {
				require.Error(t, err)
			}
			require.Empty(t, svc.running)
		})
	}
}

func TestScheduledQuestionScheduledCancellationPersistsWithoutEmail(t *testing.T) {
	for _, mode := range []string{"cancelled", "expired"} {
		t.Run(mode, func(t *testing.T) {
			svc, repo, _, _ := newScheduledQuestionFixture()
			plan := scheduledQuestionValidPlan(1)
			plan.NotifyEnabled = true
			repo.plans[1] = plan
			mail := &scheduledQuestionEmailStub{}
			svc.email = mail
			svc.settings = &scheduledQuestionSettingsStub{values: map[string]string{SettingKeyAccountQuotaNotifyEmails: `[{"email":"admin@example.com","verified":true}]`}}
			var parent context.Context
			var cancel context.CancelFunc
			if mode == "cancelled" {
				parent, cancel = context.WithCancel(context.Background())
			} else {
				parent, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			}
			defer cancel()
			svc.batch = &scheduledQuestionBatchStub{prepare: func(ctx context.Context, _ AccountBatchTestInput) (*AccountBatchTestPlan, error) {
				if mode == "cancelled" {
					cancel()
				}
				<-ctx.Done()
				return nil, ctx.Err()
			}}
			run, err := svc.runScheduledPlan(parent, 1)
			require.NoError(t, err, "cancelled rounds persist with a bounded detached context")
			require.Equal(t, "failed", run.Status)
			require.True(t, run.NotificationsSuppressed)
			require.True(t, strings.Contains(run.ErrorMessage, context.Canceled.Error()) || strings.Contains(run.ErrorMessage, context.DeadlineExceeded.Error()))
			require.Len(t, repo.finished, 1)
			require.Equal(t, 2, run.AlertedAccounts)
			require.Empty(t, mail.mails, "request cancellation must not cause detached notifications")
			for _, state := range repo.finishState {
				require.Nil(t, state.LastAlertAt)
			}
			require.Empty(t, svc.running)
		})
	}
}

func TestScheduledQuestionExecutionConcurrentPlanLock(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	repo.plans[1], repo.plans[2] = scheduledQuestionValidPlan(1), scheduledQuestionValidPlan(2)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	svc.batch = &scheduledQuestionBatchStub{run: func(ctx context.Context, _ *AccountBatchTestPlan, _ func(AccountBatchTestEvent) error) error {
		started <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 2)
	go func() { _, err := svc.run(ctx, 1, false); done <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first plan did not start")
	}
	_, err := svc.run(context.Background(), 1, false)
	require.ErrorIs(t, err, ErrScheduledQuestionTestRunning)
	go func() { _, err := svc.run(ctx, 2, false); done <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("a different plan should not share the per-plan lock")
	}
	cancel()
	for range 2 {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("plan execution did not stop")
		}
	}
	require.Empty(t, svc.running)
}

func TestScheduledQuestionRunnerDueScan(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	for id := int64(1); id <= 6; id++ {
		plan := scheduledQuestionValidPlan(id)
		if id == 6 {
			plan.Enabled = false
		}
		repo.plans[id] = plan
		repo.due = append(repo.due, plan)
	}
	started := make(chan struct{}, 6)
	var active, peak, executions atomic.Int32
	svc.batch = &scheduledQuestionBatchStub{run: func(ctx context.Context, _ *AccountBatchTestPlan, _ func(AccountBatchTestEvent) error) error {
		executions.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}}
	runner := NewScheduledQuestionTestRunnerService(repo, svc, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { runner.runScheduled(ctx); close(done) }()
	for range 3 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("due plan did not start")
		}
	}
	require.EqualValues(t, 3, peak.Load())
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not stop dispatching cancelled plans")
	}
	require.Equal(t, 1, repo.dueCalls)
	require.EqualValues(t, 3, executions.Load())
	require.Zero(t, active.Load())
	require.Empty(t, svc.running)
	// The runner trusts ListDue's NULL/past-next-run selection and does not skip
	// a restart catch-up plan just because its NextRunAt is nil.
	svc.batch = &scheduledQuestionBatchStub{}
	runner.runScheduled(context.Background())
	require.Len(t, repo.finished, 8, "three cancelled runs plus all five enabled due plans")
	before := repo.dueCalls
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	runner.runScheduled(ctx)
	require.Equal(t, before, repo.dueCalls)
	repo.dueErr = errors.New("due query unavailable")
	runner.runScheduled(context.Background())
	require.Len(t, repo.finished, 8)
}

func TestScheduledQuestionRunnerLifecycle(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	runner := NewScheduledQuestionTestRunnerService(repo, svc, nil)
	runner.Start()
	defer runner.Stop()
	runner.Start()
	require.NotNil(t, runner.cron)
	require.Len(t, runner.cron.Entries(), 1)
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	require.Equal(t, time.Minute, runner.cron.Entries()[0].Schedule.Next(now).Sub(now))
	runner.Stop()
	runner.Stop()
	for _, tz := range []string{"Asia/Singapore", "invalid-timezone"} {
		configuredService, configuredRepo, _, _ := newScheduledQuestionFixture()
		configured := NewScheduledQuestionTestRunnerService(configuredRepo, configuredService, &config.Config{Timezone: tz})
		configured.Start()
		defer configured.Stop()
		want := tz
		if tz == "invalid-timezone" {
			want = time.Local.String()
		}
		require.Equal(t, want, configured.cron.Location().String())
	}
	var nilRunner *ScheduledQuestionTestRunnerService
	nilRunner.Start()
	nilRunner.Stop()
}

func TestScheduledQuestionRunnerReloadsDisabledPlanAndSkipsScanDisabled(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	scanned := scheduledQuestionValidPlan(1)
	current := *scanned
	current.Enabled = false
	repo.plans[1] = &current
	repo.due = []*ScheduledQuestionTestPlan{scanned, scheduledQuestionValidPlan(2)}
	repo.due[1].Enabled = false
	var calls atomic.Int32
	svc.batch = &scheduledQuestionBatchStub{prepare: func(context.Context, AccountBatchTestInput) (*AccountBatchTestPlan, error) {
		calls.Add(1)
		return &AccountBatchTestPlan{}, nil
	}}
	runner := NewScheduledQuestionTestRunnerService(repo, svc, nil)
	runner.runScheduled(context.Background())
	require.Equal(t, 1, repo.dueCalls)
	require.Zero(t, calls.Load(), "scheduled execution must re-read the plan after the due scan")
	require.Empty(t, repo.finished)
	require.Empty(t, svc.running)
}

func TestScheduledQuestionExecutionRealBatchSyntheticAndDeletedAccount(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	plan := scheduledQuestionValidPlan(1)
	plan.AccountIDs, plan.QuestionIDs = []int64{99, 7}, []int64{1, 2, 3}
	repo.plans[1] = plan
	account := batchSyntheticAccount(7)
	account.Status = StatusError
	accounts := &batchTestAccountRepo{accounts: map[int64]*Account{7: account}}
	questions := &batchTestQuestionRepo{questions: map[int64]*model.AccountTestQuestion{
		1: batchEnabledQuestion(1, "(?i)anthropic"), 2: batchEnabledQuestion(2, "^never-matches$"), 3: batchEnabledQuestion(3),
	}}
	svc.batch = newBatchTestService(accounts, questions, nil, true)
	run, err := svc.run(context.Background(), 1, false)
	require.NoError(t, err)
	require.Equal(t, "success", run.Status, "deleted accounts are item failures, not a Prepare failure")
	require.Len(t, run.Summary, 2)
	require.Equal(t, int64(99), run.Summary[0].AccountID)
	require.Equal(t, "Account not found", run.Summary[0].Error)
	require.Equal(t, 3, run.Summary[0].Total)
	require.Zero(t, run.Summary[0].Judged)
	require.Equal(t, 0, *run.Summary[0].Ratio)
	require.True(t, run.Summary[0].Alerting)
	require.Equal(t, int64(7), run.Summary[1].AccountID)
	require.Equal(t, 3, run.Summary[1].Total)
	require.Equal(t, 2, run.Summary[1].Judged)
	require.Equal(t, 1, run.Summary[1].Passed)
	require.Equal(t, 50, *run.Summary[1].Ratio)
	require.True(t, run.Summary[1].Alerting)
	require.Equal(t, []int64{7}, accounts.clearedErrors, "the real batch recovers each connected account once even when an answer fails")
	require.Len(t, repo.finishState, 2)
	require.Empty(t, svc.running)
}

func TestScheduledQuestionAlertStateNotificationsAndTimestamp(t *testing.T) {
	for _, tc := range []struct {
		name             string
		previousState    string
		lastAgo          *time.Duration
		alerting         bool
		cooldown         int
		notifyDisabled   bool
		planDisabled     bool
		currentOff       bool
		currentNotifyOff bool
		noRecipients     bool
		smtpFailure      bool
		mailCount        int
		newTimestamp     bool
		recovered        bool
	}{
		{name: "new alert sent", alerting: true, cooldown: 60, mailCount: 1, newTimestamp: true},
		{name: "ok to alert sent despite recent timestamp", previousState: "ok", lastAgo: scheduledQuestionPtr(time.Minute), alerting: true, cooldown: 60, mailCount: 1, newTimestamp: true},
		{name: "inside cooldown preserves timestamp", previousState: "alert", lastAgo: scheduledQuestionPtr(30 * time.Minute), alerting: true, cooldown: 60},
		{name: "exact cooldown resends", previousState: "alert", lastAgo: scheduledQuestionPtr(time.Hour), alerting: true, cooldown: 60, mailCount: 1, newTimestamp: true},
		{name: "zero cooldown resends", previousState: "alert", lastAgo: scheduledQuestionPtr(time.Duration(0)), alerting: true, mailCount: 1, newTimestamp: true},
		{name: "future timestamp suppressed", previousState: "alert", lastAgo: scheduledQuestionPtr(-time.Hour), alerting: true, cooldown: 60},
		{name: "notify off no timestamp", alerting: true, notifyDisabled: true},
		{name: "notify off preserves timestamp", previousState: "alert", lastAgo: scheduledQuestionPtr(2 * time.Hour), alerting: true, cooldown: 60, notifyDisabled: true},
		{name: "disabled plan no timestamp", alerting: true, planDisabled: true},
		{name: "disabled during round no mail", alerting: true, currentOff: true},
		{name: "notification disabled during round no mail", alerting: true, currentNotifyOff: true},
		{name: "no verified recipients no timestamp", alerting: true, noRecipients: true},
		{name: "SMTP failure no timestamp", alerting: true, smtpFailure: true, mailCount: 1},
		{name: "SMTP failure preserves old timestamp", previousState: "alert", lastAgo: scheduledQuestionPtr(2 * time.Hour), alerting: true, cooldown: 60, smtpFailure: true, mailCount: 1},
		{name: "recovery immediate", previousState: "alert", lastAgo: scheduledQuestionPtr(time.Minute), cooldown: 60, mailCount: 1, recovered: true},
		{name: "recovery with notify off clears timestamp", previousState: "alert", lastAgo: scheduledQuestionPtr(time.Minute), cooldown: 60, notifyDisabled: true, recovered: true},
		{name: "healthy stays healthy", previousState: "ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, _ := newScheduledQuestionFixture()
			plan := scheduledQuestionValidPlan(9)
			plan.Enabled, plan.NotifyEnabled, plan.CooldownMinutes = !tc.planDisabled, !tc.notifyDisabled, tc.cooldown
			current := *plan
			current.Enabled, current.NotifyEnabled = plan.Enabled && !tc.currentOff, plan.NotifyEnabled && !tc.currentNotifyOff
			repo.plans[9] = &current
			var oldTime *time.Time
			if tc.previousState != "" {
				if tc.lastAgo != nil {
					oldTime = scheduledQuestionPtr(svc.now().Add(-*tc.lastAgo))
				}
				repo.states = []*ScheduledQuestionTestAccountState{{PlanID: 9, AccountID: 2, State: tc.previousState, LastAlertAt: oldTime}}
			}
			raw := `[{"email":"admin@example.com","verified":true}]`
			if tc.noRecipients {
				raw = `[{"email":"admin@example.com","verified":false}]`
			}
			svc.settings = &scheduledQuestionSettingsStub{values: map[string]string{SettingKeyAccountQuotaNotifyEmails: raw}}
			mail := &scheduledQuestionEmailStub{errors: make(map[string]error)}
			if tc.smtpFailure {
				mail.errors["admin@example.com"] = errors.New("smtp unavailable")
			}
			svc.email = mail
			run := &ScheduledQuestionTestRun{StartedAt: svc.now(), Summary: []ScheduledQuestionTestAccountSummary{{AccountID: 2, Alerting: tc.alerting, Ratio: scheduledQuestionPtr(0)}}}
			states, err := svc.updateAlertStates(context.Background(), plan, run)
			require.NoError(t, err)
			require.Equal(t, tc.planDisabled || tc.notifyDisabled || tc.currentOff || tc.currentNotifyOff, run.NotificationsSuppressed)
			require.Len(t, states, 1)
			require.Equal(t, int64(9), states[0].PlanID)
			require.Equal(t, int64(2), states[0].AccountID)
			require.Len(t, mail.mails, tc.mailCount)
			if tc.alerting {
				require.Equal(t, "alert", states[0].State)
				if tc.newTimestamp {
					require.NotNil(t, states[0].LastAlertAt)
					require.True(t, states[0].LastAlertAt.Equal(svc.now()))
				} else {
					require.Equal(t, oldTime, states[0].LastAlertAt)
				}
			} else {
				require.Equal(t, "ok", states[0].State)
				require.Nil(t, states[0].LastAlertAt)
			}
			if tc.recovered && tc.mailCount > 0 {
				require.Contains(t, mail.mails[0].subject, "recovered")
			}
		})
	}
}

func TestScheduledQuestionExecutionDisabledOrNotificationOffExecutesSilently(t *testing.T) {
	for _, tc := range []struct {
		name          string
		enabled       bool
		notifyEnabled bool
		changedDuring string
	}{
		{name: "disabled manual run", notifyEnabled: true},
		{name: "notifications disabled", enabled: true},
		{name: "both disabled"},
		{name: "disabled during execution", enabled: true, notifyEnabled: true, changedDuring: "enabled"},
		{name: "notify disabled during execution", enabled: true, notifyEnabled: true, changedDuring: "notify_enabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, _ := newScheduledQuestionFixture()
			plan := scheduledQuestionValidPlan(1)
			plan.Enabled, plan.NotifyEnabled = tc.enabled, tc.notifyEnabled
			repo.plans[1] = plan
			svc.settings = &scheduledQuestionSettingsStub{values: map[string]string{SettingKeyAccountQuotaNotifyEmails: `[{"email":"admin@example.com","verified":true}]`}}
			mail := &scheduledQuestionEmailStub{}
			svc.email = mail
			var called bool
			svc.batch = &scheduledQuestionBatchStub{run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
				called = true
				if tc.changedDuring != "" {
					current := *plan
					if tc.changedDuring == "enabled" {
						current.Enabled = false
					} else {
						current.NotifyEnabled = false
					}
					repo.mu.Lock()
					repo.plans[1] = &current
					repo.mu.Unlock()
				}
				for _, id := range plan.AccountIDs {
					for _, questionID := range plan.QuestionIDs {
						if err := emit(AccountBatchTestEvent{Type: "item_result", Data: AccountBatchTestItemResult{
							AccountID: id, QuestionID: questionID, Status: "failed", Judged: true,
						}}); err != nil {
							return err
						}
					}
				}
				return nil
			}}
			run, err := svc.run(context.Background(), 1, false)
			require.NoError(t, err)
			require.True(t, called)
			require.Equal(t, "success", run.Status)
			require.True(t, run.NotificationsSuppressed)
			require.Equal(t, 2, run.AlertedAccounts)
			require.Len(t, repo.finished, 1)
			require.Len(t, repo.finishState, 2)
			require.Empty(t, mail.mails)
			for _, state := range repo.finishState {
				require.Equal(t, "alert", state.State)
				require.Nil(t, state.LastAlertAt)
			}
			payload, err := json.Marshal(run)
			require.NoError(t, err)
			require.Contains(t, string(payload), `"notifications_suppressed":true`)
			require.Empty(t, svc.running)
		})
	}
	// Scheduled execution retains the enable guard even though the manual entry
	// point deliberately permits disabled plans.
	svc, repo, _, _ := newScheduledQuestionFixture()
	repo.plans[1] = scheduledQuestionValidPlan(1)
	repo.plans[1].Enabled = false
	_, err := svc.runScheduledPlan(context.Background(), 1)
	require.Error(t, err)
	require.Empty(t, repo.finished)
	require.Empty(t, svc.running)
}

func TestScheduledQuestionNotificationAttemptAndSentLogs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    string
		smtpErr error
	}{
		{name: "alert success", kind: "alert"},
		{name: "alert failure", kind: "alert", smtpErr: errors.New("SMTP unavailable")},
		{name: "recovery success", kind: "recovered"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, _ := newScheduledQuestionFixture()
			svc.settings = &scheduledQuestionSettingsStub{values: map[string]string{SettingKeyAccountQuotaNotifyEmails: `[{"email":"admin@example.com","verified":true}]`}}
			svc.email = &scheduledQuestionEmailStub{errors: map[string]error{"admin@example.com": tc.smtpErr}}
			plan := scheduledQuestionValidPlan(17)
			run := &ScheduledQuestionTestRun{StartedAt: svc.now(), Summary: []ScheduledQuestionTestAccountSummary{
				{AccountID: 7, AccountName: "account-7"}, {AccountID: 2, AccountName: "account-2"},
			}}
			var logs bytes.Buffer
			previousLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(previousLogger)
			require.Equal(t, tc.smtpErr == nil, svc.sendScheduledQuestionNotification(context.Background(), plan, run, tc.kind, []int{0, 1}))
			attempts, completed := map[int64]bool{}, map[int64]bool{}
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				var event struct {
					Message   string `json:"msg"`
					PlanID    int64  `json:"plan_id"`
					AccountID int64  `json:"account_id"`
					Kind      string `json:"kind"`
					Success   *bool  `json:"success"`
				}
				require.NoError(t, json.Unmarshal([]byte(line), &event))
				if event.Message != "scheduled_question_test_notification_attempt" && event.Message != "scheduled_question_test_notification_sent" {
					continue
				}
				require.Equal(t, int64(17), event.PlanID)
				require.Equal(t, tc.kind, event.Kind)
				require.Contains(t, []int64{7, 2}, event.AccountID)
				if event.Message == "scheduled_question_test_notification_attempt" {
					attempts[event.AccountID] = true
				} else {
					completed[event.AccountID] = true
					require.NotNil(t, event.Success)
					require.Equal(t, tc.smtpErr == nil, *event.Success)
				}
			}
			require.Len(t, attempts, 2)
			require.Len(t, completed, 2)
		})
	}
}

func TestScheduledQuestionScheduledRunRechecksNextRunAt(t *testing.T) {
	for _, tc := range []struct {
		name string
		next *time.Duration
		runs bool
	}{
		{name: "restart null next run", runs: true},
		{name: "past next run", next: scheduledQuestionPtr(-time.Minute), runs: true},
		{name: "exact due", next: scheduledQuestionPtr(time.Duration(0)), runs: true},
		{name: "manual run already advanced schedule", next: scheduledQuestionPtr(time.Minute)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, _ := newScheduledQuestionFixture()
			plan := scheduledQuestionValidPlan(1)
			if tc.next != nil {
				plan.NextRunAt = scheduledQuestionPtr(svc.now().Add(*tc.next))
			}
			repo.plans[1] = plan
			var calls int
			svc.batch = &scheduledQuestionBatchStub{prepare: func(context.Context, AccountBatchTestInput) (*AccountBatchTestPlan, error) {
				calls++
				return &AccountBatchTestPlan{}, nil
			}}
			run, err := svc.runScheduledPlan(context.Background(), 1)
			require.NoError(t, err)
			if tc.runs {
				require.NotNil(t, run)
				require.Equal(t, 1, calls)
				require.Len(t, repo.finished, 1)
			} else {
				require.Nil(t, run)
				require.Zero(t, calls)
				require.Empty(t, repo.finished)
			}
			require.Empty(t, svc.running)
			calls = 0
			_, err = svc.run(context.Background(), 1, false)
			require.NoError(t, err)
			require.Equal(t, 1, calls, "manual runs deliberately ignore next_run_at")
		})
	}
}

func waitScheduledQuestionSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal(message)
	}
}

func waitScheduledQuestionExecutions(t *testing.T, svc *ScheduledQuestionTestService) {
	t.Helper()
	done := make(chan struct{})
	go func() { svc.active.Wait(); close(done) }()
	waitScheduledQuestionSignal(t, done, "scheduled question execution did not finish")
}

func TestScheduledQuestionRunNowAcceptsBeforeUpstreamCompletesAndIgnoresRequestCancellation(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	repo.plans[1], repo.due = plan, []*ScheduledQuestionTestPlan{plan}
	before, err := svc.GetByID(context.Background(), 1)
	require.NoError(t, err)
	require.False(t, before.Running)
	listedBefore, err := svc.List(context.Background())
	require.NoError(t, err)
	require.False(t, listedBefore[0].Running)
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var upstreamCtx context.Context
	svc.batch = &scheduledQuestionBatchStub{run: func(ctx context.Context, _ *AccountBatchTestPlan, _ func(AccountBatchTestEvent) error) error {
		upstreamCtx = ctx
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	type acceptedResult struct {
		accepted *ScheduledQuestionTestAccepted
		err      error
	}
	response := make(chan acceptedResult, 1)
	go func() {
		accepted, err := svc.RunNow(requestCtx, 1)
		response <- acceptedResult{accepted, err}
	}()
	select {
	case result := <-response:
		require.NoError(t, result.err)
		require.Equal(t, &ScheduledQuestionTestAccepted{Accepted: true, PlanID: 1}, result.accepted)
		payload, err := json.Marshal(result.accepted)
		require.NoError(t, err)
		require.JSONEq(t, `{"accepted":true,"plan_id":1}`, string(payload))
	case <-time.After(5 * time.Second):
		t.Fatal("run-now waited for a deliberately blocked upstream instead of returning accepted")
	}
	waitScheduledQuestionSignal(t, started, "background batch did not start")
	deadline, ok := upstreamCtx.Deadline()
	require.True(t, ok)
	require.Positive(t, time.Until(deadline))
	require.LessOrEqual(t, time.Until(deadline), 30*time.Minute)
	cancelRequest()
	require.NoError(t, upstreamCtx.Err(), "the background round belongs to the service, not the HTTP request")
	during, err := svc.GetByID(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, during.Running)
	listedDuring, err := svc.List(context.Background())
	require.NoError(t, err)
	require.True(t, listedDuring[0].Running)
	require.False(t, before.Running, "an earlier detail snapshot must not be mutated")
	require.False(t, listedBefore[0].Running, "an earlier list snapshot must not be mutated")
	require.False(t, plan.Running, "running is a service projection, not persisted plan state")
	require.NotSame(t, plan, during)
	repo.mu.Lock()
	finishedWhileBlocked := len(repo.finished)
	repo.mu.Unlock()
	require.Zero(t, finishedWhileBlocked)
	_, err = svc.RunNow(context.Background(), 1)
	require.ErrorIs(t, err, ErrScheduledQuestionTestRunning)
	_, err = svc.runScheduledPlan(context.Background(), 1)
	require.ErrorIs(t, err, ErrScheduledQuestionTestRunning, "scheduled and manual execution share the same plan lock")
	releaseOnce.Do(func() { close(release) })
	waitScheduledQuestionExecutions(t, svc)
	after, err := svc.GetByID(context.Background(), 1)
	require.NoError(t, err)
	require.False(t, after.Running)
	listedAfter, err := svc.List(context.Background())
	require.NoError(t, err)
	require.False(t, listedAfter[0].Running)
	require.True(t, during.Running, "the in-flight snapshot remains a snapshot")
	require.Len(t, repo.finished, 1)
	require.Equal(t, "success", repo.finished[0].Status)
	require.ErrorIs(t, upstreamCtx.Err(), context.Canceled, "the round timeout is released when the background task exits")
}

func TestScheduledQuestionRunNowConflictsWithScheduledExecution(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	repo.plans[1] = scheduledQuestionValidPlan(1)
	started := make(chan struct{})
	svc.batch = &scheduledQuestionBatchStub{run: func(ctx context.Context, _ *AccountBatchTestPlan, _ func(AccountBatchTestEvent) error) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := svc.runScheduledPlan(ctx, 1); done <- err }()
	waitScheduledQuestionSignal(t, started, "scheduled execution did not start")
	accepted, err := svc.RunNow(context.Background(), 1)
	require.Nil(t, accepted)
	require.ErrorIs(t, err, ErrScheduledQuestionTestRunning)
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled execution did not finish after scan cancellation")
	}
	require.Len(t, repo.finished, 1)
	require.Equal(t, "failed", repo.finished[0].Status)
	require.Empty(t, svc.running)
}

func TestScheduledQuestionRunNowStopCancelsAndJoinsPersistence(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	repo.plans[1] = scheduledQuestionValidPlan(1)
	repo.plans[1].NotifyEnabled = true
	mail := &scheduledQuestionEmailStub{}
	svc.email = mail
	svc.settings = &scheduledQuestionSettingsStub{values: map[string]string{SettingKeyAccountQuotaNotifyEmails: `[{"email":"admin@example.com","verified":true}]`}}
	started, cancelling, persisting := make(chan struct{}), make(chan struct{}), make(chan struct{})
	allowPersistence := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(allowPersistence) })
	svc.batch = &scheduledQuestionBatchStub{run: func(ctx context.Context, _ *AccountBatchTestPlan, _ func(AccountBatchTestEvent) error) error {
		close(started)
		<-ctx.Done()
		close(cancelling)
		return ctx.Err()
	}}
	var finishCtx context.Context
	repo.beforeFinish = func(ctx context.Context, _ *ScheduledQuestionTestRun) {
		finishCtx = ctx
		close(persisting)
		<-allowPersistence
	}
	accepted, err := svc.RunNow(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, accepted.Accepted)
	waitScheduledQuestionSignal(t, started, "manual background execution did not start")
	stopped := make(chan struct{})
	go func() { svc.Stop(); close(stopped) }()
	waitScheduledQuestionSignal(t, cancelling, "service Stop did not cancel the manual execution")
	waitScheduledQuestionSignal(t, persisting, "the cancelled run was not persisted")
	require.NoError(t, finishCtx.Err(), "storage uses its own live context after service cancellation")
	deadline, ok := finishCtx.Deadline()
	require.True(t, ok)
	require.LessOrEqual(t, time.Until(deadline), 30*time.Second)
	select {
	case <-stopped:
		t.Fatal("Stop returned before failed-run persistence and execution lock cleanup")
	default:
	}
	detail, err := svc.GetByID(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, detail.Running)
	_, err = svc.RunNow(context.Background(), 1)
	require.ErrorIs(t, err, ErrScheduledQuestionTestStopped)
	releaseOnce.Do(func() { close(allowPersistence) })
	waitScheduledQuestionSignal(t, stopped, "Stop did not join the completed manual task")
	require.Len(t, repo.finished, 1)
	require.Equal(t, "failed", repo.finished[0].Status)
	require.Contains(t, repo.finished[0].ErrorMessage, context.Canceled.Error())
	require.True(t, repo.finished[0].NotificationsSuppressed)
	require.Empty(t, mail.mails)
	for _, state := range repo.finishState {
		require.Nil(t, state.LastAlertAt)
	}
	detail, err = svc.GetByID(context.Background(), 1)
	require.NoError(t, err)
	require.False(t, detail.Running)
	svc.Stop()
	svc.Start()
	_, err = svc.RunNow(context.Background(), 1)
	require.ErrorIs(t, err, ErrScheduledQuestionTestStopped, "Start must not reopen a stopped service")
}

func TestScheduledQuestionRunNowBackgroundFailuresArePersisted(t *testing.T) {
	for _, stage := range []string{"prepare", "run", "invalid event"} {
		t.Run(stage, func(t *testing.T) {
			svc, repo, _, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			repo.plans[1] = scheduledQuestionValidPlan(1)
			failure := errors.New("background " + stage + " failed")
			svc.batch = &scheduledQuestionBatchStub{
				prepare: func(context.Context, AccountBatchTestInput) (*AccountBatchTestPlan, error) {
					if stage == "prepare" {
						return nil, failure
					}
					return &AccountBatchTestPlan{}, nil
				},
				run: func(_ context.Context, _ *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
					if stage == "invalid event" {
						return emit(AccountBatchTestEvent{Type: "item_result", Data: "invalid"})
					}
					return failure
				},
			}
			accepted, err := svc.RunNow(context.Background(), 1)
			require.NoError(t, err)
			require.True(t, accepted.Accepted)
			waitScheduledQuestionExecutions(t, svc)
			require.Len(t, repo.finished, 1)
			require.Equal(t, "failed", repo.finished[0].Status)
			require.NotEmpty(t, repo.finished[0].ErrorMessage)
			if stage != "invalid event" {
				require.Equal(t, failure.Error(), repo.finished[0].ErrorMessage)
			}
			detail, err := svc.GetByID(context.Background(), 1)
			require.NoError(t, err)
			require.False(t, detail.Running)
		})
	}
}

func TestScheduledQuestionRunNowDisabledAcceptedAndNotificationsSuppressed(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.Enabled, plan.NotifyEnabled = false, true
	repo.plans[1] = plan
	mail := &scheduledQuestionEmailStub{}
	svc.email = mail
	svc.settings = &scheduledQuestionSettingsStub{values: map[string]string{SettingKeyAccountQuotaNotifyEmails: `[{"email":"admin@example.com","verified":true}]`}}
	var calls atomic.Int32
	svc.batch = &scheduledQuestionBatchStub{run: func(_ context.Context, _ *AccountBatchTestPlan, _ func(AccountBatchTestEvent) error) error {
		calls.Add(1)
		return nil
	}}
	accepted, err := svc.RunNow(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, &ScheduledQuestionTestAccepted{Accepted: true, PlanID: 1}, accepted)
	waitScheduledQuestionExecutions(t, svc)
	require.EqualValues(t, 1, calls.Load())
	require.Len(t, repo.finished, 1)
	require.True(t, repo.finished[0].NotificationsSuppressed)
	require.Len(t, repo.finishState, 2)
	require.Empty(t, mail.mails)
	_, err = svc.RunNow(context.Background(), 99)
	require.ErrorIs(t, err, ErrScheduledQuestionTestNotFound)
	require.Empty(t, svc.running, "a failed preflight must release the plan lock and waitgroup")
}
