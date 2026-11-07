package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	pkgerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// creationStudioNormalized is one normalized inner error (contract §6.2).
type creationStudioNormalized struct {
	Status   int
	Reason   string
	Message  string
	Metadata map[string]string
}

// creationStudioErrorEnvelope is the union of the four inner error shapes:
// middleware {code,message}, OpenAI {error:{type,code,message}},
// Google {error:{code:<int>,message,status}} and MiniMax
// {base_resp:{status_code,status_msg}}.
type creationStudioErrorEnvelope struct {
	message string
	code    string
	typ     string
	// reason is the machine code of a Google envelope: either error.reason (the
	// enterprise metadata bridge) or error.details[].reason (the Google auth
	// middleware). Both are absent for ordinary local errors.
	reason   string
	google   bool
	miniMax  bool
	mmStatus int
	mmMsg    string
	metadata map[string]string
}

func parseCreationStudioErrorEnvelope(body []byte) creationStudioErrorEnvelope {
	var envelope creationStudioErrorEnvelope
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return envelope
	}
	envelope.message = strings.TrimSpace(root.Get("message").String())
	if code := root.Get("code"); code.Exists() && code.Type == gjson.String {
		envelope.code = strings.TrimSpace(code.String())
	}
	if metadata := root.Get("metadata"); metadata.IsObject() {
		envelope.metadata = make(map[string]string)
		metadata.ForEach(func(key, value gjson.Result) bool {
			if text := strings.TrimSpace(value.String()); text != "" {
				envelope.metadata[key.String()] = text
			}
			return true
		})
	}
	if inner := root.Get("error"); inner.IsObject() {
		envelope.typ = strings.TrimSpace(inner.Get("type").String())
		if code := inner.Get("code"); code.Exists() {
			if code.Type == gjson.Number {
				envelope.google = true
			} else {
				envelope.code = strings.TrimSpace(code.String())
			}
		}
		if message := strings.TrimSpace(inner.Get("message").String()); message != "" {
			envelope.message = message
		}
		if reason := strings.TrimSpace(inner.Get("reason").String()); reason != "" {
			envelope.reason = reason
		}
		if details := inner.Get("details"); details.IsArray() {
			for _, detail := range details.Array() {
				if reason := strings.TrimSpace(detail.Get("reason").String()); reason != "" {
					envelope.reason = reason
					break
				}
			}
		}
		if metadata := inner.Get("metadata"); metadata.IsObject() {
			if envelope.metadata == nil {
				envelope.metadata = make(map[string]string)
			}
			metadata.ForEach(func(key, value gjson.Result) bool {
				if text := strings.TrimSpace(value.String()); text != "" {
					envelope.metadata[key.String()] = text
				}
				return true
			})
		}
	}
	if base := root.Get("base_resp"); base.IsObject() {
		envelope.miniMax = true
		envelope.mmStatus = int(base.Get("status_code").Int())
		envelope.mmMsg = strings.TrimSpace(base.Get("status_msg").String())
	}
	return envelope
}

// creationStudioKnownReasons is the set of stable machine codes of contract
// §6.1. It exists because local gateway errors of the non-Google protocols put
// their code in the OpenAI envelope's `type` field, while upstream bodies use
// `type` for generic classes ("api_error", "invalid_request_error"). Only a code
// from this table is promoted to a reason, so a generic type never becomes one.
var creationStudioKnownReasons = func() map[string]struct{} {
	codes := []string{
		"INVALID_CREATION_REQUEST", "CREATION_MODEL_NOT_AVAILABLE", "video_pricing_not_configured",
		"insufficient_balance", "INSUFFICIENT_BALANCE", "ENTERPRISE_BALANCE_INSUFFICIENT",
		"IMAGE_GENERATION_FORBIDDEN", "GROUP_DISABLED", "GROUP_DELETED", "GROUP_NOT_ALLOWED",
		"ENTERPRISE_DISABLED", "ENTERPRISE_MEMBER_DISABLED", "ENTERPRISE_GROUP_NOT_ALLOWED",
		"API_KEY_EXPIRED", "API_KEY_DISABLED", "SUBSCRIPTION_NOT_FOUND", "ACCESS_DENIED",
		"CREATION_KEY_NOT_FOUND", "ASYNC_IMAGE_DISABLED", "CREATION_STORAGE_DISABLED",
		"CREATION_UPLOAD_NOT_FOUND", "CREATION_UPLOAD_EXPIRED", "CREATION_UPLOAD_TOO_LARGE",
		"CREATION_BODY_TOO_LARGE", "CREATION_UPLOAD_UNSUPPORTED_TYPE", "CREATION_UPLOAD_INVALID_IMAGE",
		"rate_limit_exceeded", "USAGE_LIMIT_EXCEEDED", "API_KEY_QUOTA_EXHAUSTED",
		"VIDEO_RESERVATION_QUOTA_EXCEEDED", "DEPARTMENT_QUOTA_EXCEEDED", "MEMBER_QUOTA_EXCEEDED",
		"CREATION_UPLOAD_LIMIT_EXCEEDED", "upstream_error", "CREATION_UPLOAD_STORAGE_FAILED",
		"seedance_no_eligible_account", "gemini_video_no_eligible_account",
		"minimax_video_no_eligible_account", "grok_media_no_eligible_account",
		"billing_service_error", "CREATION_ESTIMATE_UNAVAILABLE",
		"CREATION_DISPATCHER_UNAVAILABLE", "CREATION_INTERNAL_ERROR", "billing_error",
	}
	table := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		table[code] = struct{}{}
	}
	return table
}()

