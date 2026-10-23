package handler

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AsyncImageHandler) ListAdminTasks(c *gin.Context) {
	filter := service.AdminAsyncTaskFilter{Kind: c.Query("kind"), Status: c.Query("status"), Protocol: c.Query("protocol"), Search: c.Query("search")}
	var err error
	filter.Page, err = strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil {
		response.BadRequest(c, "Invalid page")
		return
	}
	filter.PageSize, err = strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil {
		response.BadRequest(c, "Invalid page_size")
		return
	}
	for name, target := range map[string]*int64{"user_id": &filter.UserID, "api_key_id": &filter.APIKeyID, "group_id": &filter.GroupID} {
		if value := strings.TrimSpace(c.Query(name)); value != "" {
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 {
				response.BadRequest(c, "Invalid "+name)
				return
			}
			*target = id
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	tasks, err := h.tasks.ListAdminTasks(ctx, filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.enrichAdminTasks(ctx, tasks.Items)
	c.Header("Cache-Control", "no-store")
	response.Success(c, tasks)
}

func (h *AsyncImageHandler) adminTask(c *gin.Context) (*service.AsyncTaskRecord, bool) {
	userID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || userID <= 0 || len(c.Param("id")) > 128 {
		response.BadRequest(c, "Invalid task identity")
		return nil, false
	}
	task, err := h.tasks.GetUserTask(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return nil, false
	}
	return task, true
}

// Serialize a deliberate DTO: internal routing IDs and key secrets are never
// included. Administrators may inspect the original result URL for diagnosis.
func (h *AsyncImageHandler) adminTaskResponse(c *gin.Context, record *service.AsyncTaskRecord) {
	tasks := []service.AdminAsyncTask{{AsyncTask: record.AsyncTask, UserID: record.UserID}}
	h.enrichAdminTasks(c.Request.Context(), tasks)
	c.Header("Cache-Control", "no-store")
	response.Success(c, tasks[0])
}

func (h *AsyncImageHandler) enrichAdminTasks(ctx context.Context, tasks []service.AdminAsyncTask) {
	if len(tasks) == 0 || h.openAI == nil || h.openAI.apiKeyService == nil {
		return
	}
	ids := make([]int64, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.APIKeyID)
	}
	metadata, err := h.openAI.apiKeyService.GetTaskMetadataByIDs(ctx, ids)
	if err != nil {
		// Identity decoration is optional: keep task IDs usable during a database
		// outage instead of falling back to one query for every task.
		return
	}
	for i := range tasks {
		task := &tasks[i]
		key, exists := metadata[task.APIKeyID]
		if !exists || key.UserID != task.UserID {
			continue
		}
		task.KeyName, task.UserEmail, task.UserName = key.KeyName, key.UserEmail, key.UserName
		if task.GroupID != nil && key.GroupID != nil && *key.GroupID == *task.GroupID {
			task.GroupName = key.GroupName
		}
	}
}

func (h *AsyncImageHandler) GetAdminTask(c *gin.Context) {
	if task, ok := h.adminTask(c); ok {
		h.adminTaskResponse(c, task)
	}
}

func (h *AsyncImageHandler) RefreshAdminTask(c *gin.Context) {
	task, ok := h.adminTask(c)
	if !ok {
		return
	}
	// Keep the admin's original auth/audit context intact. Reuse the user refresh
	// path under the recorded owner so account binding and billing stay unchanged.
	owned, recorder, release := newAsyncImageContext(c, nil, 30*time.Second)
	defer release()
	owned.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: task.UserID})
	h.RefreshUserTask(owned)
	if recorder.Code >= 300 {
		c.Data(recorder.Code, "application/json", recorder.Body.Bytes())
		return
	}
	task, ok = h.adminTask(c)
	if ok {
		h.adminTaskResponse(c, task)
	}
}

func (h *AsyncImageHandler) DownloadAdminTask(c *gin.Context) {
	if _, ok := h.adminTask(c); !ok {
		return
	}
	userID, _ := strconv.ParseInt(c.Param("user_id"), 10, 64)
	owned := c.Copy()
	owned.Writer = c.Writer // Stream directly instead of buffering video in RAM.
	owned.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
	h.DownloadUserTask(owned)
}
