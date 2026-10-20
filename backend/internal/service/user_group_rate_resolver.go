package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	gocache "github.com/patrickmn/go-cache"
	"golang.org/x/sync/singleflight"
)

var userGroupRateCacheGeneration atomic.Uint64

// Each resolver flushes on its next read. The generation also fences in-flight loads.
func flushUserGroupRateCaches() { userGroupRateCacheGeneration.Add(1) }

type userGroupRateReader interface {
	GetByUserAndGroup(context.Context, int64, int64) (*float64, error)
}

func resolveEffectiveUserGroupRate(ctx context.Context, repo userGroupRateReader, userID, groupID int64) (*float64, error) {
	if repo == nil {
		return nil, nil
	}
	if effective, ok := repo.(EffectiveUserGroupRateRepository); ok {
		return effective.GetEffectiveByUserAndGroup(ctx, userID, groupID)
	}
	return repo.GetByUserAndGroup(ctx, userID, groupID)
}

func resolveEffectiveUserGroupRates(ctx context.Context, repo UserGroupRateRepository, userID int64) (map[int64]float64, error) {
	if repo == nil {
		return nil, nil
	}
	if effective, ok := repo.(EffectiveUserGroupRateRepository); ok {
		return effective.GetEffectiveByUserID(ctx, userID)
	}
	return repo.GetByUserID(ctx, userID)
}

type userGroupRateResolver struct {
	repo         UserGroupRateRepository
	cache        *gocache.Cache
	cacheTTL     time.Duration
	sf           *singleflight.Group
	logComponent string
	cacheMu      sync.RWMutex
	generation   uint64
}

func newUserGroupRateResolver(repo UserGroupRateRepository, cache *gocache.Cache, cacheTTL time.Duration, sf *singleflight.Group, logComponent string) *userGroupRateResolver {
	if cacheTTL <= 0 {
		cacheTTL = defaultUserGroupRateCacheTTL
	}
	if cacheTTL > defaultUserGroupRateCacheTTL {
		cacheTTL = defaultUserGroupRateCacheTTL
	}
	if cache == nil {
		cache = gocache.New(cacheTTL, time.Minute)
	}
	if logComponent == "" {
		logComponent = "service.gateway"
	}
	if sf == nil {
		sf = &singleflight.Group{}
	}

	return &userGroupRateResolver{
		repo:         repo,
		cache:        cache,
		cacheTTL:     cacheTTL,
		sf:           sf,
		logComponent: logComponent,
		generation:   userGroupRateCacheGeneration.Load(),
	}
}

func (r *userGroupRateResolver) Resolve(ctx context.Context, userID, groupID int64, groupDefaultMultiplier float64) float64 {
	if r == nil || userID <= 0 || groupID <= 0 {
		return groupDefaultMultiplier
	}

	key := fmt.Sprintf("%d:%d", userID, groupID)
	r.cacheMu.RLock()
	generation := userGroupRateCacheGeneration.Load()
	if r.generation != generation {
		r.cacheMu.RUnlock()
		r.cacheMu.Lock()
		generation = userGroupRateCacheGeneration.Load()
		if r.generation != generation {
			if r.cache != nil {
				r.cache.Flush()
			}
			r.generation = generation
		}
		r.cacheMu.Unlock()
		r.cacheMu.RLock()
		generation = r.generation
	}
	if r.cache != nil {
		if cached, ok := r.cache.Get(key); ok {
			if multiplier, castOK := cached.(float64); castOK {
				r.cacheMu.RUnlock()
				userGroupRateCacheHitTotal.Add(1)
				return multiplier
			}
		}
	}
	r.cacheMu.RUnlock()
	if r.repo == nil {
		return groupDefaultMultiplier
	}
	userGroupRateCacheMissTotal.Add(1)

	value, err, shared := r.sf.Do(fmt.Sprintf("%d:%s", generation, key), func() (any, error) {
		r.cacheMu.RLock()
		if r.cache != nil {
			if cached, ok := r.cache.Get(key); ok && r.generation == generation {
				if multiplier, castOK := cached.(float64); castOK {
					r.cacheMu.RUnlock()
					userGroupRateCacheHitTotal.Add(1)
					return multiplier, nil
				}
			}
		}
		r.cacheMu.RUnlock()

		userGroupRateCacheLoadTotal.Add(1)
		userRate, repoErr := resolveEffectiveUserGroupRate(ctx, r.repo, userID, groupID)
		if repoErr != nil {
			return nil, repoErr
		}

		multiplier := groupDefaultMultiplier
		if userRate != nil {
			multiplier = *userRate
		}
		r.cacheMu.Lock()
		if r.cache != nil && r.generation == generation && userGroupRateCacheGeneration.Load() == generation {
			r.cache.Set(key, multiplier, r.cacheTTL)
		}
		r.cacheMu.Unlock()
		return multiplier, nil
	})
	if shared {
		userGroupRateCacheSFSharedTotal.Add(1)
	}
	if err != nil {
		userGroupRateCacheFallbackTotal.Add(1)
		logger.LegacyPrintf(r.logComponent, "get user group rate failed, fallback to group default: user=%d group=%d err=%v", userID, groupID, err)
		return groupDefaultMultiplier
	}

	multiplier, ok := value.(float64)
	if !ok {
		userGroupRateCacheFallbackTotal.Add(1)
		return groupDefaultMultiplier
	}
	return multiplier
}