// creationStudioIsKnownReason reports whether value is a stable machine code.
func creationStudioIsKnownReason(value string) bool {
	if value == "" {
		return false
	}
	if _, ok := creationStudioKnownReasons[value]; ok {
		return true
	}
	return strings.HasPrefix(value, "ENTERPRISE_")
}

// creationStudioSubscriptionWindowReasons are the internal subscription window
// codes. They are not part of the §6.1 table, so they are always reported as
// USAGE_LIMIT_EXCEEDED.
var creationStudioSubscriptionWindowReasons = map[string]struct{}{
	"DAILY_LIMIT_EXCEEDED":   {},
	"WEEKLY_LIMIT_EXCEEDED":  {},
	"MONTHLY_LIMIT_EXCEEDED": {},
}

// creationStudioNormalizeReason folds internal codes that are not part of the
// §6.1 table into the reason the panel branches on.
func creationStudioNormalizeReason(reason string) string {
	if _, ok := creationStudioSubscriptionWindowReasons[reason]; ok {
		return "USAGE_LIMIT_EXCEEDED"
	}
	return reason
}

// creationStudioGoogleLocalReasons maps the exact message of a local (never
// upstream) Google-envelope error onto its stable reason. The Google envelope
// carries no machine-readable code: error.code is the HTTP status and
// error.status is the coarse Google status, so for local errors the message is
// the only discriminator. Entries are built from the gateway's own error
// constants, so a renamed message cannot silently drift out of the table.
var creationStudioGoogleLocalReasons = func() map[string]string {
	table := map[string]string{
		// apiKeyAuth Google path: balance at or below the minimum reserve.
		"Insufficient account balance": "INSUFFICIENT_BALANCE",
		// handleGrokMedia coverage stage: balance positive but below the hold.
		"Available balance does not cover the video reservation": "insufficient_balance",
		// apiKeyAuth Google path: key state.
		"API key is disabled": "API_KEY_DISABLED",
		// apiKeyAuth Google path: the middleware writes its own literals, so the key
		// quota message differs in case from the error constant.
		"API key 额度已用完":                               "API_KEY_QUOTA_EXHAUSTED",
		"API key 已过期":                                 "API_KEY_EXPIRED",
		"No active subscription found for this group": "SUBSCRIPTION_NOT_FOUND",
	}
	add := func(err error, reason string) {
		if message := strings.TrimSpace(pkgerrors.Message(err)); message != "" {
			table[message] = reason
		}
	}
	add(service.ErrInsufficientBalance, "INSUFFICIENT_BALANCE")
	add(service.ErrAPIKeyQuotaExhausted, "API_KEY_QUOTA_EXHAUSTED")
	add(service.ErrAPIKeyRateLimit5hExceeded, "rate_limit_exceeded")
	add(service.ErrAPIKeyRateLimit1dExceeded, "rate_limit_exceeded")
	add(service.ErrAPIKeyRateLimit7dExceeded, "rate_limit_exceeded")
	add(service.ErrGroupRPMExceeded, "rate_limit_exceeded")
	add(service.ErrUserRPMExceeded, "rate_limit_exceeded")
	add(service.ErrUserPlatformDailyQuotaExhausted, "rate_limit_exceeded")
	add(service.ErrUserPlatformWeeklyQuotaExhausted, "rate_limit_exceeded")
	add(service.ErrUserPlatformMonthlyQuotaExhausted, "rate_limit_exceeded")
	add(service.ErrDailyLimitExceeded, "USAGE_LIMIT_EXCEEDED")
	add(service.ErrWeeklyLimitExceeded, "USAGE_LIMIT_EXCEEDED")
	add(service.ErrMonthlyLimitExceeded, "USAGE_LIMIT_EXCEEDED")
	add(service.ErrVideoReservationQuotaExceeded, "VIDEO_RESERVATION_QUOTA_EXCEEDED")
	add(service.ErrBillingServiceUnavailable, "billing_service_error")
	return table
}()

