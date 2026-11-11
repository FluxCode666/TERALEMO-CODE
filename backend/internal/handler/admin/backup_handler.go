package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type BackupHandler struct {
	backupService *service.BackupService
	imageStorage  *service.ImageStorageSettingService
}

func NewBackupHandler(backupService *service.BackupService, imageStorage *service.ImageStorageSettingService) *BackupHandler {
	return &BackupHandler{
		backupService: backupService,
		imageStorage:  imageStorage,
	}
}

// ─── S3 配置 ───

func (h *BackupHandler) GetS3Config(c *gin.Context) {
	cfg, err := h.backupService.GetS3Config(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *BackupHandler) UpdateS3Config(c *gin.Context) {
	var req service.BackupS3Config
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	cfg, err := h.backupService.UpdateS3Config(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if h.imageStorage != nil {
		h.imageStorage.Invalidate()
	}
	response.Success(c, cfg)
}

func (h *BackupHandler) TestS3Connection(c *gin.Context) {
	var req service.BackupS3Config
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	err := h.backupService.TestS3Connection(c.Request.Context(), req)
	if err != nil {
		response.Success(c, gin.H{"ok": false, "message": err.Error()})
		return
	}
	response.Success(c, gin.H{"ok": true, "message": "connection successful"})
}

func (h *BackupHandler) GetImageStorageConfig(c *gin.Context) {
	ctx := c.Request.Context()
	cfg, err := h.imageStorage.Get(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"config":            cfg,
		"secret_configured": h.imageStorage.SecretConfigured(ctx),
	})
}

func (h *BackupHandler) UpdateImageStorageConfig(c *gin.Context) {
	var req service.ImageStorageSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	cfg, err := h.imageStorage.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *BackupHandler) TestImageStorageConnection(c *gin.Context) {
	var req service.ImageStorageSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.imageStorage.TestConnection(c.Request.Context(), req); err != nil {
		response.Success(c, gin.H{"ok": false, "message": err.Error()})
		return
	}
	response.Success(c, gin.H{"ok": true, "message": "connection successful"})
}
