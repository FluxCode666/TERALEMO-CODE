package service

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/outbound"
)

const (
	alertNotifyTimeout        = 10 * time.Second
	alertNotifyConcurrency    = 8
	alertNotifyPendingBatches = 128
	alertNotifyRate           = 30
	alertNotifyRateWindow     = time.Minute
)

// AlertNotifier accepts immutable notification snapshots from alert producers.
type AlertNotifier interface {
	Dispatch(AlertNotification)
}

type AlertNotification struct {
	Event      string
	Status     string
	Severity   string
	Title      string
	Summary    string
	Fields     []AlertNotifyField
	OccurredAt time.Time
	SiteName   string
	Source     AlertNotifySource
	Test       bool
}

type AlertNotifyField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type AlertNotifySource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// AlertNotifyService owns channel settings and asynchronous delivery.
type AlertNotifyService struct {
	repo      SettingRepository
	encryptor SecretEncryptor
	client    alertNotifyHTTPClient
	now       func() time.Time

	configMu sync.Mutex
	config   atomic.Pointer[alertNotifyConfigSnapshot]

	batches chan struct{}
	sends   chan struct{}
	rateMu  sync.Mutex
	recent  map[string][]time.Time
}

func NewAlertNotifyService(repo SettingRepository, encryptor SecretEncryptor, client alertNotifyHTTPClient) *AlertNotifyService {
	if client == nil {
		guarded := outbound.NewClient(alertNotifyTimeout)
		guarded.CheckRedirect = rejectAlertNotifyRedirect
		client = guarded
	}
	return &AlertNotifyService{
		repo: repo, encryptor: encryptor, client: client, now: time.Now,
		batches: make(chan struct{}, alertNotifyPendingBatches),
		sends:   make(chan struct{}, alertNotifyConcurrency),
		recent:  make(map[string][]time.Time),
	}
}

func (s *AlertNotifyService) TestChannel(ctx context.Context, input AlertNotifyChannelInput) error {
	ctx, cancel := context.WithTimeout(ctx, alertNotifyTimeout)
	defer cancel()
	var old alertNotifyStoredChannel
	if input.ID != "" {
		channels, err := s.loadChannels(ctx)
		if err != nil {
			return err
		}
		index := slices.IndexFunc(channels, func(channel alertNotifyStoredChannel) bool { return channel.ID == input.ID })
		if index < 0 {
			return alertNotifyBadRequest("unknown channel id")
		}
		old = channels[index]
	}
	endpoint, err := s.resolveEndpoint(input, old)
	if err != nil {
		return err
	}
	alert := AlertNotification{
		Event: "test", Status: OpsAlertStatusFiring, Severity: "info", Test: true,
		Title: "告警通知渠道连通性测试", Summary: "这是一条测试消息，用于验证告警通知渠道。",
		OccurredAt: s.now().UTC(), SiteName: s.siteName(ctx), Source: AlertNotifySource{Type: "test", ID: input.ID},
	}
	if _, err := s.send(ctx, endpoint, alert); err != nil {
		return alertNotifyBadRequest(err.Error())
	}
	return nil
}

// Dispatch admits a bounded number of event batches without waiting for delivery or capacity.
func (s *AlertNotifyService) Dispatch(alert AlertNotification) {
	if s == nil {
		return
	}
	select {
	case s.batches <- struct{}{}:
		alert.Fields = slices.Clone(alert.Fields)
		go s.dispatch(alert)
	default:
		slog.Warn("alert notification dropped: pending capacity reached", "event", alert.Event,
			"source_type", alert.Source.Type, "source_id", alert.Source.ID)
	}
}

func (s *AlertNotifyService) dispatch(alert AlertNotification) {
	defer func() {
		<-s.batches
		if recover() != nil {
			slog.Error("alert notification dispatch failed", "reason", "unexpected dispatch error")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), alertNotifyTimeout)
	defer cancel()
	channels, err := s.loadChannels(ctx)
	if err != nil {
		slog.Warn("alert notification dispatch failed", "reason", err.Error())
		return
	}
	var matches []alertNotifyStoredChannel
	for _, channel := range channels {
		if matchesAlertNotifyChannel(channel, alert) {
			matches = append(matches, channel)
		}
	}
	if len(matches) == 0 {
		return
	}
	if alert.SiteName == "" {
		alert.SiteName = s.siteName(ctx)
	}
	var work sync.WaitGroup
	for _, channel := range matches {
		work.Go(func() { s.deliver(channel, alert) })
	}
	work.Wait()
}

func matchesAlertNotifyChannel(channel alertNotifyStoredChannel, alert AlertNotification) bool {
	return channel.Enabled && slices.Contains(channel.Events, alert.Event) &&
		(alert.Event != AlertNotifyEventOps || alert.Status != OpsAlertStatusResolved || channel.SendResolved) &&
		alertNotifySeverity(alert.Severity) >= alertNotifySeverity(channel.MinSeverity)
}

func alertNotifySeverity(severity string) int {
	switch severity {
	case "info":
		return 1
	case "warning":
		return 2
	case "critical":
		return 3
	default:
		return 0
	}
}

func (s *AlertNotifyService) allow(id string) bool {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	now := s.now()
	cursor := now.Add(-alertNotifyRateWindow)
	for channelID, deliveries := range s.recent {
		if !deliveries[len(deliveries)-1].After(cursor) {
			delete(s.recent, channelID)
		}
	}
	recent := s.recent[id]
	for len(recent) > 0 && !recent[0].After(cursor) {
		recent = recent[1:]
	}
	if len(recent) >= alertNotifyRate {
		return false
	}
	s.recent[id] = append(recent, now)
	return true
}

func (s *AlertNotifyService) siteName(ctx context.Context) string {
	name, err := s.repo.GetValue(ctx, SettingKeySiteName)
	if err != nil || strings.TrimSpace(name) == "" {
		return defaultSiteName
	}
	return name
}

func (s *AlertNotifyService) deliver(channel alertNotifyStoredChannel, alert AlertNotification) {
	s.sends <- struct{}{}
	defer func() {
		<-s.sends
		if recover() != nil {
			logAlertNotifyFailure(channel, "unexpected delivery error")
		}
	}()
	if !s.allow(channel.ID) {
		logAlertNotifyFailure(channel, "rate limit exceeded")
		return
	}
	endpoint, err := s.resolveEndpoint(AlertNotifyChannelInput{Type: channel.Type}, channel)
	if err != nil {
		logAlertNotifyFailure(channel, err.Error())
		return
	}
	retry, err := s.send(context.Background(), endpoint, alert)
	if retry {
		_, err = s.send(context.Background(), endpoint, alert)
	}
	if err != nil {
		logAlertNotifyFailure(channel, err.Error())
	}
}

func logAlertNotifyFailure(channel alertNotifyStoredChannel, reason string) {
	slog.Warn("alert notification not delivered", "channel_id", channel.ID, "channel_name", channel.Name, "channel_type", channel.Type, "reason", reason)
}
