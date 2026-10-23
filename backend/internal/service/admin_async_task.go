package service

import (
	"context"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type AdminAsyncTaskFilter struct {
	Page, PageSize            int
	UserID, APIKeyID, GroupID int64
	Kind, Status, Protocol    string
	Search                    string
}

type AdminAsyncTask struct {
	AsyncTask
	UserID    int64  `json:"user_id"`
	UserEmail string `json:"user_email,omitempty"`
	UserName  string `json:"user_name,omitempty"`
	KeyName   string `json:"key_name,omitempty"`
	GroupName string `json:"group_name,omitempty"`
}

type AdminAsyncTaskPage struct {
	Items    []AdminAsyncTask `json:"items"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

// The repository filters records in bounded batches, retaining only one page.
type AdminAsyncTaskStore interface {
	ListAdminAsyncTasks(context.Context, AdminAsyncTaskFilter) ([]AsyncTaskRecord, int, error)
}

func (f AdminAsyncTaskFilter) Matches(task AsyncTaskRecord) bool {
	if task.ExpiresAt <= time.Now().Unix() || f.UserID > 0 && task.UserID != f.UserID || f.APIKeyID > 0 && task.APIKeyID != f.APIKeyID || f.GroupID > 0 && (task.GroupID == nil || *task.GroupID != f.GroupID) {
		return false
	}
	if f.Kind != "" && task.Kind != f.Kind || f.Status != "" && task.Status != f.Status || f.Protocol != "" && task.Protocol != f.Protocol {
		return false
	}
	return f.Search == "" || strings.Contains(strings.ToLower(task.TaskID+" "+task.Model), f.Search)
}

func (s *ImageTaskService) ListAdminTasks(ctx context.Context, filter AdminAsyncTaskFilter) (*AdminAsyncTaskPage, error) {
	if filter.Page < 1 || filter.Page > 1000000 || filter.PageSize < 1 || filter.PageSize > 100 || filter.UserID < 0 || filter.APIKeyID < 0 || filter.GroupID < 0 {
		return nil, infraerrors.BadRequest("INVALID_TASK_FILTER", "Invalid task pagination or identity filter")
	}
	if filter.Kind != "" && filter.Kind != "image" && filter.Kind != "video" {
		return nil, infraerrors.BadRequest("INVALID_TASK_FILTER", "Invalid task kind")
	}
	switch filter.Status {
	case "", "processing", "completed", "failed", "cancelled":
	default:
		return nil, infraerrors.BadRequest("INVALID_TASK_FILTER", "Invalid task status")
	}
	filter.Search = strings.ToLower(strings.TrimSpace(filter.Search))
	if len(filter.Search) > 256 || len(filter.Protocol) > 64 {
		return nil, infraerrors.BadRequest("INVALID_TASK_FILTER", "Task filter is too long")
	}
	if s == nil {
		return nil, ErrImageTaskUnavailable
	}
	store, ok := s.store.(AdminAsyncTaskStore)
	if !ok {
		return nil, ErrImageTaskUnavailable
	}
	records, total, err := store.ListAdminAsyncTasks(ctx, filter)
	if err != nil {
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	items := make([]AdminAsyncTask, 0, len(records))
	for _, record := range records {
		items = append(items, AdminAsyncTask{AsyncTask: record.AsyncTask, UserID: record.UserID})
	}
	return &AdminAsyncTaskPage{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}
