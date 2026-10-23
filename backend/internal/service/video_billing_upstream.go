package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrVideoUpstreamOriginChanged = errors.New("上游地址已变更")

// url.Error quotes its URL, potentially adding another escaping layer. Do not
// log that transport payload at all: it can contain relay credentials or paths.
func safeVideoReconcileError(err error) string {
	var transportError *url.Error
	if errors.As(err, &transportError) {
		return "upstream transport request failed"
	}
	return sanitizeNativeVideoTestError(err.Error(), nil)
}

// Only the origin is retained: never persist credentials, query or relay paths.
func NormalizeVideoUpstreamOrigin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Host == "" || u.Opaque != "" {
		return "", errors.New("invalid video upstream address; URL userinfo is forbidden")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return "", errors.New("video upstream address must use HTTP or HTTPS")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" {
		return "", errors.New("invalid video upstream host")
	}
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", errors.New("invalid video upstream port")
		}
		port = strconv.Itoa(n)
	}
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host, nil
}

func videoTaskUpstreamOrigin(account *Account, protocol string) (string, error) {
	if account == nil {
		return "", ErrVideoReservationInvalid
	}
	base := account.VideoBaseURL()
	if protocol != VideoProtocolSeedance && protocol != VideoProtocolGemini && protocol != VideoProtocolMiniMax {
		base = account.GetGrokMediaBaseURL()
	}
	return NormalizeVideoUpstreamOrigin(base)
}

// Optional for old test/custom repositories. Production binds legacy origins in
// SQL with first-writer-wins semantics before the first authenticated request.
type VideoBillingUpstreamOriginRepository interface {
	BindUpstreamOrigin(ctx context.Context, id, origin string) (string, error)
}

func (s *OpenAIGatewayService) checkVideoPollingOrigin(ctx context.Context, v *VideoBillingReservation, account *Account) error {
	current, err := videoTaskUpstreamOrigin(account, v.Protocol)
	if err != nil {
		return s.stopChangedVideoUpstream(ctx, v, "上游地址无效或含用户凭据")
	}
	var snapshot VideoBillingSnapshot
	if err := json.Unmarshal(v.Snapshot, &snapshot); err != nil {
		return ErrVideoReservationInvalid
	}
	if snapshot.UpstreamOrigin == "" {
		binder, ok := s.videoBillingRepo.(VideoBillingUpstreamOriginRepository)
		if !ok {
			return s.stopChangedVideoUpstream(ctx, v, "旧预占无法冻结上游地址，需人工处理")
		}
		snapshot.UpstreamOrigin, err = binder.BindUpstreamOrigin(ctx, v.ID, current)
		if err != nil {
			return err
		}
		slog.Warn("video_billing.legacy_upstream_origin_bound", "reservation_id", v.ID, "account_id", v.AccountID)
	}
	if current != snapshot.UpstreamOrigin {
		return s.stopChangedVideoUpstream(ctx, v, "上游地址已变更")
	}
	return nil
}

func (s *OpenAIGatewayService) stopChangedVideoUpstream(ctx context.Context, v *VideoBillingReservation, reason string) error {
	detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.MarkVideoTaskManual(detached, v, reason); err != nil {
		return err
	}
	return ErrVideoUpstreamOriginChanged
}

type VideoAccountReservationInspector interface {
	CountActiveVideoBillingReservations(context.Context, int64) (int64, error)
}

func warnVideoAccountUpstreamChange(ctx context.Context, repo AccountRepository, before string, after *Account) {
	if after == nil || before == after.GetCredential("base_url") || (after.Platform != PlatformVideo && after.Platform != PlatformGrok) {
		return
	}
	inspector, ok := repo.(VideoAccountReservationInspector)
	if !ok {
		slog.Warn("video_billing.account_upstream_changed_hold_check_unavailable", "account_id", after.ID)
		return
	}
	count, err := inspector.CountActiveVideoBillingReservations(ctx, after.ID)
	if err != nil {
		slog.Warn("video_billing.account_upstream_changed_hold_check_failed", "account_id", after.ID)
	} else if count > 0 {
		slog.Warn("video_billing.account_upstream_changed_with_active_holds", "account_id", after.ID, "active_reservations", count)
	}
}
