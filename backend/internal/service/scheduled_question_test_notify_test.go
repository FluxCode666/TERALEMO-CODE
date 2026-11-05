//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type scheduledQuestionFeishuNotificationCall struct {
	webhook, secret string
	notification    scheduledQuestionNotification
}

type scheduledQuestionFeishuNotificationStub struct {
	calls   []scheduledQuestionFeishuNotificationCall
	success bool
	detail  string
}

func (n *scheduledQuestionFeishuNotificationStub) Send(_ context.Context, webhook, secret string, notification scheduledQuestionNotification) (bool, string) {
	n.calls = append(n.calls, scheduledQuestionFeishuNotificationCall{webhook, secret, notification})
	return n.success, n.detail
}

func TestScheduledQuestionNotificationChannels(t *testing.T) {
	for _, tc := range []struct {
		name, kind                   string
		email, feishu, feishuSuccess bool
		emailError                   bool
		channels                     int
		sent                         bool
	}{
		{name: "email", kind: "alert", email: true, channels: 1, sent: true},
		{name: "feishu", kind: "recovered", feishu: true, feishuSuccess: true, channels: 1, sent: true},
		{name: "both", kind: "report", email: true, feishu: true, feishuSuccess: true, channels: 2, sent: true},
		{name: "feishu succeeds after email fails", kind: "alert", email: true, feishu: true, emailError: true, feishuSuccess: true, channels: 2, sent: true},
		{name: "email succeeds despite feishu failure", kind: "alert", email: true, feishu: true, channels: 2, sent: true},
		{name: "both fail", kind: "alert", email: true, feishu: true, emailError: true, channels: 2},
		{name: "channels disabled", kind: "report"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, _ := newScheduledQuestionFixture()
			defer svc.Stop()
			svc.settings = &scheduledQuestionSettingsStub{values: map[string]string{
				SettingKeyAccountQuotaNotifyEmails: `[{"email":"admin@example.com","verified":true}]`,
				SettingKeySiteName:                 "Notification Site",
			}}
			mail := &scheduledQuestionEmailStub{errors: map[string]error{}}
			if tc.emailError {
				mail.errors["admin@example.com"] = errors.New("unsafe upstream SMTP body " + scheduledQuestionTestWebhook)
			}
			feishu := &scheduledQuestionFeishuNotificationStub{success: tc.feishuSuccess, detail: "sent"}
			if !tc.feishuSuccess {
				feishu.detail = "upstream body: " + scheduledQuestionTestWebhook + " token-sensitive-1234 secret-value"
			}
			svc.email, svc.feishu = mail, feishu
			plan := scheduledQuestionValidPlan(1)
			plan.EmailEnabled, plan.FeishuEnabled = tc.email, tc.feishu
			plan.FeishuWebhookURL, plan.FeishuSecret = scheduledQuestionTestWebhook, "secret-value"
			run := &ScheduledQuestionTestRun{Status: "success", StartedAt: svc.now(), Summary: []ScheduledQuestionTestAccountSummary{{AccountID: 1, AccountName: "account"}}}
			require.Equal(t, tc.sent, svc.sendScheduledQuestionNotification(context.Background(), plan, run, tc.kind, []int{0}))
			require.Len(t, run.NotificationResults, tc.channels)
			for _, result := range run.NotificationResults {
				require.Equal(t, tc.kind, result.Kind)
				require.NotContains(t, result.Detail, "token-sensitive-1234")
				require.NotContains(t, result.Detail, "secret-value")
				require.NotContains(t, result.Detail, "upstream")
			}
			if tc.email {
				require.Len(t, mail.mails, 1)
				require.Contains(t, mail.mails[0].subject, "[Notification Site]")
			} else {
				require.Empty(t, mail.mails)
			}
			if tc.feishu {
				require.Len(t, feishu.calls, 1)
				require.Equal(t, scheduledQuestionTestWebhook, feishu.calls[0].webhook)
				require.Equal(t, "secret-value", feishu.calls[0].secret)
			} else {
				require.Empty(t, feishu.calls)
			}
		})
	}
}

