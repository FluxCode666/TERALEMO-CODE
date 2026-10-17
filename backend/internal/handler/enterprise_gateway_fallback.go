package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// A disallowed fallback keeps the upstream error that triggered the fallback.
func (h *GatewayHandler) rejectEnterprisePromptTooLongFallback(c *gin.Context, user *service.User, group *service.Group, account *service.Account, original *service.PromptTooLongError) bool {
	if service.EnterpriseAllowsGroupForUser(user, group) {
		return false
	}
	_ = h.antigravityGatewayService.WriteMappedClaudeError(c, account, original.StatusCode, original.RequestID, original.Body)
	return true
}
