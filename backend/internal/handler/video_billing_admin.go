package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpenAIGatewayHandler) videoBillingAdminActor(c *gin.Context) (int64, bool) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Administrator authentication required")
		return 0, false
	}
	role, ok := middleware.GetUserRoleFromContext(c)
	if !ok || role != service.RoleAdmin {
		response.Forbidden(c, "Administrator access required")
		return 0, false
	}
	if h == nil || h.gatewayService == nil {
		response.ErrorFrom(c, service.ErrBillingServiceUnavailable)
		return 0, false
	}
	return subject.UserID, true
}

func (h *OpenAIGatewayHandler) ListVideoBillingReservations(c *gin.Context) {
	if _, ok := h.videoBillingAdminActor(c); !ok {
		return
	}
	middleware.SetAuditAction(c, "admin.video_billing_reservations.list")
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil {
		response.BadRequest(c, "Invalid page")
		return
	}
	pageSize, err := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil {
		response.BadRequest(c, "Invalid page_size")
		return
	}
	items, err := h.gatewayService.ListVideoBillingReservations(c.Request.Context(), c.Query("state"), page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func decodeVideoAdminBody(c *gin.Context, dst any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		response.BadRequest(c, "Invalid resolution body; only reason and actual usage fields are accepted")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		response.BadRequest(c, "Resolution body must contain one JSON object")
		return false
	}
	return true
}

func (h *OpenAIGatewayHandler) ReleaseVideoBillingReservation(c *gin.Context) {
	actorID, ok := h.videoBillingAdminActor(c)
	if !ok {
		return
	}
	middleware.SetAuditAction(c, "admin.video_billing_reservations.release")
	var body struct {
		Reason string `json:"reason"`
	}
	if !decodeVideoAdminBody(c, &body) {
		return
	}
	middleware.SetAuditExtra(c, map[string]any{"reservation_id": c.Param("reservation_id"), "resolution_reason": strings.TrimSpace(body.Reason)})
	item, err := h.gatewayService.AdminReleaseVideoReservation(c.Request.Context(), c.Param("reservation_id"), actorID, body.Reason)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *OpenAIGatewayHandler) SettleVideoBillingReservation(c *gin.Context) {
	actorID, ok := h.videoBillingAdminActor(c)
	if !ok {
		return
	}
	middleware.SetAuditAction(c, "admin.video_billing_reservations.settle")
	var body service.VideoBillingManualUsage
	if !decodeVideoAdminBody(c, &body) {
		return
	}
	middleware.SetAuditExtra(c, map[string]any{"reservation_id": c.Param("reservation_id"), "resolution_reason": strings.TrimSpace(body.Reason)})
	item, err := h.gatewayService.AdminSettleVideoReservation(c.Request.Context(), c.Param("reservation_id"), actorID, body)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}