func TestScheduledQuestionNotificationEmailResultAndUnavailableChannels(t *testing.T) {
	svc, _, _, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	svc.settings = &scheduledQuestionSettingsStub{values: map[string]string{SettingKeyAccountQuotaNotifyEmails: `[
		{"email":"one@example.com","verified":true},
		{"email":"two@example.com","verified":true},
		{"email":"three@example.com","verified":true},
		{"email":"unverified@example.com"}
	]`}}
	svc.email = &scheduledQuestionEmailStub{errors: map[string]error{"two@example.com": errors.New("SMTP secret")}}
	plan := scheduledQuestionValidPlan(1)
	plan.EmailEnabled = true
	run := &ScheduledQuestionTestRun{StartedAt: svc.now()}
	require.True(t, svc.sendScheduledQuestionNotification(context.Background(), plan, run, "report", nil))
	require.Equal(t, []ScheduledQuestionTestNotificationResult{{Kind: "report", Channel: "email", Success: true, Detail: "sent 2/3 recipients (rejected)"}}, run.NotificationResults)
	for _, tc := range []struct {
		name, detail string
		settings     SettingRepository
		email        scheduledQuestionEmailSender
	}{
		{name: "email unavailable", settings: svc.settings, detail: "email service unavailable"},
		{name: "settings unavailable", email: svc.email, detail: "email service unavailable"},
		{name: "no verified emails", settings: &scheduledQuestionSettingsStub{}, email: svc.email, detail: "no verified recipients"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notifier := &ScheduledQuestionTestService{settings: tc.settings, email: tc.email}
			run := &ScheduledQuestionTestRun{StartedAt: time.Now()}
			require.False(t, notifier.sendScheduledQuestionNotification(context.Background(), plan, run, "report", nil))
			require.Len(t, run.NotificationResults, 1)
			require.Equal(t, tc.detail, run.NotificationResults[0].Detail)
		})
	}
	plan.EmailEnabled, plan.FeishuEnabled, plan.FeishuWebhookURL = false, true, scheduledQuestionTestWebhook
	svc.feishu = nil
	run.NotificationResults = nil
	require.False(t, svc.sendScheduledQuestionNotification(context.Background(), plan, run, "alert", nil))
	require.Equal(t, "feishu service unavailable", run.NotificationResults[0].Detail)
	plan.FeishuWebhookURL = ""
	run.NotificationResults = nil
	require.False(t, svc.sendScheduledQuestionNotification(context.Background(), plan, run, "report", nil))
	require.Equal(t, []ScheduledQuestionTestNotificationResult{{Kind: "report", Channel: "feishu", Detail: "invalid feishu webhook URL"}}, run.NotificationResults)
}

func TestScheduledQuestionNotificationCancellationRecordsEveryEnabledChannel(t *testing.T) {
	feishu := &scheduledQuestionFeishuNotificationStub{success: true, detail: "sent"}
	svc := &ScheduledQuestionTestService{feishu: feishu}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan := &ScheduledQuestionTestPlan{EmailEnabled: true, FeishuEnabled: true, FeishuWebhookURL: scheduledQuestionTestWebhook}
	run := &ScheduledQuestionTestRun{}
	require.False(t, svc.sendScheduledQuestionNotification(ctx, plan, run, "report", nil))
	require.Equal(t, []ScheduledQuestionTestNotificationResult{
		{Kind: "report", Channel: "email", Detail: "aborted"},
		{Kind: "report", Channel: "feishu", Detail: "aborted"},
	}, run.NotificationResults)
	require.Empty(t, feishu.calls)
}

type scheduledQuestionSlowEmailSender struct {
	mu       sync.Mutex
	calls    int
	active   int
	signals  chan struct{}
	finishes chan struct{}
}

