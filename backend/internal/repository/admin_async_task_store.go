package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	asyncTasksGlobalIndex       = "async_tasks:all"
	asyncTasksGlobalExpiry      = "async_tasks:expires"
	asyncTasksGlobalReady       = "async_tasks:global_index_ready:v1"
	asyncTaskBatchSize          = 200
	pruneGlobalAsyncTasksScript = `
local expired = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', ARGV[1], 'LIMIT', 0, ARGV[2])
for _, member in ipairs(expired) do
  redis.call('ZREM', KEYS[1], member)
  redis.call('ZREM', KEYS[2], member)
end
return #expired`
)

func indexGlobalAsyncTask(ctx context.Context, pipe redis.Pipeliner, task *service.AsyncTaskRecord) {
	member := asyncTaskKey(task.UserID, task.ID)
	pipe.ZAdd(ctx, asyncTasksGlobalIndex, redis.Z{Score: float64(task.CreatedAt), Member: member})
	pipe.ZAdd(ctx, asyncTasksGlobalExpiry, redis.Z{Score: float64(task.ExpiresAt), Member: member})
	pipe.Expire(ctx, asyncTasksGlobalIndex, 48*time.Hour)
	pipe.Expire(ctx, asyncTasksGlobalExpiry, 48*time.Hour)
}

// Backfill existing, unexpired records once. SCAN and writes are batched; no
// KEYS command or changes to the original records' TTLs are required.
func (s *imageTaskStore) ensureGlobalAsyncTaskIndex(ctx context.Context) error {
	ready, err := s.rdb.Exists(ctx, asyncTasksGlobalReady).Result()
	if err != nil || ready != 0 {
		return err
	}
	s.adminIndexMu.Lock()
	defer s.adminIndexMu.Unlock()
	ready, err = s.rdb.Exists(ctx, asyncTasksGlobalReady).Result()
	if err != nil || ready != 0 {
		return err
	}
	var cursor uint64
	for {
		keys, next, err := s.rdb.Scan(ctx, cursor, "async_task:*", asyncTaskBatchSize).Result()
		if err != nil {
			return err
		}
		for start := 0; start < len(keys); start += asyncTaskBatchSize {
			batch := keys[start:min(start+asyncTaskBatchSize, len(keys))]
			values, err := s.rdb.MGet(ctx, batch...).Result()
			if err != nil {
				return err
			}
			pipe := s.rdb.Pipeline()
			for i, value := range values {
				task := decodeGlobalAsyncTask(value, batch[i])
				if task != nil {
					indexGlobalAsyncTask(ctx, pipe, task)
				}
			}
			if _, err := pipe.Exec(ctx); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return s.rdb.Set(ctx, asyncTasksGlobalReady, "1", 48*time.Hour).Err()
}

func decodeGlobalAsyncTask(value any, key string) *service.AsyncTaskRecord {
	data, ok := value.(string)
	if !ok {
		return nil
	}
	var task service.AsyncTaskRecord
	if json.Unmarshal([]byte(data), &task) != nil || task.UserID <= 0 || task.ID == "" || key != asyncTaskKey(task.UserID, task.ID) || task.ExpiresAt <= time.Now().Unix() {
		return nil
	}
	return &task
}

func (s *imageTaskStore) ListAdminAsyncTasks(ctx context.Context, filter service.AdminAsyncTaskFilter) ([]service.AsyncTaskRecord, int, error) {
	if err := s.ensureGlobalAsyncTaskIndex(ctx); err != nil {
		return nil, 0, err
	}
	// Remove expired entries atomically from both indexes, in bounded chunks.
	for {
		count, err := s.rdb.Eval(ctx, pruneGlobalAsyncTasksScript, []string{asyncTasksGlobalIndex, asyncTasksGlobalExpiry}, strconv.FormatInt(time.Now().Unix(), 10), 500).Int()
		if err != nil {
			return nil, 0, err
		}
		if count < 500 {
			break
		}
	}
	items := make([]service.AsyncTaskRecord, 0, filter.PageSize)
	start, total := (filter.Page-1)*filter.PageSize, 0
	for offset := int64(0); ; offset += asyncTaskBatchSize {
		keys, err := s.rdb.ZRevRange(ctx, asyncTasksGlobalIndex, offset, offset+asyncTaskBatchSize-1).Result()
		if err != nil {
			return nil, 0, err
		}
		if len(keys) == 0 {
			break
		}
		values, err := s.rdb.MGet(ctx, keys...).Result()
		if err != nil {
			return nil, 0, err
		}
		for i, value := range values {
			task := decodeGlobalAsyncTask(value, keys[i])
			if task == nil || !filter.Matches(*task) {
				continue
			}
			if total >= start && len(items) < filter.PageSize {
				items = append(items, *task)
			}
			total++
		}
	}
	return items, total, nil
}
