package handler

import (
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// creationStudioImagePrecheck is the hard admission gate of the creation studio
// image path (contract §5.3). It runs inside the replayed AsyncImageHandler.Submit
// call, after the security audit and before the task is created, and only when
// the request carries the in-process replay marker. Direct API users of
// /v1/images/*/async never reach it, so their behaviour is unchanged.
//
// Three stages:
//  1. eligibility — CheckBillingEligibility (same call the executor makes later),
//  2. amount — EstimateCreationImageCost, the very function /estimate uses,
//  3. coverage — ReserveFixedCharge for that amount, released immediately.
//
// It is only an admission check: the executor still performs its own eligibility
// check and in-flight reservation, and their verdict wins under concurrency.
func (h *AsyncImageHandler) creationStudioImagePrecheck(c *gin.Context, apiKey *service.APIKey, platform string, body []byte) bool {
	if h == nil || h.openAI == nil || apiKey == nil || apiKey.User == nil {
		return true
	}
	billing := h.openAI.billingCacheService
	gateway := h.openAI.gatewayService
	if billing == nil || gateway == nil {
		return true
	}
	ctx := c.Request.Context()
	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	if err := billing.CheckBillingEligibility(ctx, apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(ctx, apiKey)); err != nil {
		status, code, message, retryAfter := creationStudioTableAError(c, err)
		creationStudioPrecheckJSONError(c, status, code, message, retryAfter)
		return false
	}

	model, size, count := creationStudioImageRequestShape(gateway, c, platform, body)
	estimate := gateway.EstimateCreationImageCost(ctx, apiKey, apiKey.User.ID, model, size, count, len(body))
	if !estimate.Priced {
		if billing.InflightReservationFailClosedOnUnpriced() {
			creationStudioPrecheckJSONError(c, 402, "insufficient_balance", "image cost could not be estimated", 0)
			return false
		}
		return true
	}
	if estimate.Amount == 0 {
		// An explicitly free model has nothing to reserve: admitting it must not
		// depend on the wallet at all (contract §4.2.1: a resolved price of 0 is
		// "priced", not "unpriced").
		return true
	}
	if estimate.Amount < 0 {
		// Defensive: the estimator already normalizes negatives to unpriced.
		return true
	}

	reservation, err := billing.ReserveFixedCharge(ctx, apiKey.User, apiKey, apiKey.Group, subscription, estimate.Amount)
	if reservation != nil {
		// Admission only: the hold must never outlive this check, whatever the
		// outcome. Release is nil-safe and idempotent.
		defer reservation.Release()
	}
	if err != nil {
		status, code, message, retryAfter := creationStudioTableBError(c, err)
		creationStudioPrecheckJSONError(c, status, code, message, retryAfter)
		return false
	}
	return true
}

// creationStudioImageRequestShape reads the same model/size/n the executor will
// bill with: ParseGrokMediaRequest for Grok, ParseOpenAIImagesRequest (corrected
// by the resolved upstream model) for OpenAI.
func creationStudioImageRequestShape(gateway *service.OpenAIGatewayService, c *gin.Context, platform string, body []byte) (model, size string, count int) {
	if platform == service.PlatformGrok {
		parsed := service.ParseGrokMediaRequest(c.GetHeader("Content-Type"), body)
		return parsed.Model, parsed.Size, parsed.N
	}
	parsed, err := gateway.ParseOpenAIImagesRequest(c, body)
	if err != nil {
		return "", "", 0
	}
	model = parsed.Model
	if resolved, ok := service.ResolvedUpstreamModelFromContext(c.Request.Context()); ok {
		model = resolved
	}
	return model, parsed.Size, parsed.N
}