func (s *scheduledQuestionSlowEmailSender) SendEmail(ctx context.Context, _, _, _ string) error {
	s.mu.Lock()
	s.calls++
	s.active++
	s.mu.Unlock()
	if s.signals != nil {
		s.signals <- struct{}{}
	}
	<-ctx.Done()
	s.mu.Lock()
	s.active--
	s.mu.Unlock()
	if s.finishes != nil {
		s.finishes <- struct{}{}
	}
	return ctx.Err()
}

type scheduledQuestionContextFeishuSender struct {
	mu          sync.Mutex
	titles      []string
	signals     chan struct{}
	waitForDone bool
	active      int
}

func (s *scheduledQuestionContextFeishuSender) Send(ctx context.Context, _, _ string, notification scheduledQuestionNotification) (bool, string) {
	s.mu.Lock()
	s.titles = append(s.titles, notification.Title)
	s.active++
	s.mu.Unlock()
	if s.signals != nil {
		s.signals <- struct{}{}
	}
	if s.waitForDone {
		<-ctx.Done()
	}
	s.mu.Lock()
	s.active--
	s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err.Error()
	}
	return true, "sent"
}

func TestScheduledQuestionSlowSMTPDoesNotStarveFeishuAlertAndReport(t *testing.T) {
	svc, repo, _, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	plan := scheduledQuestionValidPlan(1)
	plan.NotifyEnabled, plan.ReportEnabled, plan.EmailEnabled, plan.FeishuEnabled = true, true, true, true
	plan.FeishuWebhookURL = scheduledQuestionTestWebhook
	repo.plans[1] = plan
	svc.settings = &scheduledQuestionSettingsStub{values: map[string]string{SettingKeyAccountQuotaNotifyEmails: `[{"email":"admin@example.com","verified":true}]`}}
	mail := &scheduledQuestionSlowEmailSender{}
	feishu := &scheduledQuestionContextFeishuSender{}
	svc.email, svc.feishu = mail, feishu
	svc.notificationTimeouts = scheduledQuestionNotificationTimeouts{Email: 25 * time.Millisecond, Feishu: time.Second, Stage: time.Second}
	run := &ScheduledQuestionTestRun{Status: "success", StartedAt: svc.now(), Summary: []ScheduledQuestionTestAccountSummary{{
		AccountID: 1, AccountName: "account", Alerting: true, Ratio: scheduledQuestionPtr(0), Total: 1, Judged: 1,
	}}}
	states, err := svc.updateAlertStates(context.Background(), plan, run)
	require.NoError(t, err)
	require.Equal(t, "success", run.Status)
	require.Len(t, states, 1)
	require.NotNil(t, states[0].LastAlertAt, "healthy feishu advances alert cooldown despite SMTP timeout")
	require.Equal(t, []ScheduledQuestionTestNotificationResult{
		{Kind: "alert", Channel: "email", Detail: "sent 0/1 recipients (timeout)"},
		{Kind: "alert", Channel: "feishu", Success: true, Detail: "sent"},
		{Kind: "report", Channel: "email", Detail: "sent 0/1 recipients (timeout)"},
		{Kind: "report", Channel: "feishu", Success: true, Detail: "sent"},
	}, run.NotificationResults)
	mail.mu.Lock()
	require.Equal(t, 2, mail.calls)
	require.Zero(t, mail.active, "all SMTP workers must join before returning")
	mail.mu.Unlock()
	feishu.mu.Lock()
	require.Len(t, feishu.titles, 2)
	require.Contains(t, feishu.titles[0], "Scheduled test alert")
	require.Contains(t, feishu.titles[1], "Scheduled test report")
	feishu.mu.Unlock()
}