// creationStudioGoogleLocalReason resolves the reason of a local Google-envelope
// error: exact message first, then the two dynamic local shapes (the IP ACL
// message carries the client IP, and the subscription middleware writes the
// whole ApplicationError text, which still contains reason="...").
func creationStudioGoogleLocalReason(message string) string {
	message = strings.TrimSpace(message)
	if reason, ok := creationStudioGoogleLocalReasons[message]; ok {
		return reason
	}
	if strings.HasPrefix(message, "Access denied. Your IP is ") {
		return "ACCESS_DENIED"
	}
	if match := creationStudioReasonFromErrorMessage.FindStringSubmatch(message); match != nil {
		return match[1]
	}
	return ""
}

// creationStudioReasonFromErrorMessage extracts reason="X" from the text form of
// an ApplicationError ("error: code=429 reason=\"DAILY_LIMIT_EXCEEDED\" ...").
var creationStudioReasonFromErrorMessage = regexp.MustCompile(`reason="([A-Z][A-Z0-9_]*)"`)

// creationStudioNormalizeError maps an inner gateway error onto the stable
// reasons of contract §6.2. The first matching rule wins.
func creationStudioNormalizeError(status int, header http.Header, body []byte) creationStudioNormalized {
	envelope := parseCreationStudioErrorEnvelope(body)
	normalized := creationStudioNormalized{
		Status:  status,
		Reason:  "upstream_error",
		Message: envelope.message,
	}
	if normalized.Message == "" {
		trimmed := strings.TrimSpace(string(body))
		if trimmed == "" || !json.Valid(body) {
			trimmed = ""
		}
		normalized.Message = trimmed
	}
	if normalized.Message == "" || normalized.Message == "{}" {
		normalized.Message = http.StatusText(status)
	}
	if normalized.Message == "" {
		normalized.Message = "upstream request failed"
	}
	retryAfter := ""
	if header != nil {
		retryAfter = strings.TrimSpace(header.Get("Retry-After"))
	}

	switch {
	// 1. Balance below the minimum reserve: the gateway reports it as a generic
	// billing_error. Only the creation layer renames it. The code lives in the
	// middleware `code` for the OpenAI-compatible paths and in the OpenAI `type`
	// for handleGrokMedia (Seedance / MiniMax / Grok video), so both are checked.
	case status == http.StatusForbidden &&
		(envelope.code == "billing_error" || envelope.typ == "billing_error") &&
		envelope.message == "insufficient balance":
		normalized.Reason = "INSUFFICIENT_BALANCE"
		normalized.Message = envelope.message
	// 2. The group forbids image generation.
	case status == http.StatusForbidden && envelope.code == "permission_error" && envelope.message == service.ImageGenerationPermissionMessage():
		normalized.Reason = "IMAGE_GENERATION_FORBIDDEN"
	// 3. Async image tasks are switched off.
	case status == http.StatusNotFound && envelope.message == "async image tasks are not enabled":
		normalized.Reason = "ASYNC_IMAGE_DISABLED"
	// 4. The platform does not serve the requested API at all.
	case status == http.StatusNotFound && (envelope.message == "Images API is not supported for this platform" ||
		envelope.message == "Videos API is not supported for this platform"):
		normalized.Status = http.StatusBadRequest
		normalized.Reason = "CREATION_MODEL_NOT_AVAILABLE"
	// 5. The key quota is exhausted as reported by an upstream OpenAI body.
	case status == http.StatusTooManyRequests && envelope.typ == "insufficient_quota":
		normalized.Reason = "API_KEY_QUOTA_EXHAUSTED"
	// 5b. An explicit machine code from the enterprise metadata bridge
	// (error.reason) or from the Google auth middleware (error.details[].reason).
	// Both envelopes carry it, so this is checked before the shape-specific rules.
	case envelope.reason != "":
		normalized.Reason = envelope.reason
	// 5c. Google 402 is only ever produced by the video coverage check.
	case envelope.google && status == http.StatusPaymentRequired:
		normalized.Reason = "insufficient_balance"
	// 5d. Local Google errors have no code, only their own message. Without this
	// the 403/429 billing and quota rejections degrade to INVALID_CREATION_REQUEST.
	case envelope.google && creationStudioGoogleLocalReason(envelope.message) != "":
		normalized.Reason = creationStudioGoogleLocalReason(envelope.message)
	// 6. Malformed request rejected by the gateway or the upstream.
	case status == http.StatusBadRequest && (envelope.typ == "invalid_request_error" || envelope.google):
		normalized.Reason = "INVALID_CREATION_REQUEST"
	// 7. A structured code from the middleware or OpenAI envelope wins.
	case envelope.code != "":
		normalized.Reason = envelope.code
	// 7b. Local gateway errors of the non-Google protocols carry their code in the
	// OpenAI envelope's type field (errorResponse(status, errType, message)).
	case creationStudioIsKnownReason(envelope.typ):
		normalized.Reason = envelope.typ
	// 8. MiniMax business errors are upstream failures.
	case envelope.miniMax && envelope.mmStatus != 0:
		normalized.Status = http.StatusBadGateway
		normalized.Reason = "upstream_error"
		if envelope.mmMsg != "" {
			normalized.Message = envelope.mmMsg
		}
	// 9. Any other client error is a rejected form.
	case status >= http.StatusBadRequest && status < http.StatusInternalServerError:
		normalized.Reason = "INVALID_CREATION_REQUEST"
	// 10. Any other server error: 503 stays 503, everything else becomes 502.
	default:
		if status != http.StatusServiceUnavailable {
			normalized.Status = http.StatusBadGateway
		}
		normalized.Reason = "upstream_error"
	}

	normalized.Reason = creationStudioNormalizeReason(normalized.Reason)

	metadata := map[string]string{}
	for key, value := range envelope.metadata {
		metadata[key] = value
	}
	if retryAfter != "" {
		metadata["retry_after"] = retryAfter
	}
	if len(metadata) > 0 {
		normalized.Metadata = metadata
	}
	return normalized
}

