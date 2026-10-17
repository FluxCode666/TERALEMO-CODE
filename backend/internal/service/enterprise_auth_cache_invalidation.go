package service

import (
	"context"
	"time"
)

const (
	enterpriseAuthCacheSecondInvalidationDelay = 2 * time.Second
	enterpriseAuthCacheInvalidationTimeout     = 5 * time.Second
)

// invalidateEnterpriseMembersAuthCache also removes an old snapshot that a
// concurrent authentication read may refill after the first invalidation.
func invalidateEnterpriseMembersAuthCache(ctx context.Context, userIDs []int64, invalidator enterpriseManagementAuthInvalidator) {
	if invalidator == nil {
		return
	}
	seen := make(map[int64]bool, len(userIDs))
	ids := make([]int64, 0, len(userIDs))
	for _, id := range userIDs {
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
			invalidator.InvalidateAuthCacheByUserID(ctx, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	background := context.WithoutCancel(ctx)
	go func() {
		time.Sleep(enterpriseAuthCacheSecondInvalidationDelay)
		delayed, cancel := context.WithTimeout(background, enterpriseAuthCacheInvalidationTimeout)
		defer cancel()
		for _, id := range ids {
			invalidator.InvalidateAuthCacheByUserID(delayed, id)
		}
	}()
}
