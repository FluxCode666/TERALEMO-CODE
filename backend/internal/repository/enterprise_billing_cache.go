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

const enterpriseQuotaCacheVersion = "1"

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

func (c *enterpriseBillingCache) GetEnterpriseBalance(ctx context.Context, id int64) (float64, error) {
	value, err := c.rdb.Get(ctx, enterpriseBillingBalanceKey(id)).Float64()
	if err == nil && (math.IsNaN(value) || math.IsInf(value, 0)) {
		return 0, fmt.Errorf("invalid enterprise balance cache")
	}
	return value, err
}
func (c *enterpriseBillingCache) SetEnterpriseBalance(ctx context.Context, id int64, balance float64) error {
	return c.rdb.Set(ctx, enterpriseBillingBalanceKey(id), balance, jitteredTTL()).Err()
}
func (c *enterpriseBillingCache) DeductEnterpriseBalance(ctx context.Context, id int64, amount float64) error {
	return deductBalanceScript.Run(ctx, c.rdb, []string{enterpriseBillingBalanceKey(id)}, amount, int(jitteredTTL().Seconds())).Err()
}
func (c *enterpriseBillingCache) InvalidateEnterpriseBalance(ctx context.Context, id int64) error {
	return c.rdb.Del(ctx, enterpriseBillingBalanceKey(id)).Err()
}
func (c *enterpriseBillingCache) InvalidateEnterpriseQuota(ctx context.Context, scope string, id int64) error {
	return c.rdb.Del(ctx, enterpriseBillingQuotaKey(scope, id)).Err()
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
	if q == nil {
		return fmt.Errorf("nil enterprise quota")
	}
	fields := map[string]any{"version": enterpriseQuotaCacheVersion, "id": q.ID, "enterprise_id": q.EnterpriseID, "scope_type": q.ScopeType, "scope_id": q.ScopeID,
		"daily_limit": enterpriseCacheLimit(q.DailyLimitUSD), "weekly_limit": enterpriseCacheLimit(q.WeeklyLimitUSD), "monthly_limit": enterpriseCacheLimit(q.MonthlyLimitUSD), "total_limit": enterpriseCacheLimit(q.TotalLimitUSD),
		"daily_usage": q.DailyUsageUSD, "weekly_usage": q.WeeklyUsageUSD, "monthly_usage": q.MonthlyUsageUSD, "total_usage": q.TotalUsageUSD,
		"daily_start": enterpriseCacheStart(q.DailyWindowStart), "weekly_start": enterpriseCacheStart(q.WeeklyWindowStart), "monthly_start": enterpriseCacheStart(q.MonthlyWindowStart)}
	key := enterpriseBillingQuotaKey(q.ScopeType, q.ScopeID)
	pipe := c.rdb.TxPipeline()
	pipe.HSet(ctx, key, fields)
	pipe.Expire(ctx, key, jitteredTTL())
	_, err := pipe.Exec(ctx)
	return err
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
 if redis.call('EXISTS',KEYS[1])==0 then return 0 end
 local cost=tonumber(ARGV[1])
 local function increment_window(name,current)
  local start=tonumber(redis.call('HGET',KEYS[1],name..'_start') or '0')
  if start==0 or start<current then
   redis.call('HSET',KEYS[1],name..'_usage',cost,name..'_start',current)
  else
   redis.call('HINCRBYFLOAT',KEYS[1],name..'_usage',cost)
  end
 end
 increment_window('daily',tonumber(ARGV[2]))
 increment_window('weekly',tonumber(ARGV[3]))
 increment_window('monthly',tonumber(ARGV[4]))
 redis.call('HINCRBYFLOAT',KEYS[1],'total_usage',cost)
 redis.call('EXPIRE',KEYS[1],ARGV[5])
 return 1
`)

func (c *enterpriseBillingCache) IncrementEnterpriseQuotaUsage(ctx context.Context, scope string, id int64, cost float64, now time.Time) error {
	return incrementEnterpriseQuotaScript.Run(ctx, c.rdb, []string{enterpriseBillingQuotaKey(scope, id)}, cost, timezone.StartOfDay(now).Unix(), timezone.StartOfWeek(now).Unix(), timezone.StartOfMonth(now).Unix(), int(jitteredTTL().Seconds())).Err()
}

var _ service.EnterpriseBillingRuntimeCache = (*enterpriseBillingCache)(nil)

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
