package admin

import (
	"log/slog"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) SetForceTotpAuth(auth *service.AuthService) { h.forceTotpAuth = auth }

func (h *SettingHandler) GetForceTotp(c *gin.Context) {
	if h.forceTotpAuth == nil {
		response.Error(c, 503, "Session service unavailable")
		return
	}
	enabled, err := h.settingService.ForceTotpEnabled(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	count, err := h.forceTotpAuth.CountUsersWithoutTOTP(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"enabled": enabled, "users_without_totp": count, "encryption_key_configured": h.settingService.IsTotpEncryptionKeyConfigured()})
}

func (h *SettingHandler) UpdateForceTotp(c *gin.Context) {
	var request struct {
		Enabled *bool `json:"enabled" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if h.forceTotpAuth == nil {
		response.Error(c, 503, "Session service unavailable")
		return
	}
	if *request.Enabled && !h.settingService.IsTotpEncryptionKeyConfigured() {
		response.ErrorWithDetails(c, 400, "Configure a fixed TOTP encryption key before requiring two-factor authentication", "TOTP_ENCRYPTION_KEY_REQUIRED", nil)
		return
	}
	if !h.enforceSecuritySettingsStepUp(c) {
		return
	}
	if err := h.forceTotpAuth.UpdateForceTotp(c.Request.Context(), *request.Enabled); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	slog.Info("forced TOTP policy updated", "audit", true, "user_id", subject.UserID, "enabled", *request.Enabled)
	h.GetForceTotp(c)
}
