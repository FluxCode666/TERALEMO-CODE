package admin

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// massEmailTransparentGIF 1x1 透明 GIF（43 字节）。
var massEmailTransparentGIF = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00,
	0x00, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x21, 0xf9, 0x04, 0x01, 0x00,
	0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00,
	0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3b,
}

// MassEmailHandler 管理后台「邮件群发」。
type MassEmailHandler struct {
	service     *service.MassEmailService
	userService *service.UserService
}

// NewMassEmailHandler 创建处理器。
func NewMassEmailHandler(svc *service.MassEmailService, userService *service.UserService) *MassEmailHandler {
	return &MassEmailHandler{service: svc, userService: userService}
}

// --- 请求体 ---

// MassEmailRecipientFilterRequest 收件人条件（创建/编辑/预览共用）。
type MassEmailRecipientFilterRequest struct {
	IncludeAllUsers  bool                             `json:"include_all_users"`
	UserFilter       domain.MassEmailUserFilter       `json:"user_filter"`
	EnterpriseFilter domain.MassEmailEnterpriseFilter `json:"enterprise_filter"`
	ManualEmails     []string                         `json:"manual_emails"`
}

func (r MassEmailRecipientFilterRequest) toDomain() domain.MassEmailRecipientFilter {
	return domain.MassEmailRecipientFilter{
		IncludeAllUsers:  r.IncludeAllUsers,
		UserFilter:       r.UserFilter,
		EnterpriseFilter: r.EnterpriseFilter,
		ManualEmails:     r.ManualEmails,
	}
}

// CreateMassEmailRequest 创建任务。
type CreateMassEmailRequest struct {
	Subject         string                          `json:"subject" binding:"required"`
	BodyHTML        string                          `json:"body_html" binding:"required"`
	RecipientFilter MassEmailRecipientFilterRequest `json:"recipient_filter"`
	SendNow         bool                            `json:"send_now"`
	ScheduledAt     *time.Time                      `json:"scheduled_at"`
}

// UpdateMassEmailRequest 编辑任务。
type UpdateMassEmailRequest struct {
	Subject         string                          `json:"subject" binding:"required"`
	BodyHTML        string                          `json:"body_html" binding:"required"`
	RecipientFilter MassEmailRecipientFilterRequest `json:"recipient_filter"`
	ScheduledAt     *time.Time                      `json:"scheduled_at"`
}

// PreviewMassEmailRequest 预览收件人。
type PreviewMassEmailRequest struct {
	RecipientFilter MassEmailRecipientFilterRequest `json:"recipient_filter"`
	SampleSize      int                             `json:"sample_size"`
}

// TestMassEmailRequest 测试发送。
type TestMassEmailRequest struct {
	Subject  string `json:"subject" binding:"required"`
	BodyHTML string `json:"body_html" binding:"required"`
	To       string `json:"to"`
}

// --- handlers ---

// List GET /api/v1/admin/mass-emails
func (h *MassEmailHandler) List(c *gin.Context) {
	params := paginationParamsFromQuery(c)
	filters := domain.MassEmailCampaignListFilters{
		Statuses: splitCommaSeparated(c.Query("status")),
		Search:   strings.TrimSpace(c.Query("search")),
	}
	items, result, err := h.service.List(c.Request.Context(), params, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.PaginatedWithResult(c, dto.MassEmailCampaignsFromService(items), toResponsePagination(result))
}

// Create POST /api/v1/admin/mass-emails
func (h *MassEmailHandler) Create(c *gin.Context) {
	var req CreateMassEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	campaign, err := h.service.Create(c.Request.Context(), subject.UserID, service.MassEmailCreateInput{
		Subject:         req.Subject,
		BodyHTML:        req.BodyHTML,
		RecipientFilter: req.RecipientFilter.toDomain(),
		SendNow:         req.SendNow,
		ScheduledAt:     req.ScheduledAt,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, dto.MassEmailCampaignFromService(campaign))
}

// Preview POST /api/v1/admin/mass-emails/preview-recipients
func (h *MassEmailHandler) Preview(c *gin.Context) {
	var req PreviewMassEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	preview, err := h.service.PreviewRecipients(c.Request.Context(), req.RecipientFilter.toDomain(), req.SampleSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.MassEmailPreviewFromService(preview))
}

// Test POST /api/v1/admin/mass-emails/test
func (h *MassEmailHandler) Test(c *gin.Context) {
	var req TestMassEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}

	fallbackTo := ""
	if subject, ok := middleware2.GetAuthSubjectFromContext(c); ok && h.userService != nil {
		if user, err := h.userService.GetByID(c.Request.Context(), subject.UserID); err == nil && user != nil {
			fallbackTo = user.Email
		}
	}

	to, err := h.service.TestSend(c.Request.Context(), fallbackTo, service.MassEmailTestInput{
		Subject:  req.Subject,
		BodyHTML: req.BodyHTML,
		To:       req.To,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"sent": true, "to": to})
}

// Get GET /api/v1/admin/mass-emails/:id
func (h *MassEmailHandler) Get(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	detail, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.MassEmailCampaignDetail{
		Campaign: dto.MassEmailCampaignFromService(detail.Campaign),
		Stats:    dto.MassEmailCampaignStatsFromService(detail.Stats),
	})
}

