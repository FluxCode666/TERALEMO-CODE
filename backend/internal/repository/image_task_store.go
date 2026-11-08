package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/tidwall/gjson"
)

const imageTaskKeyPrefix = "image_task:"

type imageTaskStore struct {
	rdb          *redis.Client
	adminIndexMu sync.Mutex
}

func NewImageTaskStore(rdb *redis.Client) service.ImageTaskStore {
	return &imageTaskStore{rdb: rdb}
}

func (s *imageTaskStore) Save(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, imageTaskKey(task.ID), data, ttl)
	resultURLs := imageTaskResultURLs(task.Result)
	record := &service.AsyncTaskRecord{AsyncTask: service.AsyncTask{
		ID: task.ID, TaskID: task.ID, Kind: "image", Protocol: task.Platform, Model: task.Model,
		Status: task.Status, APIKeyID: task.APIKeyID, GroupID: task.GroupID,
		ResultURL: gjson.GetBytes(task.Result, "data.0.url").String(), ResultURLs: resultURLs,
		Error:     gjson.GetBytes(task.Error, "message").String(),
		CreatedAt: task.CreatedAt, CompletedAt: task.CompletedAt, ExpiresAt: task.ExpiresAt,
	}, UserID: task.UserID, RequestID: task.ID}
	if err := writeAsyncTask(ctx, pipe, record); err != nil {
		return err
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (s *imageTaskStore) Get(ctx context.Context, id string) (*service.ImageTaskRecord, error) {
	data, err := s.rdb.Get(ctx, imageTaskKey(id)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, service.ErrImageTaskNotFound
		}
		return nil, err
	}
	var task service.ImageTaskRecord
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func imageTaskKey(id string) string {
	return imageTaskKeyPrefix + strings.TrimSpace(id)
}

// imageTaskResultURLs collects every result link of an image task, preserving
// the upstream order, skipping blank entries and bounding the list to 10.
func imageTaskResultURLs(result json.RawMessage) []string {
	if len(result) == 0 {
		return nil
	}
	values := gjson.GetBytes(result, "data.#.url")
	if !values.IsArray() {
		return nil
	}
	urls := make([]string, 0, len(values.Array()))
	for _, value := range values.Array() {
		url := strings.TrimSpace(value.String())
		if url == "" {
			continue
		}
		urls = append(urls, url)
		if len(urls) == 10 {
			break
		}
	}
	if len(urls) == 0 {
		return nil
	}
	return urls
}

func asyncTaskKey(userID int64, id string) string {
	return fmt.Sprintf("async_task:%d:%s", userID, id)
}

func asyncTaskIndex(userID int64) string { return fmt.Sprintf("async_tasks:user:%d", userID) }

func writeAsyncTask(ctx context.Context, pipe redis.Pipeliner, task *service.AsyncTaskRecord) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	ttl := time.Until(time.Unix(task.ExpiresAt, 0))
	if ttl <= 0 {
		return service.ErrImageTaskNotFound
	}
	key := asyncTaskIndex(task.UserID)
	pipe.Eval(ctx, `
local current = redis.call('GET', KEYS[1])
if current then
  local previous = cjson.decode(current)
  local incoming = cjson.decode(ARGV[1])
  if previous.status ~= 'processing' and previous.status ~= incoming.status then
    return 0
  end
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
return 1`, []string{asyncTaskKey(task.UserID, task.ID)}, data, max(1, ttl.Milliseconds()))
	pipe.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(time.Now().Unix(), 10))
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(task.ExpiresAt), Member: task.ID})
	// Bound each user's index independently. Evicted records retain their own TTL.
	pipe.ZRemRangeByRank(ctx, key, 0, -1001)
	pipe.Expire(ctx, key, 48*time.Hour)
	indexGlobalAsyncTask(ctx, pipe, task)
	// Keep the global catalog bounded by retention even without admin readers.
	pipe.Eval(ctx, pruneGlobalAsyncTasksScript, []string{asyncTasksGlobalIndex, asyncTasksGlobalExpiry}, strconv.FormatInt(time.Now().Unix(), 10), 50)
	return nil
}

