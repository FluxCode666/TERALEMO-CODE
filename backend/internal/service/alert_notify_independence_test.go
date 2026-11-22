//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAlertNotifyEnabledChannelSnapshot(t *testing.T) {
	ctx := t.Context()
	repo := &alertNotifySettingsFixture{}
	svc := NewAlertNotifyService(repo, alertNotifyCipherFixture{}, nil)
	initial := NewAlertNotifyService(repo, alertNotifyCipherFixture{}, nil)
	now := time.Unix(1700000000, 0)
	svc.now = func() time.Time { return now }
	input := alertNotifyInput()
	input.Enabled, input.Events = false, []string{AlertNotifyEventQuota}
	created, err := svc.ReplaceChannels(ctx, []AlertNotifyChannelInput{input})
	require.NoError(t, err)
	require.False(t, svc.HasEnabledChannel(AlertNotifyEventQuota))
	input.ID, input.URL, input.Secret, input.Enabled = created.Channels[0].ID, "", "", true
	_, err = svc.ReplaceChannels(ctx, []AlertNotifyChannelInput{input})
	require.NoError(t, err)
	require.True(t, svc.HasEnabledChannel(AlertNotifyEventQuota))
	require.False(t, svc.HasEnabledChannel(AlertNotifyEventOps))
	require.False(t, svc.HasEnabledChannel("unknown"))
	require.Equal(t, 2, repo.reads)
	_, err = initial.loadChannels(ctx)
	require.NoError(t, err)
	require.True(t, initial.HasEnabledChannel(AlertNotifyEventQuota))
	require.Equal(t, 3, repo.reads)
	for range 20 {
		require.True(t, svc.HasEnabledChannel(AlertNotifyEventQuota))
		_, err = svc.loadChannels(ctx)
		require.NoError(t, err)
	}
	require.Equal(t, 3, repo.reads)

	input.Events = []string{AlertNotifyEventOps}
	_, err = initial.ReplaceChannels(ctx, []AlertNotifyChannelInput{input})
	require.NoError(t, err)
	require.Equal(t, 4, repo.reads)
	require.True(t, svc.HasEnabledChannel(AlertNotifyEventQuota))
	now = now.Add(alertNotifyCacheTTL)
	_, err = svc.loadChannels(ctx)
	require.NoError(t, err)
	require.Equal(t, 5, repo.reads)
	require.False(t, svc.HasEnabledChannel(AlertNotifyEventQuota))
	require.True(t, svc.HasEnabledChannel(AlertNotifyEventOps))
}

type alertNotifyQuotaAccountReader struct{ reads int }

func (repo *alertNotifyQuotaAccountReader) GetByID(context.Context, int64) (*Account, error) {
	repo.reads++
	return nil, ErrAccountNotFound
}

type alertNotifyDelivery struct {
	path    string
	payload alertNotifyWebhookPayload
	err     error
}

func alertNotifyDeliveryClient(deliveries chan<- alertNotifyDelivery) *http.Client {
	return &http.Client{Transport: alertNotifyRoundTrip(func(req *http.Request) (*http.Response, error) {
		var payload alertNotifyWebhookPayload
		err := json.NewDecoder(req.Body).Decode(&payload)
		deliveries <- alertNotifyDelivery{path: req.URL.Path, payload: payload, err: err}
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
	})}
}

func TestAlertNotifyQuotaEmailAndChannelsAreIndependent(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		emailOn     bool
		recipients  bool
		subscribe   bool
		enabled     bool
		wantEmail   bool
		wantChannel bool
	}{
		{name: "email disabled still sends to subscribed channel", recipients: true, subscribe: true, enabled: true, wantChannel: true},
		{name: "email enabled without matching channel only sends email", emailOn: true, recipients: true, enabled: true, wantEmail: true},
		{name: "channel sends with no recipients", emailOn: true, subscribe: true, enabled: true, wantChannel: true},
		{name: "both enabled send independently", emailOn: true, recipients: true, subscribe: true, enabled: true, wantEmail: true, wantChannel: true},
		{name: "email disabled and channel disabled skips fresh account lookup", recipients: true, subscribe: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := t.Context()
			settings := newMockSettingRepo()
			settings.data[SettingKeyAccountQuotaNotifyEnabled] = strconv.FormatBool(scenario.emailOn)
			if scenario.recipients {
				settings.data[SettingKeyAccountQuotaNotifyEmails] = `[{"email":"admin@example.com","verified":true}]`
			}
			smtp, port := startFakeSMTPServer(t, false, false)
			settings.data[SettingKeySMTPHost] = "127.0.0.1"
			settings.data[SettingKeySMTPPort] = strconv.Itoa(port)
			settings.data[SettingKeySMTPUsername] = "sender@example.com"
			settings.data[SettingKeySMTPFrom] = "sender@example.com"
			settings.data[SettingKeySMTPPassword] = "fixture"

			received := make(chan alertNotifyDelivery, 4)
			notifier := NewAlertNotifyService(settings, alertNotifyCipherFixture{}, alertNotifyDeliveryClient(received))
			input := alertNotifyInput()
			input.Enabled = scenario.enabled
			input.Events = []string{AlertNotifyEventOps}
			if scenario.subscribe {
				input.Events = []string{AlertNotifyEventQuota}
			}
			_, err := notifier.ReplaceChannels(ctx, []AlertNotifyChannelInput{input})
			require.NoError(t, err)

			accountRepo := &alertNotifyQuotaAccountReader{}
			balance := NewBalanceNotifyService(NewEmailService(settings, nil), settings, accountRepo)
			balance.SetAlertNotifier(notifier)
			account := &Account{ID: 10, Name: "测试账号", Platform: "openai", Extra: map[string]any{
				"quota_notify_daily_enabled": true, "quota_notify_daily_threshold": 20.0,
			}}
			if !scenario.wantEmail && !scenario.wantChannel {
				balance.CheckAccountQuotaAfterIncrement(ctx, account, 10, nil)
				require.Zero(t, accountRepo.reads)
			}
			balance.CheckAccountQuotaAfterIncrement(ctx, account, 10, &AccountQuotaState{DailyUsed: 85, DailyLimit: 100})
			require.Eventually(t, func() bool { return len(notifier.batches) == 0 }, time.Second, time.Millisecond)
			if scenario.wantChannel {
				require.Len(t, received, 1)
				delivery := <-received
				require.NoError(t, delivery.err)
				require.Equal(t, AlertNotifyEventQuota, delivery.payload.Event)
				require.Equal(t, "warning", delivery.payload.Severity)
				require.Equal(t, "10-daily", delivery.payload.Source.ID)
				require.Contains(t, delivery.payload.Fields, AlertNotifyField{Label: "剩余", Value: "$15.00"})
			} else {
				require.Empty(t, received)
			}
			if scenario.wantEmail {
				require.Eventually(t, func() bool { return smtp.sawCommand("QUIT") }, time.Second, time.Millisecond)
				require.True(t, smtp.sawCommand("RCPT TO:<ADMIN@EXAMPLE.COM>"))
			} else {
				require.Never(t, func() bool { return smtp.conns.Load() > 0 }, 40*time.Millisecond, time.Millisecond)
			}
		})
	}
}

