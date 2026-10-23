package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// AsyncTask exposes only user-owned task metadata. Internal routing fields are
// stored separately by the repository and never serialized into panel responses.
type AsyncTask struct {
	ID          string `json:"id"`
	TaskID      string `json:"task_id"`
	Kind        string `json:"kind"`
	Protocol    string `json:"protocol"`
	Model       string `json:"model,omitempty"`
	Status      string `json:"status"`
	APIKeyID    int64  `json:"api_key_id"`
	GroupID     *int64 `json:"group_id,omitempty"`
	ResultURL   string `json:"result_url,omitempty"`
	Error       string `json:"error,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	CompletedAt *int64 `json:"completed_at,omitempty"`
	ExpiresAt   int64  `json:"expires_at"`
}

type AsyncTaskRecord struct {
	AsyncTask
	UserID    int64  `json:"user_id"`
	RequestID string `json:"request_id"`
}

type AsyncTaskStore interface {
	SaveAsyncTask(context.Context, *AsyncTaskRecord) error
	GetAsyncTask(context.Context, int64, string) (*AsyncTaskRecord, error)
	ListAsyncTasks(context.Context, int64) ([]AsyncTaskRecord, error)
}

type AsyncTaskPage struct {
	Items    []AsyncTask `json:"items"`
	Total    int         `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}

func (s *ImageTaskService) taskCatalog() (AsyncTaskStore, error) {
	if s == nil {
		return nil, ErrImageTaskUnavailable
	}
	store, ok := s.store.(AsyncTaskStore)
	if !ok {
		return nil, ErrImageTaskUnavailable
	}
	return store, nil
}

func (s *ImageTaskService) ListUserTasks(ctx context.Context, userID int64, page, pageSize int, kind, status, search string) (*AsyncTaskPage, error) {
	store, err := s.taskCatalog()
	if err != nil {
		return nil, err
	}
	records, err := store.ListAsyncTasks(ctx, userID)
	if err != nil {
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	items := make([]AsyncTask, 0, len(records))
	search = strings.ToLower(strings.TrimSpace(search))
	for _, record := range records {
		if record.UserID != userID || record.ExpiresAt <= time.Now().Unix() {
			continue
		}
		if kind != "" && record.Kind != kind || status != "" && record.Status != status {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(record.TaskID+" "+record.Model), search) {
			continue
		}
		items = append(items, record.AsyncTask)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt == items[j].CreatedAt {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt > items[j].CreatedAt
	})
	total := len(items)
	start := min((page-1)*pageSize, total)
	return &AsyncTaskPage{Items: items[start:min(start+pageSize, total)], Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *ImageTaskService) GetUserTask(ctx context.Context, userID int64, id string) (*AsyncTaskRecord, error) {
	store, err := s.taskCatalog()
	if err != nil {
		return nil, err
	}
	record, err := store.GetAsyncTask(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if record == nil || record.UserID != userID || record.ExpiresAt <= time.Now().Unix() {
		return nil, ErrImageTaskNotFound
	}
	return record, nil
}

func (s *ImageTaskService) ObserveVideoTask(ctx context.Context, key *APIKey, requestID, model, protocol string, result *OpenAIForwardResult, deleting bool) error {
	if s == nil || result == nil || key == nil {
		return nil
	}
	store, err := s.taskCatalog()
	if err != nil {
		return err
	}
	id := fmt.Sprintf("vidtask_%x", sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", key.UserID, key.ID, requestID))))
	record, err := store.GetAsyncTask(ctx, key.UserID, id)
	if err != nil && !errors.Is(err, ErrImageTaskNotFound) {
		return err
	}
	if record == nil {
		now := time.Now()
		taskID := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(requestID, "seedance:"), "gemini:"), "minimax:")
		record = &AsyncTaskRecord{AsyncTask: AsyncTask{ID: id, TaskID: taskID, Kind: "video", Protocol: protocol, Model: model, Status: "processing", APIKeyID: key.ID, GroupID: key.GroupID, CreatedAt: now.Unix(), ExpiresAt: now.Add(defaultImageTaskTTL).Unix()}, UserID: key.UserID, RequestID: requestID}
	}
	if record.Status == "processing" {
		if result.AsyncTaskStatus != "" {
			record.Status = result.AsyncTaskStatus
		} else if result.VideoCount > 0 {
			record.Status = "completed"
		}
		if deleting {
			record.Status = "cancelled"
		}
		if record.Status != "processing" {
			now := time.Now().Unix()
			record.CompletedAt = &now
		}
		record.ResultURL, record.Error = result.AsyncTaskURL, result.AsyncTaskError
	}
	return store.SaveAsyncTask(ctx, record)
}
