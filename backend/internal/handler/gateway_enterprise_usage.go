package handler

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type enterpriseUsageIdentityReader interface {
	Identity(context.Context, int64) (*service.EnterpriseIdentity, error)
}

// Use the caller's current membership and role, independent of any management
// query actor. A stale auth snapshot must never expose the personal wallet.
func (h *GatewayHandler) enterpriseUsageIdentity(c *gin.Context, ctx context.Context, key *service.APIKey, userID int64) (*service.EnterpriseIdentity, bool) {
	var identity *service.EnterpriseIdentity
	var err error
	if h.enterpriseIdentityReader != nil {
		identity, err = h.enterpriseIdentityReader.Identity(ctx, userID)
	}
	if err == nil && identity == nil && key.User != nil && key.User.Enterprise != nil {
		err = fmt.Errorf("enterprise usage identity unavailable")
	}
	if err != nil {
		h.errorResponse(c, http.StatusServiceUnavailable, "billing_service_error", "Failed to get enterprise balance visibility")
		return nil, false
	}
	if identity != nil {
		if identity.EnterpriseStatus != service.EnterpriseStatusActive {
			h.errorResponse(c, http.StatusForbidden, service.ErrEnterpriseGatewayDisabled.Reason, service.ErrEnterpriseGatewayDisabled.Message)
			return nil, false
		}
		if identity.MemberStatus != service.EnterpriseMemberStatusActive {
			h.errorResponse(c, http.StatusForbidden, service.ErrEnterpriseGatewayMemberDisabled.Reason, service.ErrEnterpriseGatewayMemberDisabled.Message)
			return nil, false
		}
	}
	return identity, true
}

func applyEnterpriseUsageBalance(resp gin.H, identity *service.EnterpriseIdentity) {
	if identity == nil {
		return
	}
	resp["balance_visible"] = identity.BalanceVisible
	resp["balance"] = nil
	resp["remaining"] = nil
	if identity.BalanceVisible && identity.Balance != nil {
		resp["balance"] = *identity.Balance
		resp["remaining"] = *identity.Balance
	}
	resp["unit"] = "USD"
}
