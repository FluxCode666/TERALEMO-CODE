package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// Use the same error for an unknown, inaccessible, stale or incompatible draft.
var ErrSeedanceDraftTaskUnavailable = errors.New("the Seedance draft task is unavailable or incompatible")

type seedanceDraftReceiptKey struct{}

type seedanceDraftReceipt struct {
	service       *OpenAIGatewayService
	payloadHash   string
	accountID     int64
	upstreamModel string
	info          GrokMediaRequestInfo
}

// PrepareSeedanceDraftSubmission authenticates a locally recorded source before
// making its non-billable status request. Its private receipt authorizes exactly
// this body/account to inherit billing inputs without sending them back to Ark.
// Ordinary submissions are returned unchanged and need no receipt.
func (s *OpenAIGatewayService) PrepareSeedanceDraftSubmission(ctx context.Context, key *APIKey, userID int64, body []byte) (context.Context, GrokMediaRequestInfo, int64, error) {
	info, err := ParseSeedanceRequest(body)
	if err != nil {
		return ctx, info, 0, err
	}
	id, _ := seedanceDraftTaskID(body)
	if id == "" {
		return ctx, info, 0, nil
	}
	fail := func() (context.Context, GrokMediaRequestInfo, int64, error) {
		return ctx, GrokMediaRequestInfo{}, 0, ErrSeedanceDraftTaskUnavailable
	}
	if s == nil || s.accountRepo == nil || key == nil || key.ID <= 0 || key.UserID != userID || key.User == nil || key.User.ID != userID || key.GroupID == nil || key.Group == nil || key.Group.ID != *key.GroupID {
		return fail()
	}
	if !key.Group.ModelAllowlist.Allows(info.Model) {
		return fail()
	}
	taskID := SeedanceTaskKey(id)
	var accountID int64
	var publicModel, upstreamModel string
	var fallbackDuration float64
	source, lookupErr := s.FindVideoTaskReservation(ctx, VideoProtocolSeedance, taskID)
	if lookupErr == nil {
		if source == nil || source.Protocol != VideoProtocolSeedance || source.TaskID != taskID || source.UserID != userID || source.APIKeyID != key.ID || source.GroupID != *key.GroupID || source.AccountID <= 0 || source.State != VideoReservationAccepted && source.State != VideoReservationSettled {
			return fail()
		}
		var snapshot VideoBillingSnapshot
		var contract VideoPricingContract
		if json.Unmarshal(source.Snapshot, &snapshot) != nil || json.Unmarshal(source.Contract, &contract) != nil || snapshot.Resolution != VideoBillingResolution480P || snapshot.VideoCount != 1 || contract.Model != snapshot.Model || contract.Resolution != VideoBillingResolution480P || contract.MaxDurationSeconds <= 0 || contract.MaxDurationSeconds > 60 || contract.MaxVideoCount != 1 {
			return fail()
		}
		accountID, publicModel, upstreamModel = source.AccountID, snapshot.Model, snapshot.UpstreamModel
		fallbackDuration = snapshot.RequestedDurationSeconds
		if fallbackDuration <= 0 {
			fallbackDuration = float64(contract.MaxDurationSeconds)
		}
	} else if errors.Is(lookupErr, ErrVideoReservationNotFound) {
		// Rolling-upgrade pending entries have no durable contract. Require both
		// the owner/key cache entry and its group-scoped, fixed-account binding.
		pending, pendingErr := s.LoadGrokVideoPendingBilling(ctx, taskID, userID, key.ID)
		if pendingErr != nil || pending == nil || NormalizeVideoBillingResolutionOrDefault(pending.VideoResolution) != VideoBillingResolution480P {
			return fail()
		}
		if pending.BillingIdentity != nil && (pending.BillingIdentity.UserID != userID || pending.BillingIdentity.GroupID != *key.GroupID) {
			return fail()
		}
		accountID, err = s.ResolveGrokMediaVideoRequestAccount(ctx, key.GroupID, taskID, userID, key.ID)
		if err != nil || accountID <= 0 {
			return fail()
		}
		publicModel, upstreamModel = pending.Model, pending.UpstreamModel
		// Old pending writers clamp positive durations to fifteen seconds. A
		// missing native duration therefore uses the conservative 2.5 ceiling.
		fallbackDuration = 30
	} else {
		return fail()
	}
	if publicModel == "" || info.Model != publicModel || upstreamModel == "" || !validVideoContractNumber(fallbackDuration) || fallbackDuration <= 0 || fallbackDuration > 60 {
		return fail()
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil || account.ID != accountID || !account.IsSchedulable() || !slices.Contains(account.GroupIDs, *key.GroupID) || !account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilitySeedance) || account.GetMappedModel(publicModel) != upstreamModel {
		return fail()
	}
	limits, err := resolveVideoAccountLimits(account, seedanceModelLimits(upstreamModel))
	if err != nil {
		return fail()
	}
	status, err := s.loadSeedanceDraftStatus(ctx, account, id)
	if err != nil || gjson.GetBytes(status, "id").String() != id || isSeedance25Model(upstreamModel) && gjson.GetBytes(status, "model").String() != upstreamModel || gjson.GetBytes(status, "status").String() != "succeeded" || gjson.GetBytes(status, "draft").Type != gjson.True || gjson.GetBytes(status, "resolution").String() != "480p" {
		return fail()
	}
	duration := seedanceResponseDurationSeconds(status)
	if duration == 0 {
		duration = fallbackDuration
	}
	if !validVideoContractNumber(duration) || duration <= 0 || duration > float64(limits.MaxDuration) || limits.DurationOverride && duration < float64(limits.MinDuration) {
		return fail()
	}
	info.DurationSecondsExact = duration
	info.DurationSeconds = int(math.Ceil(duration))
	info.Frames = 0 // Source frames inform price, never the formal submission.
	receipt := &seedanceDraftReceipt{service: s, payloadHash: HashUsageRequestPayload(body), accountID: accountID, upstreamModel: upstreamModel, info: info}
	return context.WithValue(ctx, seedanceDraftReceiptKey{}, receipt), info, accountID, nil
}

