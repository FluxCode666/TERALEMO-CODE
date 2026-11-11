//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type sessionRevocationUserRepo struct {
	service.UserRepository
	user *service.User
}

func (repo *sessionRevocationUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return repo.user, nil
}

func TestForceTotpSharedSessionRevocationPersistsAndBlocksRefresh(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &refreshTokenCache{rdb: client}
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "session-secret", ExpireHour: 1, RefreshTokenExpireDays: 1}}
	user := &service.User{ID: 5, Email: "user@example.com", Status: service.StatusActive}
	repo := &sessionRevocationUserRepo{user: user}
	auth := service.NewAuthService(nil, repo, nil, cache, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()
	pair, err := auth.GenerateTokenPair(ctx, user, "")
	require.NoError(t, err)
	claims, err := auth.ValidateToken(pair.AccessToken)
	require.NoError(t, err)
	require.NoError(t, auth.RevokeAllUserTokens(ctx, user.ID))
	otherInstance := service.NewAuthService(nil, repo, nil, &refreshTokenCache{rdb: client}, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	require.ErrorIs(t, otherInstance.ValidateUserSession(ctx, user.ID, claims.SessionVersion), service.ErrTokenRevoked)
	_, err = otherInstance.RefreshTokenPair(ctx, pair.RefreshToken)
	require.ErrorIs(t, err, service.ErrRefreshTokenInvalid)
	newPair, err := otherInstance.GenerateTokenPair(ctx, user, "")
	require.NoError(t, err)
	newClaims, err := otherInstance.ValidateToken(newPair.AccessToken)
	require.NoError(t, err)
	require.NoError(t, otherInstance.ValidateUserSession(ctx, user.ID, newClaims.SessionVersion))
	server.FastForward(365 * 24 * time.Hour)
	require.ErrorIs(t, otherInstance.ValidateUserSession(ctx, user.ID, claims.SessionVersion), service.ErrTokenRevoked)
}
