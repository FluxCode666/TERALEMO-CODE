package handler

import (
	"context"
	"fmt"
	"log/slog"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (h *OpenAIGatewayHandler) checkOpenAIWSModelRequestIP(ctx context.Context, clientIP string, key *service.APIKey) error {
	allowed := true
	if key != nil && (len(key.IPWhitelist) > 0 || len(key.IPBlacklist) > 0) {
		allowed, _ = ip.CheckIPRestriction(clientIP, key.IPWhitelist, key.IPBlacklist)
	}
	var err error
	if allowed && h.apiKeyService != nil {
		allowed, err = h.apiKeyService.CheckModelRequestIP(ctx, clientIP, key)
	}
	if err == nil && allowed {
		return nil
	}
	slog.Warn("websocket model request IP rejected", "audit", true, "client_ip", clientIP, "error", err)
	return infraerrors.Forbidden("ip_not_allowed", fmt.Sprintf("Client IP %s is not allowed; contact the administrator", clientIP))
}
