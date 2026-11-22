package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	AlertNotifyEventOps   = "ops.alert"
	AlertNotifyEventQuota = "account.quota_alert"
	alertNotifyFeishu     = "feishu"
	alertNotifyWebhook    = "webhook"
	alertNotifyMaxConfig  = 20
	alertNotifyCacheTTL   = 30 * time.Second
)

type alertNotifyEventMask uint8

const (
	alertNotifyOpsMask alertNotifyEventMask = 1 << iota
	alertNotifyQuotaMask
)

type alertNotifyConfigSnapshot struct {
	channels []alertNotifyStoredChannel
	enabled  alertNotifyEventMask
	expires  time.Time
}

func alertNotifyEventBit(event string) alertNotifyEventMask {
	switch event {
	case AlertNotifyEventOps:
		return alertNotifyOpsMask
	case AlertNotifyEventQuota:
		return alertNotifyQuotaMask
	default:
		return 0
	}
}

// HasEnabledChannel reads the last published configuration without touching storage.
func (s *AlertNotifyService) HasEnabledChannel(event string) bool {
	if s == nil {
		return false
	}
	snapshot := s.config.Load()
	return snapshot != nil && snapshot.enabled&alertNotifyEventBit(event) != 0
}

func (s *AlertNotifyService) publishChannels(channels []alertNotifyStoredChannel) {
	var enabled alertNotifyEventMask
	for _, channel := range channels {
		if channel.Enabled {
			for _, event := range channel.Events {
				enabled |= alertNotifyEventBit(event)
			}
		}
	}
	s.config.Store(&alertNotifyConfigSnapshot{channels: channels, enabled: enabled, expires: s.now().Add(alertNotifyCacheTTL)})
}

// AlertNotifyChannelInput is shared by the configuration and connection test APIs.
type AlertNotifyChannelInput struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Enabled      bool     `json:"enabled"`
	URL          string   `json:"url"`
	Secret       string   `json:"secret"`
	ClearSecret  bool     `json:"clear_secret"`
	Events       []string `json:"events"`
	MinSeverity  string   `json:"min_severity"`
	SendResolved bool     `json:"send_resolved"`
}

