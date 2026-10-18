package admin

import (
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type enterpriseImportRequest struct {
	Rows []service.EnterpriseImportRow `json:"rows" binding:"required,max=1000"`
}

func (h *EnterpriseHandler) importPreview(c *gin.Context, a service.EnterpriseActor, id int64) {
	var request enterpriseImportRequest
	if !enterpriseBody(c, &request) {
		return
	}
	item, err := h.management.PreviewImport(c.Request.Context(), a, id, request.Rows)
	enterpriseSuccess(c, item, err)
}

func (h *EnterpriseHandler) importCommit(c *gin.Context, a service.EnterpriseActor, id int64) {
	var request enterpriseImportRequest
	if !enterpriseBody(c, &request) {
		return
	}
	item, err := h.management.CommitImport(c.Request.Context(), a, id, request.Rows)
	enterpriseSuccess(c, item, err)
}

func (h *EnterpriseHandler) resetMemberPassword(c *gin.Context, a service.EnterpriseActor, id int64) {
	memberID, ok := enterprisePathID(c, "memberId")
	if !ok {
		return
	}
	var request struct {
		Password *string `json:"password"`
	}
	if !enterpriseBody(c, &request) {
		return
	}
	item, err := h.management.ResetMemberPassword(c.Request.Context(), a, id, memberID, request.Password)
	enterpriseSuccess(c, item, err)
}

func (h *EnterpriseHandler) batchMemberQuotas(c *gin.Context, a service.EnterpriseActor, id int64) {
	var request struct {
		MemberIDs []int64         `json:"member_ids" binding:"required,min=1,max=500"`
		Fields    []string        `json:"fields" binding:"required,min=1,max=4,dive,oneof=daily weekly monthly total"`
		Daily     json.RawMessage `json:"daily_limit_usd"`
		Weekly    json.RawMessage `json:"weekly_limit_usd"`
		Monthly   json.RawMessage `json:"monthly_limit_usd"`
		Total     json.RawMessage `json:"total_limit_usd"`
	}
	if !enterpriseBody(c, &request) {
		return
	}
	in := service.EnterpriseBatchMemberQuotaInput{MemberIDs: request.MemberIDs, Fields: request.Fields}
	values := map[string]json.RawMessage{"daily": request.Daily, "weekly": request.Weekly, "monthly": request.Monthly, "total": request.Total}
	targets := map[string]**float64{"daily": &in.DailyLimitUSD, "weekly": &in.WeeklyLimitUSD, "monthly": &in.MonthlyLimitUSD, "total": &in.TotalLimitUSD}
	for _, field := range request.Fields {
		raw := values[field]
		if len(raw) == 0 || json.Unmarshal(raw, targets[field]) != nil {
			response.BadRequest(c, "Invalid or missing "+field+"_limit_usd")
			return
		}
	}
	item, err := h.management.BatchUpdateMemberQuotas(c.Request.Context(), a, id, in)
	enterpriseSuccess(c, item, err)
}
