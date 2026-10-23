package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AsyncImageHandler) ListUserTasks(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	// Bound pagination before offset multiplication in the service.
	if page < 1 || page > 1000 || size < 1 || size > 100 {
		response.BadRequest(c, "Invalid pagination")
		return
	}
	tasks, err := h.tasks.ListUserTasks(c.Request.Context(), subject.UserID, page, size, c.Query("kind"), c.Query("status"), c.Query("search"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	for i := range tasks.Items {
		tasks.Items[i] = h.publicAsyncTask(c, subject.UserID, tasks.Items[i])
	}
	response.Success(c, tasks)
}

func (h *AsyncImageHandler) GetUserTask(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	task, err := h.tasks.GetUserTask(c.Request.Context(), subject.UserID, c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, h.publicAsyncTask(c, subject.UserID, task.AsyncTask))
}

func (h *AsyncImageHandler) RefreshUserTask(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	task, err := h.tasks.GetUserTask(c.Request.Context(), subject.UserID, c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if task.Kind != "video" || task.Status != "processing" {
		response.Success(c, h.publicAsyncTask(c, subject.UserID, task.AsyncTask))
		return
	}
	if h.openAI == nil || h.openAI.apiKeyService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Task refresh unavailable")
		return
	}
	key, err := h.openAI.apiKeyService.GetByID(c.Request.Context(), task.APIKeyID)
	if err != nil || key == nil || key.UserID != subject.UserID || key.User == nil || key.GroupID == nil || task.GroupID == nil || *key.GroupID != *task.GroupID {
		response.NotFound(c, "Task API key or group is no longer available")
		return
	}
	// Reuse the owned-task gateway path, including its account binding and billing
	// snapshot/claim, without exposing an API key secret to the browser.
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	poll, recorder, release := newAsyncImageContext(c, nil, 30*time.Second)
	defer release()
	poll.Request = poll.Request.WithContext(ctx)
	poll.Request.Method = http.MethodGet
	poll.Set(string(middleware.ContextKeyAPIKey), key)
	poll.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: subject.UserID, Concurrency: key.User.Concurrency})
	endpoint := service.GrokMediaEndpointVideoStatus
	if task.Protocol == service.VideoProtocolSeedance {
		endpoint = service.SeedanceEndpointStatus
	}
	if task.Protocol == service.VideoProtocolGemini {
		endpoint = service.GeminiVideoEndpointStatus
	}
	if task.Protocol == service.VideoProtocolMiniMax {
		endpoint = service.MiniMaxVideoEndpointStatus
	}
	h.openAI.handleGrokMedia(poll, endpoint, task.RequestID)
	if recorder.Code >= 300 {
		response.ErrorWithDetails(c, http.StatusBadGateway, "Failed to refresh upstream task; retry later", "TASK_REFRESH_FAILED", nil)
		return
	}
	task, err = h.tasks.GetUserTask(c.Request.Context(), subject.UserID, task.ID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, h.publicAsyncTask(c, subject.UserID, task.AsyncTask))
}

func (h *AsyncImageHandler) DownloadUserTask(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	task, err := h.tasks.GetUserTask(c.Request.Context(), subject.UserID, c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if task.Kind != "video" || task.Status != "completed" {
		response.NotFound(c, "Video download unavailable")
		return
	}
	if h.openAI == nil || h.openAI.apiKeyService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Task download unavailable")
		return
	}
	key, err := h.openAI.apiKeyService.GetByID(c.Request.Context(), task.APIKeyID)
	if err != nil || key == nil || key.UserID != subject.UserID || key.GroupID == nil || task.GroupID == nil || *key.GroupID != *task.GroupID {
		response.NotFound(c, "Task API key or group is no longer available")
		return
	}
	if task.Protocol == service.VideoProtocolGemini {
		if err := h.openAI.gatewayService.ForwardNativeVideoContent(c.Request.Context(), c, task, 0); err != nil {
			response.Error(c, http.StatusBadGateway, "Failed to download video from upstream")
		}
		return
	}
	if task.Protocol == service.VideoProtocolMiniMax || task.Protocol == service.VideoProtocolSeedance {
		if err := h.openAI.gatewayService.ForwardNativeVideoContent(c.Request.Context(), c, task, 0); err != nil {
			response.Error(c, http.StatusBadGateway, "Failed to download video from upstream")
		}
		return
	}
	c.Set(string(middleware.ContextKeyAPIKey), key)
	h.openAI.handleGrokMedia(c, service.GrokMediaEndpointVideoContent, task.RequestID)
}

func (h *AsyncImageHandler) publicAsyncTask(c *gin.Context, userID int64, task service.AsyncTask) service.AsyncTask {
	if task.Kind != "video" {
		return task
	}
	if h.openAI == nil || h.openAI.gatewayService == nil {
		task.ResultURL, task.Error = "", ""
		return task
	}
	return h.openAI.gatewayService.PublicVideoTask(c.Request.Context(), userID, task)
}
