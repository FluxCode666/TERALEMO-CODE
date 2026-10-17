package repository

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const enterpriseQuotaCacheVersion = "2"

// Usage increments never refresh this deadline. A busy enterprise must still
// reload its limits and balance at least once per ordinary billing cache TTL.
const enterpriseBillingMaxFreshness = billingCacheTTL

type enterpriseBillingCache struct{ rdb *redis.Client }

func NewEnterpriseBillingCache(rdb *redis.Client) service.EnterpriseBillingCache {
	return &enterpriseBillingCache{rdb: rdb}
}

func enterpriseBillingBalanceKey(id int64) string {
	return "billing:ent_balance:" + strconv.FormatInt(id, 10)
}
func enterpriseBillingQuotaKey(scope string, id int64) string {
	return fmt.Sprintf("billing:ent_quota:%s:%d", scope, id)
}

func enterpriseBillingGenerationKey(key string) string { return key + ":generation" }
func enterpriseBillingLoadedAtKey(key string) string   { return key + ":loaded_at" }

var getEnterpriseBalanceScript = redis.NewScript(`
 local loaded=tonumber(redis.call('GET',KEYS[2]) or '0')
 if loaded==0 or tonumber(ARGV[1])-loaded>=tonumber(ARGV[2]) then return false end
 return redis.call('GET',KEYS[1])
`)

var setEnterpriseBalanceScript = redis.NewScript(`
 if ARGV[4]~='' and (redis.call('GET',KEYS[3]) or '0')~=ARGV[4] then return 0 end
 redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[3])
 redis.call('SET',KEYS[2],ARGV[2],'PX',ARGV[3])
 return 1
`)

var invalidateEnterpriseBillingScript = redis.NewScript(`
 redis.call('INCR',KEYS[2])
 redis.call('DEL',KEYS[1])
 if #KEYS>2 then redis.call('DEL',KEYS[3]) end
 return 1
`)

var deductEnterpriseBalanceScript = redis.NewScript(`
 redis.call('INCR',KEYS[2])
 if redis.call('EXISTS',KEYS[1])==0 then return 0 end
 redis.call('INCRBYFLOAT',KEYS[1],-tonumber(ARGV[1]))
 return 1
`)

func (c *enterpriseBillingCache) GetEnterpriseBalance(ctx context.Context, id int64) (float64, error) {
	key := enterpriseBillingBalanceKey(id)
	value, err := getEnterpriseBalanceScript.Run(ctx, c.rdb, []string{key, enterpriseBillingLoadedAtKey(key)}, time.Now().UnixMilli(), enterpriseBillingMaxFreshness.Milliseconds()).Float64()
	if err == nil && (math.IsNaN(value) || math.IsInf(value, 0)) {
		return 0, fmt.Errorf("invalid enterprise balance cache")
	}
	return value, err
}
func (c *enterpriseBillingCache) SetEnterpriseBalance(ctx context.Context, id int64, balance float64) error {
	key := enterpriseBillingBalanceKey(id)
	return setEnterpriseBalanceScript.Run(ctx, c.rdb, []string{key, enterpriseBillingLoadedAtKey(key), enterpriseBillingGenerationKey(key)}, balance, time.Now().UnixMilli(), jitteredTTL().Milliseconds(), "").Err()
}

func (c *enterpriseBillingCache) GetEnterpriseBalanceGeneration(ctx context.Context, id int64) (int64, error) {
	return c.getGeneration(ctx, enterpriseBillingBalanceKey(id))
}
func (c *enterpriseBillingCache) GetEnterpriseQuotaGeneration(ctx context.Context, scope string, id int64) (int64, error) {
	return c.getGeneration(ctx, enterpriseBillingQuotaKey(scope, id))
}
func (c *enterpriseBillingCache) getGeneration(ctx context.Context, key string) (int64, error) {
	generation, err := c.rdb.Get(ctx, enterpriseBillingGenerationKey(key)).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return generation, err
}
func (c *enterpriseBillingCache) SetEnterpriseBalanceIfGeneration(ctx context.Context, id int64, balance float64, generation int64) (bool, error) {
	key := enterpriseBillingBalanceKey(id)
	stored, err := setEnterpriseBalanceScript.Run(ctx, c.rdb, []string{key, enterpriseBillingLoadedAtKey(key), enterpriseBillingGenerationKey(key)}, balance, time.Now().UnixMilli(), jitteredTTL().Milliseconds(), generation).Int64()
	return stored == 1, err
}
func (c *enterpriseBillingCache) DeductEnterpriseBalance(ctx context.Context, id int64, amount float64) error {
	key := enterpriseBillingBalanceKey(id)
	return deductEnterpriseBalanceScript.Run(ctx, c.rdb, []string{key, enterpriseBillingGenerationKey(key)}, amount).Err()
}
func (c *enterpriseBillingCache) InvalidateEnterpriseBalance(ctx context.Context, id int64) error {
	key := enterpriseBillingBalanceKey(id)
	return invalidateEnterpriseBillingScript.Run(ctx, c.rdb, []string{key, enterpriseBillingGenerationKey(key), enterpriseBillingLoadedAtKey(key)}).Err()
}
func (c *enterpriseBillingCache) InvalidateEnterpriseQuota(ctx context.Context, scope string, id int64) error {
	key := enterpriseBillingQuotaKey(scope, id)
	return invalidateEnterpriseBillingScript.Run(ctx, c.rdb, []string{key, enterpriseBillingGenerationKey(key)}).Err()
}

