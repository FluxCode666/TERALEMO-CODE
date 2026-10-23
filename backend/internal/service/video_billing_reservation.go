package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	VideoReservationPreparing = "preparing"
	VideoReservationAccepted  = "accepted"
	VideoReservationSettled   = "settled"
	VideoReservationReleased  = "released"
	VideoReservationManual    = "manual"
)

var (
	ErrVideoReservationNotFound      = errors.New("video billing reservation not found")
	ErrVideoReservationConflict      = errors.New("video billing reservation conflict")
	ErrVideoReservationInvalid       = errors.New("invalid video billing reservation")
	ErrVideoReservationQuotaExceeded = infraerrors.TooManyRequests("VIDEO_RESERVATION_QUOTA_EXCEEDED", "Video cost exceeds available quota after outstanding reservations.")
)

// VideoBillingReservation is the durable payment obligation for an upstream task.
// Contract and Snapshot must contain billing/routing data only, never API secrets.
// A preparing task whose upstream outcome is unknown must retain its hold.
type VideoBillingReservation struct {
	// Admission-only cache floor; never persisted with the task snapshot.
	QuotaUsageFloor             *UserPlatformQuotaCacheEntry `json:"-"`
	ID                          string
	UserID                      int64
	APIKeyID                    int64
	GroupID                     int64
	SubscriptionID              *int64
	EnterpriseID                int64
	EnterpriseDepartmentID      int64
	EnterpriseMemberID          int64
	EnterpriseDepartmentPathIDs []int64
	Platform                    string
	Protocol                    string
	Endpoint                    string
	TaskID                      string
	FileID                      string
	AccountID                   int64
	HoldAmount                  float64
	ActualAmount                float64
	Contract                    json.RawMessage
	Snapshot                    json.RawMessage
	State                       string
	Attempts                    int
	LastError                   string
	NextAttemptAt               time.Time
	LeaseOwner                  string
	LeaseUntil                  *time.Time
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

// VideoBillingQuotaScopeRepository marks scopes whose monetary usage is DB
// authoritative, avoiding Redis-flusher double accounting after durable capture.
type VideoBillingQuotaScopeRepository interface {
	HasVideoBillingScope(ctx context.Context, userID int64, platform string) (bool, error)
}

// VideoReservationSettlement is committed with its dedup record and usage log.
// Billing carries account/key counters; the repository owns payer and cost fields.
type VideoReservationSettlement struct {
	ID            string
	Usage         *UsageLog
	Billing       *UsageBillingCommand
	ManualActorID int64
	ManualReason  string
}

type VideoReservationResult struct {
	Applied              bool
	Capped               bool
	ActualAmount         float64
	NewBalance           *float64
	NewEnterpriseBalance *float64
}

type VideoBillingReservationRepository interface {
	Reserve(context.Context, *VideoBillingReservation) error
	Accept(ctx context.Context, id, taskID string, accountID int64) error
	Get(ctx context.Context, id string) (*VideoBillingReservation, error)
	GetByTask(ctx context.Context, protocol, taskID string) (*VideoBillingReservation, error)
	BindFile(ctx context.Context, id, fileID string) error
	GetByFile(ctx context.Context, protocol, fileID string) (*VideoBillingReservation, error)
	ClaimDue(ctx context.Context, owner string, limit int, lease time.Duration) ([]*VideoBillingReservation, error)
	Reschedule(ctx context.Context, id, owner string, next time.Time, lastError string) error
	MarkManual(ctx context.Context, id, reason string) error
	Settle(context.Context, *VideoReservationSettlement) (*VideoReservationResult, error)
	Release(ctx context.Context, id, reason string) (*VideoReservationResult, error)
}

// NormalizeVideoSubscriptionWindows shares subscription reset rules with the
// durable reservation transaction, including single-day grants and expiry.
func NormalizeVideoSubscriptionWindows(sub *UserSubscription, now time.Time) {
	if sub == nil {
		return
	}
	if !sub.IsWindowActivated() {
		daily := startOfDay(now)
		periodic := now
		sub.DailyWindowStart, sub.WeeklyWindowStart, sub.MonthlyWindowStart = &daily, &periodic, &periodic
	}
	if start, ok := sub.automaticDailyWindowStartAt(now); ok {
		sub.DailyWindowStart, sub.DailyUsageUSD = &start, 0
	}
	if start, ok := sub.automaticWindowStartAt(sub.WeeklyWindowStart, 7*24*time.Hour, now); ok {
		sub.WeeklyWindowStart, sub.WeeklyUsageUSD = &start, 0
	}
	if start, ok := sub.automaticWindowStartAt(sub.MonthlyWindowStart, 30*24*time.Hour, now); ok {
		sub.MonthlyWindowStart, sub.MonthlyUsageUSD = &start, 0
	}
}