func TestScheduledQuestionNotificationChannelsStartTogetherAndJoinOnCancel(t *testing.T) {
	svc, _, _, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	svc.settings = &scheduledQuestionSettingsStub{values: map[string]string{SettingKeyAccountQuotaNotifyEmails: `[{"email":"admin@example.com","verified":true}]`}}
	mail := &scheduledQuestionSlowEmailSender{signals: make(chan struct{}, 1)}
	feishu := &scheduledQuestionContextFeishuSender{signals: make(chan struct{}, 1), waitForDone: true}
	svc.email, svc.feishu = mail, feishu
	plan := &ScheduledQuestionTestPlan{EmailEnabled: true, FeishuEnabled: true, FeishuWebhookURL: scheduledQuestionTestWebhook}
	run := &ScheduledQuestionTestRun{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan bool, 1)
	go func() { done <- svc.sendScheduledQuestionNotification(ctx, plan, run, "report", nil) }()
	for _, signal := range []<-chan struct{}{mail.signals, feishu.signals} {
		select {
		case <-signal:
		case <-time.After(time.Second):
			cancel()
			<-done
			t.Fatal("both channels must start before either finishes")
		}
	}
	cancel()
	select {
	case sent := <-done:
		require.False(t, sent)
	case <-time.After(time.Second):
		t.Fatal("notification workers did not join on cancellation")
	}
	require.Equal(t, []ScheduledQuestionTestNotificationResult{
		{Kind: "report", Channel: "email", Detail: "sent 0/1 recipients (aborted)"},
		{Kind: "report", Channel: "feishu", Detail: "aborted"},
	}, run.NotificationResults)
	mail.mu.Lock()
	require.Zero(t, mail.active)
	mail.mu.Unlock()
	feishu.mu.Lock()
	require.Zero(t, feishu.active)
	feishu.mu.Unlock()
}

func TestScheduledQuestionNotificationFeishuHasIndependentTimeout(t *testing.T) {
	svc := &ScheduledQuestionTestService{feishu: &scheduledQuestionContextFeishuSender{waitForDone: true}, notificationTimeouts: scheduledQuestionNotificationTimeouts{Feishu: 20 * time.Millisecond}}
	plan := &ScheduledQuestionTestPlan{FeishuEnabled: true, FeishuWebhookURL: scheduledQuestionTestWebhook}
	run := &ScheduledQuestionTestRun{}
	require.False(t, svc.sendScheduledQuestionNotification(context.Background(), plan, run, "report", nil))
	require.Equal(t, []ScheduledQuestionTestNotificationResult{{Kind: "report", Channel: "feishu", Detail: "timeout"}}, run.NotificationResults)
}

type scheduledQuestionContextEmailFunc func(context.Context, string, string, string) error

func (f scheduledQuestionContextEmailFunc) SendEmail(ctx context.Context, to, subject, body string) error {
	return f(ctx, to, subject, body)
}

func TestScheduledQuestionNotificationEmailPartialSuccessSurvivesChannelTimeout(t *testing.T) {
	svc := &ScheduledQuestionTestService{
		settings: &scheduledQuestionSettingsStub{values: map[string]string{SettingKeyAccountQuotaNotifyEmails: `[
			{"email":"first@example.com","verified":true},{"email":"slow@example.com","verified":true}
		]`}},
		notificationTimeouts: scheduledQuestionNotificationTimeouts{Email: 20 * time.Millisecond},
	}
	svc.email = scheduledQuestionContextEmailFunc(func(ctx context.Context, to, _, _ string) error {
		if to == "first@example.com" {
			return nil
		}
		<-ctx.Done()
		return ctx.Err()
	})
	run := &ScheduledQuestionTestRun{}
	require.True(t, svc.sendScheduledQuestionNotification(context.Background(), &ScheduledQuestionTestPlan{EmailEnabled: true}, run, "alert", nil))
	require.Equal(t, []ScheduledQuestionTestNotificationResult{{Kind: "alert", Channel: "email", Success: true, Detail: "sent 1/2 recipients (timeout)"}}, run.NotificationResults)
}