func (c *enterpriseBillingCache) GetEnterpriseQuotas(ctx context.Context, enterpriseID int64, scopes []service.EnterpriseQuotaScope) ([]service.EnterpriseQuota, error) {
	pipe := c.rdb.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(scopes))
	for i, scope := range scopes {
		cmds[i] = pipe.HGetAll(ctx, enterpriseBillingQuotaKey(scope.ScopeType, scope.ScopeID))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	quotas := make([]service.EnterpriseQuota, 0, len(scopes))
	for i, cmd := range cmds {
		fields, err := cmd.Result()
		if err != nil {
			return nil, err
		}
		if len(fields) == 0 {
			continue
		}
		q, err := parseEnterpriseQuotaCache(fields)
		if err != nil {
			return nil, err
		}
		loaded, err := strconv.ParseInt(fields["loaded_at"], 10, 64)
		if err != nil || loaded <= 0 || time.Now().UnixMilli()-loaded >= enterpriseBillingMaxFreshness.Milliseconds() {
			continue
		}
		if q.EnterpriseID != enterpriseID || q.ScopeType != scopes[i].ScopeType || q.ScopeID != scopes[i].ScopeID {
			return nil, fmt.Errorf("enterprise quota cache scope mismatch")
		}
		quotas = append(quotas, *q)
	}
	return quotas, nil
}

func enterpriseCacheLimit(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'g', -1, 64)
}
func enterpriseCacheStart(v *time.Time) int64 {
	if v == nil {
		return 0
	}
	return v.Unix()
}
func (c *enterpriseBillingCache) SetEnterpriseQuota(ctx context.Context, q *service.EnterpriseQuota) error {
	_, err := c.setEnterpriseQuota(ctx, q, "")
	return err
}

func (c *enterpriseBillingCache) SetEnterpriseQuotaIfGeneration(ctx context.Context, q *service.EnterpriseQuota, generation int64) (bool, error) {
	return c.setEnterpriseQuota(ctx, q, strconv.FormatInt(generation, 10))
}

var setEnterpriseQuotaScript = redis.NewScript(`
 if ARGV[1]~='' and (redis.call('GET',KEYS[2]) or '0')~=ARGV[1] then return 0 end
 redis.call('DEL',KEYS[1])
 for i=3,#ARGV,2 do redis.call('HSET',KEYS[1],ARGV[i],ARGV[i+1]) end
 redis.call('PEXPIRE',KEYS[1],ARGV[2])
 return 1
`)

func (c *enterpriseBillingCache) setEnterpriseQuota(ctx context.Context, q *service.EnterpriseQuota, generation string) (bool, error) {
	if q == nil {
		return false, fmt.Errorf("nil enterprise quota")
	}
	fields := map[string]any{"version": enterpriseQuotaCacheVersion, "id": q.ID, "enterprise_id": q.EnterpriseID, "scope_type": q.ScopeType, "scope_id": q.ScopeID,
		"daily_limit": enterpriseCacheLimit(q.DailyLimitUSD), "weekly_limit": enterpriseCacheLimit(q.WeeklyLimitUSD), "monthly_limit": enterpriseCacheLimit(q.MonthlyLimitUSD), "total_limit": enterpriseCacheLimit(q.TotalLimitUSD),
		"daily_usage": q.DailyUsageUSD, "weekly_usage": q.WeeklyUsageUSD, "monthly_usage": q.MonthlyUsageUSD, "total_usage": q.TotalUsageUSD,
		"daily_start": enterpriseCacheStart(q.DailyWindowStart), "weekly_start": enterpriseCacheStart(q.WeeklyWindowStart), "monthly_start": enterpriseCacheStart(q.MonthlyWindowStart), "loaded_at": time.Now().UnixMilli()}
	key := enterpriseBillingQuotaKey(q.ScopeType, q.ScopeID)
	args := []any{generation, jitteredTTL().Milliseconds()}
	for name, value := range fields {
		args = append(args, name, value)
	}
	stored, err := setEnterpriseQuotaScript.Run(ctx, c.rdb, []string{key, enterpriseBillingGenerationKey(key)}, args...).Int64()
	return stored == 1, err
}

