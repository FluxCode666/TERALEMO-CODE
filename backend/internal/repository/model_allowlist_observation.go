package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const modelObservationIndex = "model_allowlist:observations"
const modelObservationPrefix = "model_allowlist:observation:"

var modelObservationScript = redis.NewScript(`
local member = ARGV[1]
local now = tonumber(ARGV[2])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - 604800)
if redis.call('ZSCORE', KEYS[1], member) == false and redis.call('ZCARD', KEYS[1]) >= 10000 then
  return 0
end
redis.call('ZADD', KEYS[1], now, member)
redis.call('EXPIRE', KEYS[1], 604800)
redis.call('HSET', KEYS[2], 'data', ARGV[3])
redis.call('HINCRBY', KEYS[2], 'count', 1)
redis.call('EXPIRE', KEYS[2], 604800)
return 1
`)

func (c *apiKeyCache) RecordModelAllowlistObservation(ctx context.Context, observation service.ModelAllowlistObservation) error {
	encoded, err := json.Marshal(observation)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", observation.IP, observation.UserID, observation.KeyID)))
	member := hex.EncodeToString(digest[:])
	return modelObservationScript.Run(ctx, c.rdb, []string{modelObservationIndex, modelObservationPrefix + member}, member, observation.LastSeen, string(encoded)).Err()
}

func (c *apiKeyCache) ListModelAllowlistObservations(ctx context.Context) ([]service.ModelAllowlistObservation, error) {
	members, err := c.rdb.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key: modelObservationIndex, Start: "+inf", Stop: strconv.FormatInt(time.Now().Add(-7*24*time.Hour).Unix(), 10), ByScore: true, Rev: true, Count: 10000,
	}).Result()
	if err != nil {
		return nil, err
	}
	pipe := c.rdb.Pipeline()
	commands := make([]*redis.MapStringStringCmd, 0, len(members))
	for _, member := range members {
		commands = append(commands, pipe.HGetAll(ctx, modelObservationPrefix+member))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	results := make([]service.ModelAllowlistObservation, 0, len(commands))
	for _, command := range commands {
		values := command.Val()
		var observation service.ModelAllowlistObservation
		if json.Unmarshal([]byte(values["data"]), &observation) != nil {
			continue
		}
		count, err := strconv.ParseInt(values["count"], 10, 64)
		if err != nil {
			continue
		}
		observation.Count = count
		results = append(results, observation)
	}
	return results, nil
}