func (s *imageTaskStore) SaveAsyncTask(ctx context.Context, task *service.AsyncTaskRecord) error {
	pipe := s.rdb.TxPipeline()
	if err := writeAsyncTask(ctx, pipe, task); err != nil {
		return err
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (s *imageTaskStore) GetAsyncTask(ctx context.Context, userID int64, id string) (*service.AsyncTaskRecord, error) {
	data, err := s.rdb.Get(ctx, asyncTaskKey(userID, id)).Bytes()
	if err == redis.Nil {
		return nil, service.ErrImageTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	var task service.AsyncTaskRecord
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}
	if task.UserID != userID {
		return nil, service.ErrImageTaskNotFound
	}
	return &task, nil
}

func (s *imageTaskStore) ListAsyncTasks(ctx context.Context, userID int64) ([]service.AsyncTaskRecord, error) {
	index := asyncTaskIndex(userID)
	if err := s.rdb.ZRemRangeByScore(ctx, index, "-inf", strconv.FormatInt(time.Now().Unix(), 10)).Err(); err != nil {
		return nil, err
	}
	ids, err := s.rdb.ZRangeArgs(ctx, redis.ZRangeArgs{Key: index, Start: 0, Stop: 999, Rev: true}).Result()
	if err != nil {
		return nil, err
	}
	items := make([]service.AsyncTaskRecord, 0, len(ids))
	if len(ids) == 0 {
		return items, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = asyncTaskKey(userID, id)
	}
	data, err := s.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	for _, raw := range data {
		if raw == nil {
			continue
		}
		var task service.AsyncTaskRecord
		value, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("invalid async task record")
		}
		if err := json.Unmarshal([]byte(value), &task); err != nil {
			return nil, err
		}
		if task.UserID == userID {
			items = append(items, task)
		}
	}
	return items, nil
}

// creationUploadKey is the Redis key of one upload record.
func creationUploadKey(uploadID string) string {
	return "creation_upload:" + strings.TrimSpace(uploadID)
}

// creationUploadIndex is the per-user sorted set of live upload IDs scored by
// expiry, used to count and prune the 24h window without scanning keys.
func creationUploadIndex(userID int64) string {
	return fmt.Sprintf("creation_uploads:user:%d", userID)
}

// SaveCreationUpload writes the record with a TTL equal to its remaining
// lifetime and refreshes the user index, dropping already-expired members.
func (s *imageTaskStore) SaveCreationUpload(ctx context.Context, rec *service.CreationUploadRecord) error {
	if rec == nil || strings.TrimSpace(rec.UploadID) == "" || rec.UserID <= 0 {
		return service.ErrCreationUploadNotFound
	}
	ttl := time.Until(time.Unix(rec.ExpiresAt, 0))
	if ttl <= 0 {
		return service.ErrCreationUploadNotFound
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	index := creationUploadIndex(rec.UserID)
	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, creationUploadKey(rec.UploadID), data, ttl)
	pipe.ZRemRangeByScore(ctx, index, "-inf", strconv.FormatInt(time.Now().Unix(), 10))
	pipe.ZAdd(ctx, index, redis.Z{Score: float64(rec.ExpiresAt), Member: rec.UploadID})
	pipe.Expire(ctx, index, 48*time.Hour)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *imageTaskStore) GetCreationUpload(ctx context.Context, uploadID string) (*service.CreationUploadRecord, error) {
	data, err := s.rdb.Get(ctx, creationUploadKey(uploadID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, service.ErrCreationUploadNotFound
		}
		return nil, err
	}
	var record service.CreationUploadRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *imageTaskStore) CountActiveCreationUploads(ctx context.Context, userID int64) (int64, error) {
	index := creationUploadIndex(userID)
	if err := s.rdb.ZRemRangeByScore(ctx, index, "-inf", strconv.FormatInt(time.Now().Unix(), 10)).Err(); err != nil {
		return 0, err
	}
	return s.rdb.ZCard(ctx, index).Result()
}
