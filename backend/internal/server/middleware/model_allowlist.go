package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/googleapi"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TrustedProxyContext(settings *service.SettingService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var policy ip.ProxyPolicy
		if settings != nil {
			resolved, err := settings.ModelProxyPolicy(c.Request.Context())
			if err == nil {
				policy = resolved
			} else {
				slog.Warn("trusted proxy settings unavailable", "error", err)
			}
		} else if cfg != nil {
			policy.Local, _ = ip.ParseProxyRanges(cfg.Server.TrustedProxies)
		}
		ip.SetProxyPolicy(c, policy)
		binding := &service.SessionBinding{IP: ip.GetClientIP(c), UserAgent: c.Request.UserAgent()}
		c.Request = c.Request.WithContext(service.WithSessionBinding(c.Request.Context(), binding))
		c.Next()
	}
}

func enforceModelAllowlist(c *gin.Context, apiKeyService *service.APIKeyService, apiKey *service.APIKey, google bool) bool {
	clientIP := ip.GetSecurityClientIP(c, false)
	allowed, err := apiKeyService.CheckModelRequestIP(c.Request.Context(), clientIP, apiKey)
	if allowed && err == nil {
		return true
	}
	MarkIngressRejected(c, IngressRejectIPRestricted)
	slog.Warn("model request IP rejected", "audit", true, "client_ip", clientIP, "path", c.Request.URL.Path, "error", err)
	message := fmt.Sprintf("Client IP %s is not allowed; contact the administrator", clientIP)
	if err != nil {
		message = "Model request IP policy is temporarily unavailable"
	}
	if google {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{
			"code": http.StatusForbidden, "message": message, "status": googleapi.HTTPStatusToGoogleStatus(http.StatusForbidden),
			"details": []googleapi.ErrorDetailInfo{{Type: "type.googleapis.com/google.rpc.ErrorInfo", Reason: "ip_not_allowed", Domain: "sub2api"}},
		}})
	} else if strings.HasSuffix(c.Request.URL.Path, "/messages") || strings.HasSuffix(c.Request.URL.Path, "/messages/count_tokens") {
		c.JSON(http.StatusForbidden, gin.H{"type": "error", "error": gin.H{"type": "permission_error", "code": "ip_not_allowed", "message": message}})
	} else {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"type": "permission_error", "code": "ip_not_allowed", "message": message, "param": nil}})
	}
	c.Abort()
	return false
}