// creationStudioTableAError maps the admission (eligibility) stage errors of
// contract §5.3 table A.
func creationStudioTableAError(c *gin.Context, err error) (int, string, string, int) {
	switch {
	case err == nil:
		return http.StatusOK, "", "", 0
	case isCreationStudioInsufficientBalance(err):
		return http.StatusForbidden, "INSUFFICIENT_BALANCE", creationStudioBalanceMessage(err), 0
	case isCreationStudioUsageLimit(err):
		return http.StatusTooManyRequests, "USAGE_LIMIT_EXCEEDED", creationStudioErrorText(err), 0
	}
	return billingErrorDetailsForContext(c, err)
}

// creationStudioTableBError maps the coverage (ReserveFixedCharge) stage errors
// of contract §5.3 table B.
func creationStudioTableBError(c *gin.Context, err error) (int, string, string, int) {
	switch {
	case err == nil:
		return http.StatusOK, "", "", 0
	case isCreationStudioEnterpriseBalance(err):
		return billingErrorDetailsForContext(c, err)
	case isCreationStudioInsufficientBalance(err):
		return http.StatusPaymentRequired, "insufficient_balance", creationStudioBalanceMessage(err), 0
	case isCreationStudioUsageLimit(err):
		return http.StatusTooManyRequests, "USAGE_LIMIT_EXCEEDED", creationStudioErrorText(err), 0
	case isCreationStudioAPIKeyQuota(err):
		return http.StatusTooManyRequests, "API_KEY_QUOTA_EXHAUSTED", creationStudioErrorText(err), 0
	}
	return billingErrorDetailsForContext(c, err)
}

// isCreationStudioInsufficientBalance matches both the eligibility balance error
// and the coverage balance error: they differ only by stage.
func isCreationStudioInsufficientBalance(err error) bool {
	return errors.Is(err, service.ErrInsufficientBalance)
}

// isCreationStudioUsageLimit matches the subscription day/week/month windows.
func isCreationStudioUsageLimit(err error) bool {
	return errors.Is(err, service.ErrDailyLimitExceeded) ||
		errors.Is(err, service.ErrWeeklyLimitExceeded) ||
		errors.Is(err, service.ErrMonthlyLimitExceeded)
}

// isCreationStudioEnterpriseBalance matches the enterprise wallet error.
func isCreationStudioEnterpriseBalance(err error) bool {
	return errors.Is(err, service.ErrEnterpriseBillingBalanceInsufficient)
}

// isCreationStudioAPIKeyQuota matches an exhausted API key quota.
func isCreationStudioAPIKeyQuota(err error) bool {
	return errors.Is(err, service.ErrAPIKeyQuotaExhausted)
}

func creationStudioErrorText(err error) string {
	if text := strings.TrimSpace(pkgerrors.Message(err)); text != "" {
		return text
	}
	return "billing error"
}

func creationStudioBalanceMessage(err error) string {
	if text := strings.TrimSpace(pkgerrors.Message(err)); text != "" {
		return text
	}
	return "insufficient balance"
}

// creationStudioPrecheckJSONError writes an inner-style error body and keeps the
// Retry-After header when the caller knows it.
func creationStudioPrecheckJSONError(c *gin.Context, status int, code, message string, retryAfter int) {
	if retryAfter > 0 {
		c.Header("Retry-After", strconv.Itoa(retryAfter))
	}
	imageTaskJSONError(c, status, code, message)
}