func TestScheduledQuestionEmailSafeFailureCategories(t *testing.T) {
	for _, tc := range []struct {
		name, category string
		err            error
	}{
		{name: "wrapped socket timeout", category: "timeout", err: fmt.Errorf("SMTP failure with private response: %w", &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded})},
		{name: "wrapped context timeout", category: "timeout", err: fmt.Errorf("SMTP private response: %w", context.DeadlineExceeded)},
		{name: "wrapped cancel", category: "aborted", err: fmt.Errorf("SMTP private response: %w", context.Canceled)},
		{name: "connection error", category: "connection failed", err: fmt.Errorf("dial: %w", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("token-sensitive-1234 private response")})},
		{name: "authentication reply", category: "auth failed", err: fmt.Errorf("mail operation: %w", &textproto.Error{Code: 535, Msg: "secret-value private response"})},
		{name: "authentication wrapper", category: "auth failed", err: fmt.Errorf("smtp auth: %w", errors.New("secret-value private response"))},
		{name: "rejected recipient", category: "rejected", err: fmt.Errorf("smtp rcpt: %w", &textproto.Error{Code: 550, Msg: scheduledQuestionTestWebhook})},
		{name: "unrecognized reply", category: "rejected", err: errors.New("secret-value private response")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, partial := range []bool{false, true} {
				svc := &ScheduledQuestionTestService{settings: &scheduledQuestionSettingsStub{values: map[string]string{
					SettingKeyAccountQuotaNotifyEmails: `[{"email":"first@example.com","verified":true},{"email":"second@example.com","verified":true}]`,
				}}}
				svc.email = scheduledQuestionContextEmailFunc(func(ctx context.Context, to, _, _ string) error {
					require.NoError(t, ctx.Err(), "socket timeout must classify without expiring the context")
					if partial && to == "first@example.com" {
						return nil
					}
					return tc.err
				})
				plan := &ScheduledQuestionTestPlan{EmailEnabled: true, FeishuWebhookURL: scheduledQuestionTestWebhook, FeishuSecret: "secret-value"}
				run := &ScheduledQuestionTestRun{}
				require.Equal(t, partial, svc.sendScheduledQuestionNotification(context.Background(), plan, run, "report", nil))
				sent := 0
				if partial {
					sent = 1
				}
				require.Equal(t, []ScheduledQuestionTestNotificationResult{{Kind: "report", Channel: "email", Success: partial, Detail: fmt.Sprintf("sent %d/2 recipients (%s)", sent, tc.category)}}, run.NotificationResults)
			}
		})
	}
}

