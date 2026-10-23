package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// VideoBillingSnapshot intentionally excludes user passwords, API key material,
// account credentials and request prompts. Reconciliation needs only immutable
// payment/routing identity, not the currently authenticated key or membership.
type VideoBillingSnapshot struct {
	UpstreamOrigin           string  `json:"upstream_origin,omitempty"`
	Model                    string  `json:"model"`
	UpstreamModel            string  `json:"upstream_model"`
	Resolution               string  `json:"resolution"`
	RequestedDurationSeconds float64 `json:"requested_duration_seconds"`
	RequestedFrames          int     `json:"requested_frames,omitempty"`
	VideoCount               int     `json:"video_count"`
	AccountType              string  `json:"account_type"`
	AccountRateMultiplier    float64 `json:"account_rate_multiplier"`
	AccountHasQuota          bool    `json:"account_has_quota"`
	UserAgent                string  `json:"user_agent,omitempty"`
	IPAddress                string  `json:"ip_address,omitempty"`
	SessionID                string  `json:"session_id,omitempty"`
	PayloadHash              string  `json:"payload_hash,omitempty"`
}

type ReserveVideoTaskInput struct {
	APIKey             *APIKey
	Subscription       *UserSubscription
	Account            *Account
	Endpoint           GrokMediaEndpoint
	Protocol           string
	Model              string
	Resolution         string
	DurationSeconds    float64
	Frames             int
	MaxDurationSeconds int
	VideoCount         int
	InboundEndpoint    string
	UpstreamEndpoint   string
	UserAgent          string
	IPAddress          string
	SessionID          string
	PayloadHash        string
	QuotaPlatform      string
}

// SetVideoBillingReservationRepository is called during application assembly.
// A missing durable store fails new submissions closed; legacy task completion
// remains supported separately during rolling upgrades.
func (s *OpenAIGatewayService) SetVideoBillingReservationRepository(repo VideoBillingReservationRepository) {
	s.videoBillingRepo = repo
}

func (s *OpenAIGatewayService) HasDurableVideoBilling() bool {
	return s != nil && s.videoBillingRepo != nil
}