type alertNotifyOpsRepo struct {
	opsRepoMock
	rule  *OpsAlertRule
	event *OpsAlertEvent
	usage float64
}

func (repo *alertNotifyOpsRepo) ListAlertRules(context.Context) ([]*OpsAlertRule, error) {
	return []*OpsAlertRule{repo.rule}, nil
}

func (repo *alertNotifyOpsRepo) GetLatestSystemMetrics(context.Context, int) (*OpsSystemMetricsSnapshot, error) {
	return &OpsSystemMetricsSnapshot{CPUUsagePercent: &repo.usage}, nil
}

func (repo *alertNotifyOpsRepo) GetActiveAlertEvent(context.Context, int64) (*OpsAlertEvent, error) {
	return repo.event, nil
}

func (repo *alertNotifyOpsRepo) CreateAlertEvent(_ context.Context, event *OpsAlertEvent) (*OpsAlertEvent, error) {
	event.ID = 42
	repo.event = event
	return event, nil
}

func (repo *alertNotifyOpsRepo) UpdateAlertEventStatus(_ context.Context, _ int64, status string, resolvedAt *time.Time) error {
	repo.event.Status, repo.event.ResolvedAt = status, resolvedAt
	return nil
}

func TestAlertNotifyOpsEvaluatorSendsDespiteEmailSwitches(t *testing.T) {
	settings := newMockSettingRepo()
	settings.data[SettingKeyOpsEmailNotificationConfig] = `{"alert":{"enabled":false,"recipients":[]}}`
	settings.data[SettingKeyOpsAlertRuntimeSettings] = `{"distributed_lock":{"enabled":false}}`
	received := make(chan alertNotifyDelivery, 4)
	notifier := NewAlertNotifyService(settings, alertNotifyCipherFixture{}, alertNotifyDeliveryClient(received))
	withResolved, withoutResolved := alertNotifyInput(), alertNotifyInput()
	withResolved.SendResolved = true
	withResolved.URL = "https://example.invalid/with-recovery"
	withoutResolved.URL = "https://example.invalid/without-recovery"
	_, err := notifier.ReplaceChannels(t.Context(), []AlertNotifyChannelInput{withResolved, withoutResolved})
	require.NoError(t, err)

	repo := &alertNotifyOpsRepo{usage: 95, rule: &OpsAlertRule{
		ID: 1, Name: "CPU", Enabled: true, Severity: "P0", MetricType: "cpu_usage_percent", Operator: ">", Threshold: 90, NotifyEmail: false,
	}}
	ops := &OpsService{settingRepo: settings, opsRepo: repo}
	evaluator := NewOpsAlertEvaluatorService(ops, repo, NewEmailService(settings, nil), nil, nil, nil)
	evaluator.SetAlertNotifier(notifier)
	evaluator.evaluateOnce(time.Minute)
	require.NotNil(t, repo.event)
	require.False(t, repo.event.EmailSent)
	require.Eventually(t, func() bool { return len(notifier.batches) == 0 }, time.Second, time.Millisecond)
	require.Len(t, received, 2)
	for range 2 {
		delivery := <-received
		require.NoError(t, delivery.err)
		require.Equal(t, AlertNotifyEventOps, delivery.payload.Event)
		require.Equal(t, OpsAlertStatusFiring, delivery.payload.Status)
	}

	repo.usage = 70
	evaluator.evaluateOnce(time.Minute)
	require.Equal(t, OpsAlertStatusResolved, repo.event.Status)
	require.Eventually(t, func() bool { return len(notifier.batches) == 0 }, time.Second, time.Millisecond)
	require.Len(t, received, 1)
	delivery := <-received
	require.NoError(t, delivery.err)
	require.Equal(t, "/with-recovery", delivery.path)
	require.Equal(t, OpsAlertStatusResolved, delivery.payload.Status)
	require.Equal(t, "42", delivery.payload.Source.ID)
}