func TestScheduledQuestionReportContentsAndOrdering(t *testing.T) {
	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	plan := &ScheduledQuestionTestPlan{Name: "report plan", MinPassRatio: 80}
	run := &ScheduledQuestionTestRun{
		StartedAt: now.Add(-2 * time.Minute), FinishedAt: now, Status: "failed", ErrorMessage: strings.Repeat("轮次失败", 100),
		TotalAccounts: 5, PassedAccounts: 2, AlertedAccounts: 2, PausedAccounts: 1, ResumedAccounts: 1, AutoPauseSkipped: true,
		Summary: []ScheduledQuestionTestAccountSummary{
			{AccountID: 1, AccountName: "passing", Passed: 2, Judged: 2, Ratio: scheduledQuestionPtr(100)},
			{AccountID: 2, AccountName: "alerting", Passed: 1, Judged: 2, Ratio: scheduledQuestionPtr(50), Alerting: true},
			{AccountID: 3, AccountName: "paused", Ratio: scheduledQuestionPtr(0), Alerting: true, ScheduleAction: "paused", Error: "unsafe account upstream response"},
			{AccountID: 4, AccountName: "manual"},
			{AccountID: 5, AccountName: "resume failed", Ratio: scheduledQuestionPtr(100), ScheduleAction: "resume_failed", AutoPaused: true},
		},
	}
	notification := buildScheduledQuestionNotification("Site", plan, run, "report", nil, now)
	require.Equal(t, "[Site] 定时测试报告 / Scheduled test report: report plan", notification.Title)
	require.Equal(t, "red", notification.Color)
	body := strings.Join(notification.Lines, "\n")
	require.Contains(t, body, run.StartedAt.In(time.Local).Format("2006-01-02 15:04:05 -07:00"))
	require.Contains(t, body, "Duration: 2m0s")
	require.Contains(t, body, "Status: failed")
	require.Contains(t, body, "Threshold: 80%")
	require.Contains(t, body, "Passed: 2 · 未通过 / Failed: 1 · 需人工 / Manual: 1 · 出错 / Errors: 1 · 跳过 / Skipped: 0 · 未完成 / Incomplete: 0 · 总账号 / Total: 5")
	require.Contains(t, body, "Paused: 1 · 自动恢复 / Resumed: 1 · 暂停/恢复失败 / Action failures: 1")
	require.Contains(t, body, "Auto-pause skipped")
	require.Contains(t, body, "Round error: "+truncateScheduledQuestionText(run.ErrorMessage, 300))
	require.NotContains(t, body, strings.Repeat("轮次失败", 76))
	require.NotContains(t, body, "unsafe account upstream response")
	rows := notification.Rows
	require.Len(t, rows, 5)
	require.Equal(t, "paused (3)", rows[0].Account)
	require.Equal(t, "出错", rows[0].Result)
	require.Equal(t, "已暂停", rows[0].Action)
	require.Equal(t, "出错", rows[0].Note)
	require.Equal(t, "resume failed (5)", rows[1].Account)
	require.Equal(t, "恢复失败", rows[1].Action)
	require.Equal(t, "alerting (2)", rows[2].Account)
	require.Equal(t, "passing (1)", rows[3].Account)
	require.Equal(t, "manual (4)", rows[4].Account)
	require.Equal(t, int64(1), run.Summary[0].AccountID, "sorting must preserve persisted account order")
}

func TestScheduledQuestionNotificationCardColors(t *testing.T) {
	for _, tc := range []struct {
		name, kind, status, color string
		alerting, paused          int
	}{
		{name: "alert", kind: "alert", color: "red"},
		{name: "recovered", kind: "recovered", color: "green"},
		{name: "report all pass", kind: "report", status: "success", color: "green"},
		{name: "report alert", kind: "report", status: "success", alerting: 1, color: "orange"},
		{name: "report paused", kind: "report", status: "success", paused: 1, color: "orange"},
		{name: "report failed overrides alert", kind: "report", status: "failed", alerting: 1, color: "red"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notification := buildScheduledQuestionNotification("Site", &ScheduledQuestionTestPlan{}, &ScheduledQuestionTestRun{
				Status: tc.status, AlertedAccounts: tc.alerting, PausedAccounts: tc.paused,
			}, tc.kind, nil, time.Now())
			require.Equal(t, tc.color, notification.Color)
		})
	}
}

func TestScheduledQuestionReportEmailListsAllWhileFeishuCapsDetails(t *testing.T) {
	svc, _, _, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	svc.settings = &scheduledQuestionSettingsStub{values: map[string]string{SettingKeyAccountQuotaNotifyEmails: `[{"email":"admin@example.com","verified":true}]`}}
	mail := &scheduledQuestionEmailStub{}
	svc.email = mail
	plan := scheduledQuestionValidPlan(1)
	plan.EmailEnabled = true
	run := &ScheduledQuestionTestRun{Status: "success", StartedAt: svc.now(), TotalAccounts: 65}
	for i := 0; i < 65; i++ {
		run.Summary = append(run.Summary, ScheduledQuestionTestAccountSummary{AccountID: int64(i + 1), AccountName: fmt.Sprintf("account-%02d", i+1)})
	}
	require.True(t, svc.sendScheduledQuestionNotification(context.Background(), plan, run, "report", nil))
	require.Len(t, mail.mails, 1)
	require.Contains(t, mail.mails[0].body, "account-65")
	notification := buildScheduledQuestionNotification("Site", plan, run, "report", nil, svc.now())
	data, err := scheduledQuestionFeishuPayload(notification, "", svc.now())
	require.NoError(t, err)
	var message scheduledQuestionFeishuMessage
	require.NoError(t, json.Unmarshal(data, &message))
	require.Len(t, message.Card.Elements, notification.DetailStart+2)
	table := message.Card.Elements[notification.DetailStart]
	require.Equal(t, "table", table.Tag)
	require.Len(t, table.Rows, 50)
	require.Equal(t, "另有 15 个账号未列出", message.Card.Elements[len(message.Card.Elements)-1].Text.Content)
	require.NotContains(t, string(data), "account-65")
}