// Update PUT /api/v1/admin/mass-emails/:id
func (h *MassEmailHandler) Update(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	var req UpdateMassEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	campaign, err := h.service.Update(c.Request.Context(), id, service.MassEmailUpdateInput{
		Subject:         req.Subject,
		BodyHTML:        req.BodyHTML,
		RecipientFilter: req.RecipientFilter.toDomain(),
		ScheduledAt:     req.ScheduledAt,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.MassEmailCampaignFromService(campaign))
}

// Delete DELETE /api/v1/admin/mass-emails/:id
func (h *MassEmailHandler) Delete(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "ok"})
}

// Send POST /api/v1/admin/mass-emails/:id/send
func (h *MassEmailHandler) Send(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	campaign, err := h.service.Send(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"campaign": dto.MassEmailCampaignFromService(campaign)})
}

// Cancel POST /api/v1/admin/mass-emails/:id/cancel
func (h *MassEmailHandler) Cancel(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	campaign, err := h.service.Cancel(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"campaign": dto.MassEmailCampaignFromService(campaign)})
}

// RetryFailed POST /api/v1/admin/mass-emails/:id/retry-failed
func (h *MassEmailHandler) RetryFailed(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	reset, campaign, err := h.service.RetryFailed(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"reset":    reset,
		"campaign": dto.MassEmailCampaignFromService(campaign),
	})
}

// RetryRecipient POST /api/v1/admin/mass-emails/:id/recipients/:rid/retry
func (h *MassEmailHandler) RetryRecipient(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}
	recipientID, ok := parsePositiveID(c, "rid")
	if !ok {
		return
	}
	recipient, err := h.service.RetryRecipient(c.Request.Context(), id, recipientID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"recipient": dto.MassEmailRecipientFromService(recipient)})
}

// ListRecipients GET /api/v1/admin/mass-emails/:id/recipients
func (h *MassEmailHandler) ListRecipients(c *gin.Context) {
	id, ok := parsePositiveID(c, "id")
	if !ok {
		return
	}

	filters := domain.MassEmailRecipientListFilters{
		Statuses: splitCommaSeparated(c.Query("status")),
		Search:   strings.TrimSpace(c.Query("search")),
	}
	if raw := strings.TrimSpace(c.Query("opened")); raw != "" {
		value := raw == "1" || strings.EqualFold(raw, "true")
		filters.Opened = &value
	}

	items, result, err := h.service.ListRecipients(c.Request.Context(), id, paginationParamsFromQuery(c), filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.PaginatedWithResult(c, dto.MassEmailRecipientsFromService(items), toResponsePagination(result))
}

// Pixel GET /api/v1/mail/o/:token.gif （公开，无需鉴权）
//
// 无论 token 是否有效都返回同一张 1x1 GIF，避免通过响应差异探测 token。
func (h *MassEmailHandler) Pixel(c *gin.Context) {
	token := c.Param("token.gif")
	if token == "" {
		token = c.Param("token")
	}
	token = strings.TrimSuffix(strings.TrimSpace(token), ".gif")
	if token != "" {
		h.service.RecordOpen(c.Request.Context(), token)
	}

	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, private")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.Data(200, "image/gif", massEmailTransparentGIF)
}

// --- helpers ---

func parsePositiveID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid "+name)
		return 0, false
	}
	return id, true
}

func splitCommaSeparated(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func paginationParamsFromQuery(c *gin.Context) pagination.PaginationParams {
	page, pageSize := response.ParsePagination(c)
	return pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    c.Query("sort_by"),
		SortOrder: c.Query("sort_order"),
	}
}
