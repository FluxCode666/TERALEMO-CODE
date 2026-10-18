package admin

import (
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type enterpriseMemberConcurrencyRequest struct {
	Concurrency json.RawMessage `json:"concurrency"`
}

func (h *EnterpriseHandler) updateMemberConcurrency(c *gin.Context, a service.EnterpriseActor, id int64) {
	var req enterpriseMemberConcurrencyRequest
	if !enterpriseBody(c, &req) {
		return
	}
	var concurrency *int
	if len(req.Concurrency) == 0 || json.Unmarshal(req.Concurrency, &concurrency) != nil || !service.EnterpriseMemberConcurrencyValid(concurrency) {
		response.ErrorFrom(c, service.ErrEnterpriseInvalidInput)
		return
	}
	result, err := h.management.UpdateMemberConcurrency(c.Request.Context(), a, id, concurrency)
	enterpriseSuccess(c, result, err)
}
