package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
)

var (
	ErrVideoManualOnly = infraerrors.Conflict("VIDEO_RESERVATION_NOT_MANUAL", "Only reservations awaiting manual review can be resolved.")
	ErrVideoAdminInput = infraerrors.BadRequest("INVALID_VIDEO_RESERVATION_RESOLUTION", "A reason and valid actual usage are required.")
)

type VideoBillingReservationAdminRepository interface {
	ListVideoBillingReservations(ctx context.Context, state string, page, pageSize int) ([]*VideoBillingReservation, int, error)
}

// VideoBillingAdminItem is deliberately explicit. Never serialize the full
// internal reservation, frozen token card, routing snapshot or credentials.
type VideoBillingAdminItem struct {
	ID                  string    `json:"id"`
	UserID              int64     `json:"user_id"`
	APIKeyID            int64     `json:"api_key_id"`
	GroupID             int64     `json:"group_id"`
	AccountID           int64     `json:"account_id"`
	EnterpriseID        int64     `json:"enterprise_id,omitempty"`
	SubscriptionID      *int64    `json:"subscription_id,omitempty"`
	Platform            string    `json:"platform"`
	Protocol            string    `json:"protocol"`
	TaskID              string    `json:"task_id,omitempty"`
	FileID              string    `json:"file_id,omitempty"`
	State               string    `json:"state"`
	HoldAmount          float64   `json:"hold_amount"`
	ActualAmount        float64   `json:"actual_amount"`
	Model               string    `json:"model,omitempty"`
	Resolution          string    `json:"resolution,omitempty"`
	BillingMode         string    `json:"billing_mode,omitempty"`
	EffectiveMultiplier float64   `json:"effective_multiplier"`
	Attempts            int       `json:"attempts"`
	ReviewNote          string    `json:"review_note,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type VideoBillingAdminPage struct {
	Items    []VideoBillingAdminItem `json:"items"`
	Total    int                     `json:"total"`
	Page     int                     `json:"page"`
	PageSize int                     `json:"page_size"`
}

type VideoBillingManualUsage struct {
	Reason           string   `json:"reason"`
	DurationSeconds  *float64 `json:"duration_seconds,omitempty"`
	VideoCount       *int     `json:"video_count,omitempty"`
	CompletionTokens *int     `json:"completion_tokens,omitempty"`
}

func videoAdminItem(v *VideoBillingReservation) VideoBillingAdminItem {
	item := VideoBillingAdminItem{ID: v.ID, UserID: v.UserID, APIKeyID: v.APIKeyID, GroupID: v.GroupID, AccountID: v.AccountID,
		EnterpriseID: v.EnterpriseID, SubscriptionID: v.SubscriptionID, Platform: v.Platform, Protocol: v.Protocol, TaskID: v.TaskID, FileID: v.FileID,
		State: v.State, HoldAmount: v.HoldAmount, ActualAmount: v.ActualAmount, Attempts: v.Attempts,
		ReviewNote: logredact.RedactText(v.LastError), CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
	var contract VideoPricingContract
	if json.Unmarshal(v.Contract, &contract) == nil {
		item.Model = contract.Model
		item.Resolution = contract.Resolution
		item.BillingMode = string(contract.Mode)
		item.EffectiveMultiplier = contract.EffectiveMultiplier
	}
	return item
}

func (s *OpenAIGatewayService) ListVideoBillingReservations(ctx context.Context, state string, page, pageSize int) (*VideoBillingAdminPage, error) {
	if !s.HasDurableVideoBilling() {
		return nil, ErrBillingServiceUnavailable
	}
	if page < 1 || page > 1000000 || pageSize < 1 || pageSize > 100 {
		return nil, ErrVideoAdminInput
	}
	if state == "" {
		state = VideoReservationManual
	}
	switch state {
	case VideoReservationPreparing, VideoReservationAccepted, VideoReservationManual, VideoReservationSettled, VideoReservationReleased:
	default:
		return nil, ErrVideoAdminInput
	}
	repo, ok := s.videoBillingRepo.(VideoBillingReservationAdminRepository)
	if !ok {
		return nil, ErrBillingServiceUnavailable
	}
	items, total, err := repo.ListVideoBillingReservations(ctx, state, page, pageSize)
	if err != nil {
		return nil, err
	}
	pageDTO := &VideoBillingAdminPage{Items: make([]VideoBillingAdminItem, 0, len(items)), Total: total, Page: page, PageSize: pageSize}
	for _, item := range items {
		pageDTO.Items = append(pageDTO.Items, videoAdminItem(item))
	}
	return pageDTO, nil
}

func (s *OpenAIGatewayService) manualVideoReservation(ctx context.Context, id string, actorID int64, reason string) (*VideoBillingReservation, error) {
	if !s.HasDurableVideoBilling() {
		return nil, ErrBillingServiceUnavailable
	}
	if actorID <= 0 || strings.TrimSpace(id) == "" || len(id) > 64 || strings.TrimSpace(reason) == "" || len([]rune(reason)) > 1000 {
		return nil, ErrVideoAdminInput
	}
	v, err := s.videoBillingRepo.Get(ctx, id)
	if errors.Is(err, ErrVideoReservationNotFound) {
		return nil, infraerrors.NotFound("VIDEO_RESERVATION_NOT_FOUND", "Video billing reservation not found.")
	}
	return v, err
}

func (s *OpenAIGatewayService) AdminReleaseVideoReservation(ctx context.Context, id string, actorID int64, reason string) (*VideoBillingAdminItem, error) {
	v, err := s.manualVideoReservation(ctx, id, actorID, reason)
	if err != nil {
		return nil, err
	}
	if v.State != VideoReservationManual && v.State != VideoReservationReleased {
		return nil, ErrVideoManualOnly
	}
	if v.State == VideoReservationManual {
		if _, err = s.videoBillingRepo.Release(ctx, id, fmt.Sprintf("manual release by administrator %d: %s", actorID, strings.TrimSpace(reason))); err != nil {
			return nil, err
		}
		s.invalidateVideoBillingCaches(ctx, v)
	}
	updated, err := s.videoBillingRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if updated.State != VideoReservationReleased {
		return nil, ErrVideoManualOnly
	}
	item := videoAdminItem(updated)
	return &item, nil
}

func (s *OpenAIGatewayService) AdminSettleVideoReservation(ctx context.Context, id string, actorID int64, input VideoBillingManualUsage) (*VideoBillingAdminItem, error) {
	v, err := s.manualVideoReservation(ctx, id, actorID, input.Reason)
	if err != nil {
		return nil, err
	}
	if v.State == VideoReservationSettled {
		item := videoAdminItem(v)
		return &item, nil
	}
	if v.State != VideoReservationManual {
		return nil, ErrVideoManualOnly
	}
	var contract VideoPricingContract
	var snapshot VideoBillingSnapshot
	if json.Unmarshal(v.Contract, &contract) != nil || json.Unmarshal(v.Snapshot, &snapshot) != nil {
		return nil, ErrBillingServiceUnavailable
	}
	actual := VideoActualUsage{VideoCount: 1}
	if input.DurationSeconds != nil {
		if !validVideoContractNumber(*input.DurationSeconds) || *input.DurationSeconds > float64(math.MaxInt32) {
			return nil, ErrVideoAdminInput
		}
		actual.DurationSecondsExact = *input.DurationSeconds
		actual.DurationSeconds = int(math.Ceil(*input.DurationSeconds))
	}
	if input.VideoCount != nil {
		if *input.VideoCount < 1 || *input.VideoCount > math.MaxInt32 {
			return nil, ErrVideoAdminInput
		}
		actual.VideoCount = *input.VideoCount
	}
	if input.CompletionTokens != nil {
		if *input.CompletionTokens < 0 || *input.CompletionTokens > math.MaxInt32 {
			return nil, ErrVideoAdminInput
		}
		actual.CompletionTokens = *input.CompletionTokens
	}
	if contract.Mode == BillingModeVideo && (input.DurationSeconds == nil || *input.DurationSeconds <= 0 || input.VideoCount == nil) {
		return nil, ErrVideoAdminInput
	}
	if contract.Mode == BillingModeToken && input.CompletionTokens == nil {
		return nil, ErrVideoAdminInput
	}
	// A missing upstream ID is not invented or bound to an account. This private
	// reservation identity is used only for an exactly-once manual money event.
	billingID := v.TaskID
	if billingID == "" {
		billingID = "reservation:" + v.ID
	}
	usage, err := contract.BuildUsageLog(StableGrokVideoBillingRequestID(billingID), actual)
	if err != nil {
		return nil, ErrVideoAdminInput.WithCause(err)
	}
	usage.UserID, usage.APIKeyID, usage.AccountID = v.UserID, v.APIKeyID, v.AccountID
	usage.GroupID = &v.GroupID
	usage.SubscriptionID = v.SubscriptionID
	usage.RequestType = RequestTypeSync
	usage.RequestedModel = snapshot.Model
	if snapshot.UpstreamModel != "" {
		usage.UpstreamModel = &snapshot.UpstreamModel
	}
	usage.AccountRateMultiplier = &snapshot.AccountRateMultiplier
	if v.SubscriptionID != nil {
		usage.BillingType = BillingTypeSubscription
	}
	ApplyUsageLogPayerIdentity(usage, v.EnterpriseID, v.EnterpriseDepartmentID)
	cmd := &UsageBillingCommand{RequestID: usage.RequestID, AccountID: v.AccountID, AccountType: snapshot.AccountType, Model: contract.Model,
		BillingType: usage.BillingType, OutputTokens: actual.CompletionTokens, RequestPayloadHash: snapshot.PayloadHash}
	if snapshot.AccountHasQuota {
		cmd.AccountQuotaCost = usage.TotalCost * snapshot.AccountRateMultiplier
	}
	settled, err := s.videoBillingRepo.Settle(ctx, &VideoReservationSettlement{ID: id, Usage: usage, Billing: cmd, ManualActorID: actorID, ManualReason: input.Reason})
	if err != nil {
		return nil, err
	}
	if settled != nil && settled.Applied {
		s.invalidateVideoBillingCaches(ctx, v)
		if s.deferredService != nil {
			s.deferredService.ScheduleLastUsedUpdate(v.AccountID)
		}
	}
	updated, err := s.videoBillingRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if updated.State != VideoReservationSettled {
		return nil, ErrVideoManualOnly
	}
	item := videoAdminItem(updated)
	return &item, nil
}
