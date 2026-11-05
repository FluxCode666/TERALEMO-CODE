package service

import "context"

type accountTestStatusContextKey struct{}

// Scheduled batches capture the upstream status without changing manual SSE output.
func recordAccountTestUpstreamStatus(ctx context.Context, status int) {
	if destination, ok := ctx.Value(accountTestStatusContextKey{}).(*int); ok {
		*destination = status
	}
}
