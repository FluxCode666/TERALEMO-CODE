package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	pkgerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

var errVideoUpstreamRequestNotSent = errors.New("upstream request not sent")

func videoBillingProtocol(endpoint service.GrokMediaEndpoint) string {
	if endpoint.IsSeedance() {
		return service.VideoProtocolSeedance
	}
	if endpoint.IsGeminiVideo() {
		return service.VideoProtocolGemini
	}
	if endpoint.IsMiniMaxVideo() {
		return service.VideoProtocolMiniMax
	}
	return "grok"
}

// Creation responses are held until the upstream task ID is durably attached.
// HTTP concurrency slots can be released independently from monetary holds.
func (h *OpenAIGatewayHandler) forwardReservedVideo(c *gin.Context, ctx context.Context, key *service.APIKey, sub *service.UserSubscription, account *service.Account, endpoint service.GrokMediaEndpoint, taskID string, body []byte, contentType string, info service.GrokMediaRequestInfo) (result *service.OpenAIForwardResult, err error) {
	if endpoint.IsSeedance() {
		body, info, err = h.gatewayService.NormalizeSeedanceSubmissionForAccount(ctx, body, account)
		if err != nil {
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			return nil, err
		}
	}
	maximum := info.DurationSeconds
	if !endpoint.IsSeedance() && !endpoint.IsGeminiVideo() && !endpoint.IsMiniMaxVideo() && !gjson.GetBytes(body, "duration").Exists() {
		maximum = 15
	}
	if endpoint.IsSeedance() {
		maximum = service.SeedanceReservationDurationForAccount(info, account)
	}
	if maximum <= 0 {
		maximum = 15
		if endpoint.IsGeminiVideo() {
			maximum = 8
		}
	}
	exact := info.DurationSecondsExact
	if exact == 0 {
		exact = float64(info.DurationSeconds)
	}
	inbound := GetInboundEndpoint(c)
	upstream := inbound
	switch {
	case endpoint.IsSeedance():
		upstream = EndpointSeedanceTasks
	case endpoint.IsGeminiVideo():
		upstream = EndpointGeminiVideoCreate
	case endpoint.IsMiniMaxVideo():
		upstream = EndpointMiniMaxVideoCreate
		if strings.Contains(strings.TrimRight(account.VideoBaseURL(), "/"), "/v2") {
			upstream = EndpointMiniMaxV2VideoCreate
		}
	}
	reservation, reserveErr := h.gatewayService.ReserveVideoTask(ctx, service.ReserveVideoTaskInput{
		APIKey: key, Subscription: sub, Account: account, Endpoint: endpoint, Protocol: videoBillingProtocol(endpoint), Model: info.Model, Resolution: info.Resolution,
		DurationSeconds: exact, Frames: info.Frames, MaxDurationSeconds: maximum, VideoCount: max(1, info.N),
		InboundEndpoint: inbound, UpstreamEndpoint: upstream, UserAgent: c.GetHeader("User-Agent"), IPAddress: ip.GetClientIP(c), SessionID: service.ExtractClientSessionID(c),
		PayloadHash: service.HashUsageRequestPayload(body), QuotaPlatform: service.QuotaPlatform(ctx, key),
	})
	if reserveErr != nil {
		return nil, reserveErr
	}
	ctx, writes := service.WithVideoSubmissionWrites(ctx)
	original := c.Writer
	buffer := &videoSubmissionWriter{ResponseWriter: original, header: make(http.Header), status: http.StatusOK, size: -1}
	c.Writer = buffer
	accepted := false
	confirmedRejection := false
	defer func() {
		c.Writer = original
		if !accepted {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			notSent := writes.DefinitelyNotSent()
			upstreamBody, complete := writes.RejectionBody()
			if confirmedRejection || (complete && videoExplicitUpstreamRejection(endpoint, upstreamBody)) || notSent {
				reason := "upstream explicitly rejected creation"
				if notSent {
					reason = "upstream request not sent"
				}
				if releaseErr := h.gatewayService.ReleaseVideoTask(cleanup, reservation, reason); releaseErr != nil {
					err = fmt.Errorf("release video reservation: %w", releaseErr)
				} else if notSent && err != nil {
					err = fmt.Errorf("%w: %w", errVideoUpstreamRequestNotSent, err)
				}
			} else {
				_ = h.gatewayService.MarkVideoTaskManual(cleanup, reservation, "creation outcome uncertain; hold retained")
			}
		}
	}()
	switch {
	case endpoint.IsSeedance():
		result, err = h.gatewayService.ForwardSeedance(ctx, c, account, endpoint, taskID, body)
	case endpoint.IsGeminiVideo():
		result, err = h.gatewayService.ForwardGeminiVideo(ctx, c, account, endpoint, taskID, body)
	case endpoint.IsMiniMaxVideo():
		result, err = h.gatewayService.ForwardMiniMaxVideo(ctx, c, account, endpoint, taskID, body)
	default:
		result, err = h.gatewayService.ForwardGrokMedia(ctx, c, account, endpoint, taskID, body, contentType)
	}
	if err != nil {
		var rejected *service.UpstreamFailoverError
		if errors.As(err, &rejected) {
			confirmedRejection = rejected.IsCredentialFailure()
			if rejected.StatusCode >= 400 && !buffer.Written() {
				if retry := rejected.ResponseHeaders.Get("Retry-After"); retry != "" {
					c.Header("Retry-After", retry)
				}
				message := "Upstream rejected video creation"
				if !writes.ReceivedResponse() {
					message = "Video submission failed; an uncertain submission retains its reservation for review"
					if writes.DefinitelyNotSent() {
						message = "Upstream request was not sent; the video reservation has been released"
					}
				}
				h.errorResponse(c, rejected.StatusCode, "upstream_error", message)
			}
		}
		if buffer.status >= 400 && buffer.Written() {
			buffer.commit(original)
		}
		return nil, err
	}
	if result == nil || strings.TrimSpace(result.ResponseID) == "" {
		return nil, fmt.Errorf("video creation omitted task ID")
	}
	persist, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer persistCancel()
	if err = h.gatewayService.AcceptVideoTask(persist, reservation, result.ResponseID); err != nil {
		return nil, err
	}
	accepted = true
	buffer.commit(original)
	return result, nil
}