func parseEnterpriseQuotaCache(fields map[string]string) (*service.EnterpriseQuota, error) {
	if fields["version"] != enterpriseQuotaCacheVersion {
		return nil, fmt.Errorf("invalid enterprise quota cache version")
	}
	q := &service.EnterpriseQuota{ScopeType: fields["scope_type"]}
	for name, target := range map[string]*int64{"id": &q.ID, "enterprise_id": &q.EnterpriseID, "scope_id": &q.ScopeID} {
		v, err := strconv.ParseInt(fields[name], 10, 64)
		if err != nil {
			return nil, err
		}
		*target = v
	}
	for name, target := range map[string]**float64{"daily_limit": &q.DailyLimitUSD, "weekly_limit": &q.WeeklyLimitUSD, "monthly_limit": &q.MonthlyLimitUSD, "total_limit": &q.TotalLimitUSD} {
		str, exists := fields[name]
		if !exists {
			return nil, fmt.Errorf("enterprise quota cache missing %s", name)
		}
		if str == "" {
			continue
		}
		v, err := strconv.ParseFloat(str, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return nil, fmt.Errorf("invalid enterprise quota %s", name)
		}
		*target = &v
	}
	for name, target := range map[string]*float64{"daily_usage": &q.DailyUsageUSD, "weekly_usage": &q.WeeklyUsageUSD, "monthly_usage": &q.MonthlyUsageUSD, "total_usage": &q.TotalUsageUSD} {
		v, err := strconv.ParseFloat(fields[name], 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return nil, fmt.Errorf("invalid enterprise quota %s", name)
		}
		*target = v
	}
	for name, target := range map[string]**time.Time{"daily_start": &q.DailyWindowStart, "weekly_start": &q.WeeklyWindowStart, "monthly_start": &q.MonthlyWindowStart} {
		v, err := strconv.ParseInt(fields[name], 10, 64)
		if err != nil {
			return nil, err
		}
		if v != 0 {
			t := time.Unix(v, 0)
			*target = &t
		}
	}
	return q, nil
}

var incrementEnterpriseQuotaScript = redis.NewScript(`
 redis.call('INCR',KEYS[2])
 if redis.call('EXISTS',KEYS[1])==0 then return 0 end
 local loaded=tonumber(redis.call('HGET',KEYS[1],'loaded_at') or '0')
 if loaded==0 or tonumber(ARGV[5])-loaded>=tonumber(ARGV[6]) then
  redis.call('DEL',KEYS[1]); return 0
 end
 for i,name in ipairs({'daily','weekly','monthly'}) do
  local start=tonumber(redis.call('HGET',KEYS[1],name..'_start') or '0')
  if start~=tonumber(ARGV[i+1]) then redis.call('DEL',KEYS[1]); return 0 end
 end
 local cost=tonumber(ARGV[1])
 redis.call('HINCRBYFLOAT',KEYS[1],'daily_usage',cost)
 redis.call('HINCRBYFLOAT',KEYS[1],'weekly_usage',cost)
 redis.call('HINCRBYFLOAT',KEYS[1],'monthly_usage',cost)
 redis.call('HINCRBYFLOAT',KEYS[1],'total_usage',cost)
 return 1
`)

func (c *enterpriseBillingCache) IncrementEnterpriseQuotaUsage(ctx context.Context, scope string, id int64, cost float64, now time.Time) error {
	return c.IncrementEnterpriseQuotaUsageForWindows(ctx, scope, id, cost, service.EnterpriseBillingWindowStarts{Daily: timezone.StartOfDay(now), Weekly: timezone.StartOfWeek(now), Monthly: timezone.StartOfMonth(now)})
}

func (c *enterpriseBillingCache) IncrementEnterpriseQuotaUsageForWindows(ctx context.Context, scope string, id int64, cost float64, windows service.EnterpriseBillingWindowStarts) error {
	key := enterpriseBillingQuotaKey(scope, id)
	return incrementEnterpriseQuotaScript.Run(ctx, c.rdb, []string{key, enterpriseBillingGenerationKey(key)}, cost, windows.Daily.Unix(), windows.Weekly.Unix(), windows.Monthly.Unix(), time.Now().UnixMilli(), enterpriseBillingMaxFreshness.Milliseconds()).Err()
}

var _ service.EnterpriseBillingRuntimeCache = (*enterpriseBillingCache)(nil)
var _ service.EnterpriseBillingGenerationCache = (*enterpriseBillingCache)(nil)
var _ service.EnterpriseQuotaWindowCache = (*enterpriseBillingCache)(nil)

// RenewInflightScope reuses the balance renewal Lua with enterprise scope keys.
func (c *billingCache) RenewInflightScope(ctx context.Context, scope, requestID string, ttl time.Duration) (bool, error) {
	zkey, hkey := billingInflightScopeKeys(scope)
	ttlMs := ttl.Milliseconds()
	if ttlMs <= 0 {
		ttlMs = 1
	}
	now := time.Now().UnixMilli()
	n, err := renewInflightBalanceScript.Run(ctx, c.rdb, []string{zkey, hkey}, requestID, now+ttlMs, ttlMs, now).Int64()
	return n == 1, err
}