type alertNotifyChannelMetadata struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	Enabled      bool      `json:"enabled"`
	Events       []string  `json:"events"`
	MinSeverity  string    `json:"min_severity"`
	SendResolved bool      `json:"send_resolved"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type alertNotifyStoredChannel struct {
	alertNotifyChannelMetadata
	URLEnc    string `json:"url_enc"`
	SecretEnc string `json:"secret_enc,omitempty"`
}

// AlertNotifyChannelView never contains raw credentials.
type AlertNotifyChannelView struct {
	alertNotifyChannelMetadata
	URLMasked        string `json:"url_masked"`
	SecretConfigured bool   `json:"secret_configured"`
}

type AlertNotifyChannelsView struct {
	Channels        []AlertNotifyChannelView `json:"channels"`
	SupportedEvents []string                 `json:"supported_events"`
}

var errAlertNotifyConfig = infraerrors.InternalServer("ALERT_NOTIFY_CONFIG", "alert notification configuration is unavailable")

func alertNotifyBadRequest(message string) error {
	return infraerrors.BadRequest("ALERT_NOTIFY_INVALID", message)
}

func (s *AlertNotifyService) GetChannels(ctx context.Context) (AlertNotifyChannelsView, error) {
	channels, err := s.loadChannels(ctx)
	if err != nil {
		return AlertNotifyChannelsView{}, err
	}
	return s.channelsView(channels)
}

func (s *AlertNotifyService) ReplaceChannels(ctx context.Context, inputs []AlertNotifyChannelInput) (AlertNotifyChannelsView, error) {
	if len(inputs) > alertNotifyMaxConfig {
		return AlertNotifyChannelsView{}, alertNotifyBadRequest("at most 20 alert notification channels are allowed")
	}

	s.configMu.Lock()
	defer s.configMu.Unlock()
	previous, err := s.readChannels(ctx)
	if err != nil {
		return AlertNotifyChannelsView{}, err
	}
	byID := make(map[string]alertNotifyStoredChannel, len(previous))
	for _, channel := range previous {
		byID[channel.ID] = channel
	}
	seen := make(map[string]bool, len(inputs))
	now := s.now().UTC()
	channels := make([]alertNotifyStoredChannel, 0, len(inputs))
	for index, input := range inputs {
		old := byID[input.ID]
		if input.ID != "" {
			if old.ID == "" {
				return AlertNotifyChannelsView{}, alertNotifyBadRequest(fmt.Sprintf("channel %d: unknown id", index+1))
			}
			if seen[input.ID] {
				return AlertNotifyChannelsView{}, alertNotifyBadRequest(fmt.Sprintf("channel %d: duplicate id", index+1))
			}
		} else {
			for {
				var id [8]byte
				if _, err := rand.Read(id[:]); err != nil {
					return AlertNotifyChannelsView{}, errAlertNotifyConfig
				}
				input.ID = hex.EncodeToString(id[:])
				if !seen[input.ID] && byID[input.ID].ID == "" {
					break
				}
			}
		}
		seen[input.ID] = true
		input.Name = strings.TrimSpace(input.Name)
		events, err := validateAlertNotifyMetadata(input)
		if err != nil {
			return AlertNotifyChannelsView{}, alertNotifyBadRequest(fmt.Sprintf("channel %d: %s", index+1, err))
		}
		endpoint, err := s.resolveEndpoint(input, old)
		if err != nil {
			return AlertNotifyChannelsView{}, err
		}
		channel := alertNotifyStoredChannel{
			alertNotifyChannelMetadata: alertNotifyChannelMetadata{
				ID: input.ID, Name: input.Name, Type: input.Type, Enabled: input.Enabled, Events: events,
				MinSeverity: input.MinSeverity, SendResolved: input.SendResolved, UpdatedAt: old.UpdatedAt,
			},
			URLEnc: old.URLEnc, SecretEnc: old.SecretEnc,
		}
		if input.URL != "" {
			channel.URLEnc, err = s.encryptAlertNotifyValue(endpoint.url, old.URLEnc)
			if err != nil {
				return AlertNotifyChannelsView{}, errAlertNotifyConfig
			}
		}
		switch {
		case input.ClearSecret:
			channel.SecretEnc = ""
		case input.Secret != "":
			channel.SecretEnc, err = s.encryptAlertNotifyValue(endpoint.secret, old.SecretEnc)
			if err != nil {
				return AlertNotifyChannelsView{}, errAlertNotifyConfig
			}
		}
		if old.ID == "" || !sameAlertNotifyChannel(channel, old) {
			channel.UpdatedAt = now
		}
		channels = append(channels, channel)
	}
	view, err := s.channelsView(channels)
	if err != nil {
		return AlertNotifyChannelsView{}, err
	}
	body, err := json.Marshal(channels)
	if err != nil || s.repo.Set(ctx, SettingKeyAlertNotifyChannels, string(body)) != nil {
		return AlertNotifyChannelsView{}, errAlertNotifyConfig
	}
	s.publishChannels(channels)
	return view, nil
}

func (s *AlertNotifyService) encryptAlertNotifyValue(value, previous string) (string, error) {
	if previous != "" {
		if plaintext, err := s.encryptor.Decrypt(previous); err == nil && plaintext == value {
			return previous, nil
		}
	}
	return s.encryptor.Encrypt(value)
}

func sameAlertNotifyChannel(channel, previous alertNotifyStoredChannel) bool {
	return channel.Name == previous.Name && channel.Type == previous.Type && channel.Enabled == previous.Enabled &&
		slices.Equal(channel.Events, previous.Events) && channel.MinSeverity == previous.MinSeverity &&
		channel.SendResolved == previous.SendResolved && channel.URLEnc == previous.URLEnc && channel.SecretEnc == previous.SecretEnc
}

func validateAlertNotifyMetadata(input AlertNotifyChannelInput) ([]string, error) {
	if length := utf8.RuneCountInString(input.Name); length < 1 || length > 64 {
		return nil, errors.New("name must contain 1 to 64 characters")
	}
	if input.MinSeverity != "" && alertNotifySeverity(input.MinSeverity) == 0 {
		return nil, errors.New("min_severity must be empty, info, warning, or critical")
	}
	if len(input.Events) == 0 {
		return nil, errors.New("at least one event is required")
	}
	events := make([]string, 0, 2)
	seen := make(map[string]bool, 2)
	for _, event := range input.Events {
		if event != AlertNotifyEventOps && event != AlertNotifyEventQuota {
			return nil, errors.New("events contains an unsupported event")
		}
		if !seen[event] {
			seen[event] = true
			events = append(events, event)
		}
	}
	return events, nil
}

func (s *AlertNotifyService) loadChannels(ctx context.Context) ([]alertNotifyStoredChannel, error) {
	if snapshot := s.config.Load(); snapshot != nil && s.now().Before(snapshot.expires) {
		return snapshot.channels, nil
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if snapshot := s.config.Load(); snapshot != nil && s.now().Before(snapshot.expires) {
		return snapshot.channels, nil
	}
	channels, err := s.readChannels(ctx)
	if err == nil {
		s.publishChannels(channels)
	}
	return channels, err
}

func (s *AlertNotifyService) refreshChannelsAndSchedule(timingWheel *TimingWheelService) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	delay := alertNotifyCacheTTL
	if _, err := s.loadChannels(ctx); err != nil {
		slog.Warn("alert notification configuration refresh failed", "reason", err.Error())
	} else if snapshot := s.config.Load(); snapshot != nil {
		delay = snapshot.expires.Sub(s.now()).Truncate(time.Second) + time.Second
	}
	timingWheel.Schedule("alert_notify:config", delay, func() { s.refreshChannelsAndSchedule(timingWheel) })
}

func (s *AlertNotifyService) readChannels(ctx context.Context) ([]alertNotifyStoredChannel, error) {
	var channels []alertNotifyStoredChannel
	value, err := s.repo.GetValue(ctx, SettingKeyAlertNotifyChannels)
	if errors.Is(err, ErrSettingNotFound) || err == nil && value == "" {
		return nil, nil
	}
	if err != nil || json.Unmarshal([]byte(value), &channels) != nil || len(channels) > alertNotifyMaxConfig {
		return nil, errAlertNotifyConfig
	}
	return channels, nil
}

func (s *AlertNotifyService) channelsView(channels []alertNotifyStoredChannel) (AlertNotifyChannelsView, error) {
	view := AlertNotifyChannelsView{
		Channels:        make([]AlertNotifyChannelView, 0, len(channels)),
		SupportedEvents: []string{AlertNotifyEventOps, AlertNotifyEventQuota},
	}
	for _, channel := range channels {
		rawURL, err := s.encryptor.Decrypt(channel.URLEnc)
		if err != nil {
			return AlertNotifyChannelsView{}, errAlertNotifyConfig
		}
		metadata := channel.alertNotifyChannelMetadata
		metadata.Events = append([]string{}, metadata.Events...)
		view.Channels = append(view.Channels, AlertNotifyChannelView{
			alertNotifyChannelMetadata: metadata,
			URLMasked:                  maskAlertNotifyURL(rawURL),
			SecretConfigured:           channel.SecretEnc != "",
		})
	}
	return view, nil
}

type alertNotifyEndpoint struct {
	typ    string
	url    string
	secret string
}

func (s *AlertNotifyService) resolveEndpoint(input AlertNotifyChannelInput, old alertNotifyStoredChannel) (alertNotifyEndpoint, error) {
	endpoint := alertNotifyEndpoint{typ: input.Type, url: input.URL, secret: input.Secret}
	if utf8.RuneCountInString(input.Secret) > 256 {
		return alertNotifyEndpoint{}, alertNotifyBadRequest("secret must contain at most 256 characters")
	}
	var err error
	if endpoint.url == "" && old.URLEnc != "" {
		endpoint.url, err = s.encryptor.Decrypt(old.URLEnc)
		if err != nil {
			return alertNotifyEndpoint{}, errAlertNotifyConfig
		}
	}
	if err := validateAlertNotifyURL(endpoint.typ, endpoint.url); err != nil {
		return alertNotifyEndpoint{}, alertNotifyBadRequest(err.Error())
	}
	if input.ClearSecret {
		endpoint.secret = ""
	} else if input.Secret == "" && old.SecretEnc != "" {
		endpoint.secret, err = s.encryptor.Decrypt(old.SecretEnc)
		if err != nil {
			return alertNotifyEndpoint{}, errAlertNotifyConfig
		}
	}
	return endpoint, nil
}

func validateAlertNotifyURL(typ, raw string) error {
	if typ != alertNotifyFeishu && typ != alertNotifyWebhook {
		return errors.New("type must be feishu or webhook")
	}
	if raw == "" || len(raw) > 2048 {
		return errors.New("url is required and must not exceed 2048 bytes")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("url must be an absolute HTTP(S) URL without userinfo")
	}
	if typ == alertNotifyFeishu && parsed.Scheme != "https" {
		return errors.New("feishu requires an HTTPS URL")
	}
	return nil
}

func maskAlertNotifyURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return "****"
	}
	path := parsed.EscapedPath()
	trimmed := strings.TrimRight(path, "/")
	start := strings.LastIndex(trimmed, "/") + 1
	segment, _ := url.PathUnescape(trimmed[start:])
	runes := []rune(segment)
	suffix := ""
	if len(runes) > 4 {
		suffix = url.PathEscape(string(runes[len(runes)-4:]))
	}
	prefix := trimmed[:start]
	trailing := path[len(trimmed):]
	if trimmed == "" {
		prefix, trailing = "/", ""
	}
	return parsed.Scheme + "://" + parsed.Host + prefix + "****" + suffix + trailing
}
