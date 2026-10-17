//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type enterpriseDelayedInvalidationCall struct {
	id  int64
	ctx context.Context
	err error
}

type enterpriseDelayedInvalidationStub struct {
	calls chan enterpriseDelayedInvalidationCall
}

func (s *enterpriseDelayedInvalidationStub) InvalidateAuthCacheByUserID(ctx context.Context, id int64) {
	s.calls <- enterpriseDelayedInvalidationCall{id: id, ctx: ctx, err: ctx.Err()}
}

func TestEnterpriseAuthCacheDelayedInvalidationSurvivesRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stub := &enterpriseDelayedInvalidationStub{calls: make(chan enterpriseDelayedInvalidationCall, 4)}
	ids := []int64{7, 7, 0, -1, 8}
	invalidateEnterpriseMembersAuthCache(ctx, ids, stub)
	for _, id := range []int64{7, 8} {
		call := <-stub.calls
		require.Equal(t, id, call.id)
		require.Same(t, ctx, call.ctx)
	}
	// The request ends and its caller may reuse the input slice while an old DB
	// authentication read is still able to refill the just-deleted cache.
	cancel()
	ids[0] = 999
	for _, id := range []int64{7, 8} {
		select {
		case call := <-stub.calls:
			require.Equal(t, id, call.id)
			require.NotSame(t, ctx, call.ctx)
			require.NoError(t, call.err)
			// The goroutine may already have canceled after the last call. Its
			// deadline still proves that it did not inherit the canceled request.
			deadline, ok := call.ctx.Deadline()
			require.True(t, ok)
			require.True(t, deadline.After(time.Now()))
		case <-time.After(enterpriseAuthCacheSecondInvalidationDelay + 3*time.Second):
			t.Fatal("missing delayed enterprise cache invalidation")
		}
	}
	require.Empty(t, stub.calls)
}

func TestEnterpriseAuthCacheInvalidationIgnoresMissingInvalidatorAndInvalidUsers(t *testing.T) {
	invalidateEnterpriseMembersAuthCache(context.Background(), []int64{1}, nil)
	stub := &enterpriseDelayedInvalidationStub{calls: make(chan enterpriseDelayedInvalidationCall, 1)}
	invalidateEnterpriseMembersAuthCache(context.Background(), []int64{0, -1}, stub)
	require.Empty(t, stub.calls)
}