func (s *OpenAIGatewayService) ReserveVideoTask(ctx context.Context, input ReserveVideoTaskInput) (*VideoBillingReservation, error) {
	if !s.HasDurableVideoBilling() {
		return nil, ErrBillingServiceUnavailable
	}
	key := input.APIKey
	if key == nil || key.User == nil || key.Group == nil || input.Account == nil {
		return nil, ErrVideoReservationInvalid
	}
	origin, err := videoTaskUpstreamOrigin(input.Account, input.Protocol)
	if err != nil {
		return nil, err
	}
	count := max(1, input.VideoCount)
	maxTokens, err := ConservativeVideoCompletionTokenBound(input.MaxDurationSeconds, count)
	if err != nil {
		return nil, err
	}
	contract, err := s.BuildVideoPricingContract(ctx, VideoPricingContractInput{
		APIKey: key, User: key.User, Model: input.Model, Resolution: input.Resolution,
		MaxDurationSeconds: input.MaxDurationSeconds, MaxVideoCount: count, MaxCompletionTokens: maxTokens,
		RequestedDurationSeconds:  math.Max(0, input.DurationSeconds),
		RequireExplicitVideoPrice: input.Endpoint.IsGeminiVideo() || input.Endpoint.IsMiniMaxVideo(),
		AllowTokenFallback:        input.Endpoint.IsSeedance() || legacyGroupTokenOverride(key, input.Model), PricingAt: time.Now(),
		UpperBoundBasis: "protocol duration/count maximum; token fallback <=4096x4096 pixels at 60fps /1024",
		InboundEndpoint: input.InboundEndpoint, UpstreamEndpoint: input.UpstreamEndpoint,
	})
	if err != nil {
		return nil, err
	}
	if s.cfg != nil && s.cfg.RunMode == "simple" {
		contract.EffectiveMultiplier = 0
		contract.ReservedAmount = 0
	}
	snapshot := VideoBillingSnapshot{
		UpstreamOrigin: origin,
		Model:          input.Model, UpstreamModel: input.Account.GetMappedModel(input.Model), Resolution: contract.Resolution,
		RequestedDurationSeconds: input.DurationSeconds, RequestedFrames: input.Frames, VideoCount: count,
		AccountType: input.Account.Type, AccountRateMultiplier: input.Account.BillingRateMultiplier(),
		AccountHasQuota: input.Account.HasAnyQuotaLimit(), UserAgent: input.UserAgent, IPAddress: input.IPAddress,
		SessionID: input.SessionID, PayloadHash: input.PayloadHash,
	}
	pricingJSON, err := json.Marshal(contract)
	if err != nil {
		return nil, err
	}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	v := &VideoBillingReservation{ID: generateRequestID(), UserID: key.UserID, APIKeyID: key.ID, GroupID: key.Group.ID,
		Platform: input.QuotaPlatform, Protocol: input.Protocol, Endpoint: string(input.Endpoint), AccountID: input.Account.ID,
		HoldAmount: contract.ReservedAmount, Contract: pricingJSON, Snapshot: snapshotJSON}
	if v.Platform == "" {
		v.Platform = key.Group.Platform
	}
	if e := key.User.Enterprise; e != nil {
		v.EnterpriseID = e.EnterpriseID
		v.EnterpriseMemberID = e.MemberID
		v.EnterpriseDepartmentID = valueOrZero(e.DepartmentID)
		v.EnterpriseDepartmentPathIDs = FrozenEnterpriseDepartmentIDs(e.DepartmentPathIDs, v.EnterpriseDepartmentID)
	}
	// Ordinary gateway requests use an active subscription before either
	// personal or enterprise wallets. Enterprise identity still labels usage.
	if input.Subscription != nil && key.Group.IsSubscriptionType() {
		id := input.Subscription.ID
		v.SubscriptionID = &id
	}
	// Absorb ordinary requests not flushed before this scope switches to the
	// durable SQL quota ledger. The repository merges matching-window floors.
	if b := s.billingCacheService; b != nil && b.cache != nil && v.SubscriptionID == nil {
		floor, _, cacheErr := b.cache.GetUserPlatformQuotaCache(ctx, v.UserID, v.Platform)
		if cacheErr == nil {
			v.QuotaUsageFloor = floor
		}
	}
	if err := s.videoBillingRepo.Reserve(ctx, v); err != nil {
		return nil, err
	}
	s.invalidateVideoBillingCaches(ctx, v)
	return v, nil
}

func (s *OpenAIGatewayService) AcceptVideoTask(ctx context.Context, v *VideoBillingReservation, taskID string) error {
	if v == nil || !s.HasDurableVideoBilling() {
		return ErrVideoReservationInvalid
	}
	if err := s.videoBillingRepo.Accept(ctx, v.ID, taskID, v.AccountID); err != nil {
		return err
	}
	v.TaskID = taskID
	v.State = VideoReservationAccepted
	return nil
}

func (s *OpenAIGatewayService) ReleaseVideoTask(ctx context.Context, v *VideoBillingReservation, reason string) error {
	if v == nil || !s.HasDurableVideoBilling() {
		return nil
	}
	if _, err := s.videoBillingRepo.Release(ctx, v.ID, reason); err != nil {
		return err
	}
	s.invalidateVideoBillingCaches(ctx, v)
	return nil
}

func (s *OpenAIGatewayService) MarkVideoTaskManual(ctx context.Context, v *VideoBillingReservation, reason string) error {
	if v == nil || !s.HasDurableVideoBilling() {
		return nil
	}
	slog.Error("video_billing.manual_review_required", "reservation_id", v.ID, "account_id", v.AccountID, "task_id", v.TaskID, "hold", v.HoldAmount, "reason", reason)
	return s.videoBillingRepo.MarkManual(ctx, v.ID, reason)
}