func TestScheduledQuestionNotificationSiteNameFallback(t *testing.T) {
	for _, settings := range []SettingRepository{
		nil,
		&scheduledQuestionSettingsStub{},
		&scheduledQuestionSettingsStub{values: map[string]string{SettingKeySiteName: " "}},
		&scheduledQuestionSettingsStub{values: map[string]string{SettingKeySiteName: "unsafe"}, errors: map[string]error{SettingKeySiteName: errors.New("unavailable")}},
	} {
		svc := &ScheduledQuestionTestService{settings: settings}
		require.Equal(t, defaultSiteName, svc.notificationSiteName(context.Background()))
	}
	svc := &ScheduledQuestionTestService{settings: &scheduledQuestionSettingsStub{values: map[string]string{SettingKeySiteName: " Example "}}}
	require.Equal(t, "Example", svc.notificationSiteName(context.Background()))
}

func TestScheduledQuestionAllSkippedReportNoticeAppearsInEmailAndFeishu(t *testing.T) {
	const notice = "本轮所有账号额度已用尽，未执行测试"
	svc, _, _, _ := newScheduledQuestionFixture()
	defer svc.Stop()
	svc.settings = &scheduledQuestionSettingsStub{values: map[string]string{
		SettingKeyAccountQuotaNotifyEmails: `[{"email":"admin@example.com","verified":true}]`,
	}}
	mail := &scheduledQuestionEmailStub{}
	feishu := &scheduledQuestionFeishuNotificationStub{success: true, detail: "sent"}
	svc.email, svc.feishu = mail, feishu
	plan := scheduledQuestionValidPlan(1)
	plan.EmailEnabled, plan.FeishuEnabled, plan.FeishuWebhookURL = true, true, scheduledQuestionTestWebhook
	run := &ScheduledQuestionTestRun{
		StartedAt: svc.now(), TotalAccounts: 2, SkippedAccounts: 2,
		Summary: []ScheduledQuestionTestAccountSummary{
			{AccountID: 1, Result: "skipped", SkipReason: "quota_exceeded"},
			{AccountID: 2, Result: "skipped", SkipReason: "rate_limited"},
		},
	}
	require.True(t, svc.sendScheduledQuestionNotification(context.Background(), plan, run, "report", nil))
	require.Len(t, mail.mails, 1)
	require.Contains(t, mail.mails[0].body, notice)
	require.Less(t, strings.Index(mail.mails[0].body, notice), strings.Index(mail.mails[0].body, "通过 / Passed:"))
	require.Contains(t, mail.mails[0].body, "<table")
	require.Len(t, feishu.calls, 1)
	data, err := scheduledQuestionFeishuPayload(feishu.calls[0].notification, "", svc.now())
	require.NoError(t, err)
	var message scheduledQuestionFeishuMessage
	require.NoError(t, json.Unmarshal(data, &message))
	noticeIndex, statsIndex := -1, -1
	for i, element := range message.Card.Elements {
		if element.Tag != "div" {
			continue
		}
		if element.Text.Content == notice {
			noticeIndex = i
		}
		if strings.HasPrefix(element.Text.Content, "通过 / Passed:") {
			statsIndex = i
		}
	}
	require.GreaterOrEqual(t, noticeIndex, 0)
	require.Greater(t, statsIndex, noticeIndex)
	require.Len(t, message.Card.Elements[len(message.Card.Elements)-1].Rows, 2)
}
