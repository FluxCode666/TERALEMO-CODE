package repository

import (
	"context"
	"errors"
	"fmt"

	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/redis/go-redis/v9"
)

func (repo *userRepository) CountUsersWithoutTOTP(ctx context.Context) (int, error) {
	return clientFromContext(ctx, repo.client).User.Query().Where(dbuser.TotpEnabledEQ(false)).Count(ctx)
}

func (repo *userRepository) ListUsersWithoutTOTP(ctx context.Context, cursor int64, limit int) ([]int64, error) {
	return clientFromContext(ctx, repo.client).User.Query().Where(dbuser.TotpEnabledEQ(false), dbuser.IDGT(cursor)).Order(dbuser.ByID()).Limit(limit).IDs(ctx)
}

func (cache *refreshTokenCache) GetUserSessionVersion(ctx context.Context, userID int64) (string, error) {
	version, err := cache.rdb.Get(ctx, fmt.Sprintf("user_session_version:%d", userID)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return version, err
}

func (cache *refreshTokenCache) SetUserSessionVersion(ctx context.Context, userID int64, version string) error {
	return cache.rdb.Set(ctx, fmt.Sprintf("user_session_version:%d", userID), version, 0).Err()
}