func (s *OpenAIGatewayService) FindVideoTaskReservation(ctx context.Context, protocol, taskID string) (*VideoBillingReservation, error) {
	if !s.HasDurableVideoBilling() {
		return nil, ErrVideoReservationNotFound
	}
	if strings.HasPrefix(taskID, "minimax-file:") {
		return s.videoBillingRepo.GetByFile(ctx, protocol, strings.TrimPrefix(taskID, "minimax-file:"))
	}
	return s.videoBillingRepo.GetByTask(ctx, protocol, taskID)
}

// ObserveVideoTaskBilling runs for every native/panel/download observation.
// Terminal settlement is one SQL transaction, independent of Redis claims.
func (s *OpenAIGatewayService) ObserveVideoTaskBilling(ctx context.Context, protocol, taskID string, result *OpenAIForwardResult, cancelled bool) (bool, error) {
	v, err := s.FindVideoTaskReservation(ctx, protocol, taskID)
	if errors.Is(err, ErrVideoReservationNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if result == nil {
		return true, nil
	}
	return true, s.settleVideoObservation(ctx, v, result, cancelled)
}

func (s *OpenAIGatewayService) settleVideoObservation(ctx context.Context, v *VideoBillingReservation, result *OpenAIForwardResult, cancelled bool) error {
	if v.State == VideoReservationReleased || v.State == VideoReservationSettled {
		return nil
	}
	if result.VideoFileID != "" {
		if err := s.videoBillingRepo.BindFile(ctx, v.ID, result.VideoFileID); err != nil {
			return err
		}
	}
	// The task directory is a view; failure here must never undo a durable
	// payment. Background observations update it without requiring a live Key.
	if store, ok := s.cache.(ImageTaskStore); ok {
		groupID := v.GroupID
		key := &APIKey{ID: v.APIKeyID, UserID: v.UserID, GroupID: &groupID}
		if err := NewImageTaskService(store).ObserveVideoTask(ctx, key, v.TaskID, "", v.Protocol, result, cancelled); err != nil {
			slog.Warn("video_billing.catalog_update_failed", "reservation_id", v.ID, "error", err)
		}
	}
	status := strings.ToLower(result.AsyncTaskStatus)
	if cancelled || status == "failed" || status == "cancelled" || status == "canceled" || status == "expired" {
		return s.ReleaseVideoTask(ctx, v, "observed "+status)
	}
	if status != "completed" && result.VideoCount <= 0 {
		return nil
	}
	var contract VideoPricingContract
	var snapshot VideoBillingSnapshot
	if err := json.Unmarshal(v.Contract, &contract); err != nil {
		return err
	}
	if err := json.Unmarshal(v.Snapshot, &snapshot); err != nil {
		return err
	}
	duration := result.VideoDurationSeconds
	exact := result.VideoDurationSecondsExact
	if exact <= 0 {
		exact = float64(duration)
	}
	if exact <= 0 {
		exact = snapshot.RequestedDurationSeconds
	}
	if snapshot.RequestedFrames > 0 && duration <= 0 && result.VideoDurationSecondsExact <= 0 {
		exact = float64(snapshot.RequestedFrames) / 24
	}
	if exact <= 0 && contract.Mode == BillingModeVideo {
		return fmt.Errorf("successful video task omitted actual dynamic duration")
	}
	if exact < 0 {
		exact = 0
	} // token/per-request charges do not require duration.
	actualCount := result.VideoCount
	if actualCount <= 0 {
		actualCount = max(1, snapshot.VideoCount)
	}
	actual := VideoActualUsage{VideoCount: actualCount, DurationSeconds: int(math.Ceil(exact)), DurationSecondsExact: exact, CompletionTokens: result.Usage.OutputTokens}
	usage, err := contract.BuildUsageLog(StableGrokVideoBillingRequestID(v.TaskID), actual)
	if err != nil {
		return err
	}
	usage.UserID = v.UserID
	usage.APIKeyID = v.APIKeyID
	usage.AccountID = v.AccountID
	groupID := v.GroupID
	usage.GroupID = &groupID
	usage.SubscriptionID = v.SubscriptionID
	usage.RequestedModel = snapshot.Model
	if snapshot.UpstreamModel != "" {
		usage.UpstreamModel = &snapshot.UpstreamModel
	}
	usage.CreatedAt = time.Now()
	usage.RequestType = RequestTypeSync
	ApplyUsageLogPayerIdentity(usage, v.EnterpriseID, v.EnterpriseDepartmentID)
	usage.AccountRateMultiplier = &snapshot.AccountRateMultiplier
	if v.SubscriptionID != nil {
		usage.BillingType = BillingTypeSubscription
	}
	durationMS := int(time.Since(v.CreatedAt).Milliseconds())
	if durationMS > 0 {
		usage.DurationMs = &durationMS
	}
	if snapshot.UserAgent != "" {
		usage.UserAgent = &snapshot.UserAgent
	}
	if snapshot.IPAddress != "" {
		usage.IPAddress = &snapshot.IPAddress
	}
	if snapshot.SessionID != "" {
		usage.SessionID = &snapshot.SessionID
	}
	cmd := &UsageBillingCommand{RequestID: usage.RequestID, APIKeyID: v.APIKeyID, UserID: v.UserID, AccountID: v.AccountID, AccountType: snapshot.AccountType,
		Model: contract.Model, BillingType: usage.BillingType, OutputTokens: actual.CompletionTokens, RequestPayloadHash: snapshot.PayloadHash}
	if snapshot.AccountHasQuota {
		cmd.AccountQuotaCost = usage.TotalCost * snapshot.AccountRateMultiplier
	}
	settled, err := s.videoBillingRepo.Settle(ctx, &VideoReservationSettlement{ID: v.ID, Usage: usage, Billing: cmd})
	if err != nil {
		return err
	}
	if settled != nil && settled.Applied {
		s.invalidateVideoBillingCaches(ctx, v)
		if s.deferredService != nil {
			s.deferredService.ScheduleLastUsedUpdate(v.AccountID)
		}
		slog.Info("video_billing.settled", "reservation_id", v.ID, "task_id", v.TaskID, "actual_cost", settled.ActualAmount, "hold", v.HoldAmount, "inbound_endpoint", contract.InboundEndpoint, "upstream_endpoint", contract.UpstreamEndpoint)
	}
	return nil
}

func (s *OpenAIGatewayService) invalidateVideoBillingCaches(ctx context.Context, v *VideoBillingReservation) {
	b := s.billingCacheService
	if b == nil {
		return
	}
	detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := b.InvalidateUserBalance(detached, v.UserID); err != nil {
		slog.Warn("video_billing.balance_cache_invalidation_failed", "error", err)
	}
	if v.SubscriptionID != nil {
		_ = b.InvalidateSubscription(detached, v.UserID, v.GroupID)
	}
	_ = b.InvalidateAPIKeyRateLimit(detached, v.APIKeyID)
	// Platform usage for scopes containing durable video tasks is read directly
	// from SQL. Never increment/delete a dirty Redis snapshot after SQL capture:
	// a concurrent cache miss would otherwise double count the same payment.
}

type videoBillingReconciler struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *OpenAIGatewayService) StartVideoBillingReconciler() {
	if !s.HasDurableVideoBilling() || s.videoBillingWorker != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &videoBillingReconciler{cancel: cancel, done: make(chan struct{})}
	s.videoBillingWorker = w
	owner := generateRequestID()
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			if err := s.ReconcileVideoBillingOnce(ctx, owner); err != nil && ctx.Err() == nil {
				slog.Error("video_billing.reconcile_failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
func (s *OpenAIGatewayService) StopVideoBillingReconciler() {
	if s == nil || s.videoBillingWorker == nil {
		return
	}
	s.videoBillingWorker.cancel()
	<-s.videoBillingWorker.done
}

func (s *OpenAIGatewayService) ReconcileVideoBillingOnce(ctx context.Context, owner string) error {
	if !s.HasDurableVideoBilling() {
		return nil
	}
	tasks, err := s.videoBillingRepo.ClaimDue(ctx, owner, 4, 3*time.Minute)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	for _, task := range tasks {
		task := task
		wg.Add(1)
		go func() { defer wg.Done(); s.reconcileVideoTask(ctx, owner, task) }()
	}
	wg.Wait()
	return nil
}
func (s *OpenAIGatewayService) reconcileVideoTask(ctx context.Context, owner string, v *VideoBillingReservation) {
	if time.Since(v.CreatedAt) > 7*24*time.Hour {
		_ = s.MarkVideoTaskManual(ctx, v, "no confirmed terminal state within 7 days; hold retained")
		return
	}
	if v.TaskID == "" && time.Since(v.CreatedAt) < 3*time.Minute {
		_ = s.videoBillingRepo.Reschedule(ctx, v.ID, owner, v.CreatedAt.Add(3*time.Minute), "submission still in progress")
		return
	}
	if v.TaskID == "" {
		_ = s.MarkVideoTaskManual(ctx, v, "submission outcome unknown; upstream task ID was not durably recorded; hold retained")
		return
	}
	pollCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := s.PollReservedVideoTask(pollCtx, v)
	if err == nil {
		err = s.settleVideoObservation(pollCtx, v, result, false)
	}
	delay := 30 * time.Second * time.Duration(1<<min(max(v.Attempts-1, 0), 6))
	if delay > 30*time.Minute {
		delay = 30 * time.Minute
	}
	detail := ""
	if err != nil {
		detail = "upstream observation or settlement failed"
		slog.Warn("video_billing.reconcile_retry", "reservation_id", v.ID, "task_id", v.TaskID, "attempt", v.Attempts, "error", safeVideoReconcileError(err))
	}
	retryCtx, retryCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer retryCancel()
	if rescheduleErr := s.videoBillingRepo.Reschedule(retryCtx, v.ID, owner, time.Now().Add(delay), detail); rescheduleErr != nil && !errors.Is(rescheduleErr, ErrVideoReservationConflict) {
		slog.Error("video_billing.reschedule_failed", "reservation_id", v.ID, "error", rescheduleErr)
	}
}

// PollReservedVideoTask bypasses mutable API-key/group selection and uses the
// original account. Deleted keys, changed memberships and callbacks therefore
// cannot erase an already accepted payment obligation.
func (s *OpenAIGatewayService) PollReservedVideoTask(ctx context.Context, v *VideoBillingReservation) (*OpenAIForwardResult, error) {
	if s.accountRepo == nil {
		return nil, ErrBillingServiceUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, v.AccountID)
	if err != nil {
		return nil, err
	}
	if err := s.checkVideoPollingOrigin(ctx, v, account); err != nil {
		return nil, err
	}
	ctx = WithHTTPUpstreamRedirectsDisabled(ctx)
	sink := &videoStatusSink{header: make(http.Header)}
	c, _ := gin.CreateTestContext(sink)
	c.Request, _ = http.NewRequestWithContext(ctx, http.MethodGet, "http://video-reconciler.invalid/", nil)
	switch v.Protocol {
	case VideoProtocolSeedance:
		return s.ForwardSeedance(ctx, c, account, SeedanceEndpointStatus, v.TaskID, nil)
	case VideoProtocolGemini:
		return s.ForwardGeminiVideo(ctx, c, account, GeminiVideoEndpointStatus, v.TaskID, nil)
	case VideoProtocolMiniMax:
		return s.ForwardMiniMaxVideo(ctx, c, account, MiniMaxVideoEndpointStatus, v.TaskID, nil)
	default:
		return s.ForwardGrokMedia(ctx, c, account, GrokMediaEndpointVideoStatus, v.TaskID, nil, "application/json")
	}
}

type videoStatusSink struct {
	header http.Header
	status int
}

func (w *videoStatusSink) Header() http.Header         { return w.header }
func (w *videoStatusSink) Write(p []byte) (int, error) { return len(p), nil }
func (w *videoStatusSink) WriteHeader(status int)      { w.status = status }

func usesDurableVideoQuota(ctx context.Context, repo UserPlatformQuotaRepository, userID int64, platform string) (bool, error) {
	if scopes, ok := repo.(VideoBillingQuotaScopeRepository); ok {
		return scopes.HasVideoBillingScope(ctx, userID, platform)
	}
	return false, nil
}
