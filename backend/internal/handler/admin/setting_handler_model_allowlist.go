package admin

import (
	"log/slog"
	"net"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetModelAllowlist(c *gin.Context) {
	settings, err := h.settingService.GetModelAllowlist(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	observations, err := h.settingService.ListModelAllowlistObservations(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	ips, users, keys := map[string]bool{}, map[int64]bool{}, map[int64]bool{}
	matches := map[string]bool{}
	for _, observation := range observations {
		if observation.LastSeen < time.Now().Add(-24*time.Hour).Unix() {
			continue
		}
		matched, checked := matches[observation.IP]
		if !checked {
			matched = h.settingService.ModelAllowlistMatches(c.Request.Context(), settings, observation.IP)
			matches[observation.IP] = matched
		}
		if !matched {
			ips[observation.IP], users[observation.UserID], keys[observation.KeyID] = true, true, true
		}
	}
	response.Success(c, gin.H{"settings": settings, "domains": h.settingService.PreviewModelAllowlist(c.Request.Context(), settings),
		"observations": observations, "impact": gin.H{"ips": len(ips), "users": len(users), "keys": len(keys)}})
}

func (h *SettingHandler) UpdateModelAllowlist(c *gin.Context) {
	var settings service.ModelAllowlistSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := service.ValidateModelAllowlist(&settings); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if !h.enforceSecuritySettingsStepUp(c) {
		return
	}
	previous, err := h.settingService.GetModelAllowlist(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	settings.Enabled = previous.Enabled
	if err := h.settingService.SaveModelAllowlistEntries(c.Request.Context(), settings); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	slog.Info("model allowlist updated", "audit", true, "user_id", subject.UserID, "enabled", settings.Enabled, "entries", settings.Entries, "trusted_cdn_proxies", settings.TrustedCDN)
	h.GetModelAllowlist(c)
}

func (h *SettingHandler) enforceSecuritySettingsStepUp(c *gin.Context) bool {
	if h.userService == nil || h.totpService == nil {
		response.Error(c, 503, "Two-factor verification service unavailable")
		return false
	}
	return middleware.EnforceStepUpAlways(c, h.totpService, h.userService)
}

func (h *SettingHandler) TestModelAllowlist(c *gin.Context) {
	var request struct {
		Settings service.ModelAllowlistSettings `json:"settings"`
		IP       string                         `json:"ip"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := service.ValidateModelAllowlist(&request.Settings); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if request.IP != "" && net.ParseIP(request.IP) == nil {
		response.BadRequest(c, "Invalid client IP")
		return
	}
	response.Success(c, gin.H{
		"allowed": h.settingService.TestModelAllowlist(c.Request.Context(), request.Settings, request.IP),
		"domains": h.settingService.PreviewModelAllowlist(c.Request.Context(), request.Settings),
	})
}

func (h *SettingHandler) ToggleModelAllowlist(c *gin.Context) {
	var request struct {
		Enabled *bool `json:"enabled" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if !h.enforceSecuritySettingsStepUp(c) {
		return
	}
	settings, err := h.settingService.GetModelAllowlist(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	settings.Enabled = *request.Enabled
	if err := h.settingService.SetModelAllowlistEnabled(c.Request.Context(), settings.Enabled); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	slog.Info("model allowlist enforcement changed", "audit", true, "user_id", subject.UserID, "enabled", settings.Enabled)
	h.GetModelAllowlist(c)
}
