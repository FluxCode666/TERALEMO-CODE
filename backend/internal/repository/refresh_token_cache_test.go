package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type refreshTokenCleanupHook struct {
	scanCalls   int
	deleteCalls int
	scan        func(*redis.ScanCmd)
}

func (h *refreshTokenCleanupHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *refreshTokenCleanupHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		switch cmd.Name() {
		case "keys":
			return fmt.Errorf("keys is not permitted for refresh cleanup")
		case "scan":
			h.scanCalls++
			if h.scan != nil {
				if scan, ok := cmd.(*redis.ScanCmd); ok {
					h.scan(scan)
					return scan.Err()
				}
			}
		case "del":
			h.deleteCalls++
			if len(cmd.Args())-1 > refreshTokenDeleteBatch {
				return fmt.Errorf("refresh cleanup exceeded delete batch limit")
			}
		}
		return next(ctx, cmd)
	}
}

func (h *refreshTokenCleanupHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func TestDeleteAllRefreshTokensBatchesAndPreservesOtherKeys(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()
	for _, prefix := range []string{refreshTokenKeyPrefix, userRefreshTokensPrefix, tokenFamilyPrefix} {
		for i := range refreshTokenDeleteBatch*2 + 7 {
			require.NoError(t, server.Set(fmt.Sprintf("%s%d", prefix, i), "value"))
		}
	}
	require.NoError(t, server.Set("other:cache", "value"))
	hook := &refreshTokenCleanupHook{}
	client.AddHook(hook)
	cache := &refreshTokenCache{rdb: client}
	require.NoError(t, cache.DeleteAllRefreshTokens(t.Context()))
	require.Equal(t, []string{"other:cache"}, server.Keys())
	require.Equal(t, 3, hook.scanCalls)
	require.Equal(t, 9, hook.deleteCalls)
	require.NoError(t, cache.DeleteAllRefreshTokens(t.Context()))
}

func TestDeleteAllRefreshTokensContinuesScanCursor(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()
	hook := &refreshTokenCleanupHook{scan: func(cmd *redis.ScanCmd) {
		cursor, ok := cmd.Args()[1].(uint64)
		require.True(t, ok)
		if cursor == 0 {
			cmd.SetVal([]string{"refresh_token:first"}, 17)
		} else {
			require.Equal(t, uint64(17), cursor)
			cmd.SetVal([]string{"refresh_token:second"}, 0)
		}
	}}
	client.AddHook(hook)
	require.NoError(t, (&refreshTokenCache{rdb: client}).DeleteAllRefreshTokens(t.Context()))
	require.Equal(t, 6, hook.scanCalls)
	require.Equal(t, 6, hook.deleteCalls)
}

func TestDeleteAllRefreshTokensReturnsRedisError(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()
	server.SetError("ERR unavailable")
	require.ErrorContains(t, (&refreshTokenCache{rdb: client}).DeleteAllRefreshTokens(t.Context()), "scan refresh token keys")
}