func isSeedance25Model(model string) bool {
	model = strings.ToLower(model)
	return strings.Contains(model, "seedance-2-5") || strings.Contains(model, "seedance-2.5")
}

// NormalizeSeedanceSubmissionForAccount is shared by Reserve and Forward. A
// private receipt is mandatory for inherited drafts, including direct service
// calls; ordinary normalization must never insert duration into such a body.
func (s *OpenAIGatewayService) NormalizeSeedanceSubmissionForAccount(ctx context.Context, body []byte, account *Account) ([]byte, GrokMediaRequestInfo, error) {
	info, err := ParseSeedanceRequest(body)
	if err != nil {
		return nil, info, err
	}
	id, _ := seedanceDraftTaskID(body)
	if account == nil {
		return nil, info, ErrSeedanceDraftTaskUnavailable
	}
	if id == "" {
		return NormalizeSeedanceSubmissionWithAccount(body, account)
	}
	receipt, _ := ctx.Value(seedanceDraftReceiptKey{}).(*seedanceDraftReceipt)
	if receipt == nil || receipt.service != s || receipt.payloadHash != HashUsageRequestPayload(body) || receipt.accountID != account.ID || receipt.info.Model != info.Model || receipt.upstreamModel != account.GetMappedModel(info.Model) {
		return nil, info, ErrSeedanceDraftTaskUnavailable
	}
	limits, err := resolveVideoAccountLimits(account, seedanceModelLimits(receipt.upstreamModel))
	if err != nil {
		return nil, info, err
	}
	// A final video always inherits its duration and must remain 1080p. Never
	// clamp either input or fall back to an account's default resolution.
	if limits.ResolutionOverride && !slices.Contains(limits.Resolutions, VideoBillingResolution1080P) || limits.DurationOverride && (receipt.info.DurationSecondsExact < float64(limits.MinDuration) || receipt.info.DurationSecondsExact > float64(limits.MaxDuration)) {
		return nil, info, limits.unsupported()
	}
	return body, receipt.info, nil
}

func (s *OpenAIGatewayService) loadSeedanceDraftStatus(ctx context.Context, account *Account, id string) ([]byte, error) {
	if s.httpUpstream == nil || strings.TrimSpace(account.GetCredential("api_key")) == "" {
		return nil, ErrSeedanceDraftTaskUnavailable
	}
	base, err := s.validateUpstreamBaseURL(account.VideoBaseURL())
	if err != nil {
		return nil, err
	}
	target, err := buildSeedanceURL(base, SeedanceEndpointStatus, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(WithHTTPUpstreamRedirectsDisabled(ctx), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+account.GetCredential("api_key"))
	req.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(req.Header)
	proxy := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(req, proxy, account.ID, account.Concurrency)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, ErrSeedanceDraftTaskUnavailable
	}
	body, err := ReadUpstreamResponseBody(resp.Body, s.cfg, nil, nil)
	if err != nil || !json.Valid(body) {
		return nil, ErrSeedanceDraftTaskUnavailable
	}
	return body, nil
}