type videoSubmissionWriter struct {
	gin.ResponseWriter
	header       http.Header
	body         bytes.Buffer
	status, size int
}

func (w *videoSubmissionWriter) Header() http.Header { return w.header }
func (w *videoSubmissionWriter) WriteHeader(code int) {
	if w.size < 0 {
		w.status = code
	}
}
func (w *videoSubmissionWriter) WriteHeaderNow() {
	if w.size < 0 {
		w.size = 0
	}
}
func (w *videoSubmissionWriter) Write(b []byte) (int, error) {
	w.WriteHeaderNow()
	n, err := w.body.Write(b)
	w.size += n
	return n, err
}
func (w *videoSubmissionWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *videoSubmissionWriter) Status() int                       { return w.status }
func (w *videoSubmissionWriter) Size() int                         { return w.size }
func (w *videoSubmissionWriter) Written() bool                     { return w.size >= 0 }
func (w *videoSubmissionWriter) Flush()                            { w.WriteHeaderNow() }
func (w *videoSubmissionWriter) commit(dst gin.ResponseWriter) {
	for k, values := range w.header {
		dst.Header()[k] = values
	}
	dst.WriteHeader(w.status)
	_, _ = dst.Write(w.body.Bytes())
}

func videoSubmissionErrorDetails(c *gin.Context, err error) (int, string, string, int) {
	if errors.Is(err, errVideoUpstreamRequestNotSent) {
		return http.StatusBadGateway, "upstream_error", "Upstream request was not sent; the video reservation has been released", 0
	}
	if errors.Is(err, service.ErrInsufficientBalance) {
		return http.StatusPaymentRequired, "insufficient_balance", "Available balance does not cover the video reservation", 0
	}
	if errors.Is(err, service.ErrVideoReservationQuotaExceeded) {
		return http.StatusTooManyRequests, "VIDEO_RESERVATION_QUOTA_EXCEEDED", service.ErrVideoReservationQuotaExceeded.Message, 0
	}
	if errors.Is(err, service.ErrModelPricingUnavailable) {
		return http.StatusBadRequest, "video_pricing_not_configured", "Configure a bounded video price before submitting this task", 0
	}
	if app := pkgerrors.FromError(err); app != nil && app.Code >= 400 && app.Code < 500 {
		return int(app.Code), app.Reason, app.Message, 0
	}
	if errors.Is(err, service.ErrBillingServiceUnavailable) {
		return billingErrorDetailsForContext(c, err)
	}
	return http.StatusBadGateway, "upstream_error", "Video submission failed; an uncertain submission retains its reservation for review", 0
}

// A completed native HTTP rejection is independent of HTTP response forwarding.
// Transport failures after a possible write and malformed 2xx remain uncertain; a complete
// JSON rejection without any task identity is a confirmed unaccepted request.
func videoExplicitUpstreamRejection(endpoint service.GrokMediaEndpoint, body []byte) bool {
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return false
	}
	if !videoErrorBodyHasNoTaskID(endpoint, body) {
		return false
	}
	if gjson.GetBytes(body, "error").IsObject() {
		return true
	}
	code := gjson.GetBytes(body, "base_resp.status_code")
	return endpoint.IsMiniMaxVideo() && code.Type == gjson.Number && code.Float() != 0
}

func videoErrorBodyHasNoTaskID(endpoint service.GrokMediaEndpoint, body []byte) bool {
	var fields []string
	switch {
	case endpoint.IsSeedance():
		fields = []string{"id"}
	case endpoint.IsGeminiVideo():
		fields = []string{"name"}
	case endpoint.IsMiniMaxVideo():
		fields = []string{"task_id", "task.id"}
	default:
		fields = []string{"request_id", "id"}
	}
	for _, field := range fields {
		if value := gjson.GetBytes(body, field); value.Exists() && strings.TrimSpace(value.String()) != "" {
			return false
		}
	}
	return true
}
