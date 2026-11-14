//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestModelAllowlistObservationAggregationAndTTL(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := &apiKeyCache{rdb: client}
	ctx := context.Background()
	observation := service.ModelAllowlistObservation{IP: "203.0.113.1", UserID: 2, KeyID: 3, KeyName: "Customer", LastSeen: time.Now().Unix()}
	require.NoError(t, store.RecordModelAllowlistObservation(ctx, observation))
	observation.LastSeen++
	require.NoError(t, store.RecordModelAllowlistObservation(ctx, observation))
	observation.KeyID = 4
	require.NoError(t, store.RecordModelAllowlistObservation(ctx, observation))
	results, err := store.ListModelAllowlistObservations(ctx)
	require.NoError(t, err)
	require.Len(t, results, 2)
	counts := map[int64]int64{}
	for _, result := range results {
		counts[result.KeyID] = result.Count
		require.Equal(t, observation.LastSeen, result.LastSeen)
	}
	require.Equal(t, int64(2), counts[3])
	require.Equal(t, int64(1), counts[4])
	server.FastForward(7*24*time.Hour + time.Second)
	results, err = store.ListModelAllowlistObservations(ctx)
	require.NoError(t, err)
	require.Empty(t, results)
}
