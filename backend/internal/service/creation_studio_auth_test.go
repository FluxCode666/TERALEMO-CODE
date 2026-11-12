//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreationStudioAPIKeyReference(t *testing.T) {
	for _, ctx := range []context.Context{nil, context.Background(), WithCreationStudioReplay(context.Background())} {
		_, _, ok := CreationStudioAPIKey(ctx)
		require.False(t, ok)
	}
	ctx := WithCreationStudioAPIKey(context.Background(), 7, 9)
	userID, keyID, ok := CreationStudioAPIKey(ctx)
	require.True(t, ok)
	require.Equal(t, int64(7), userID)
	require.Equal(t, int64(9), keyID)
	require.True(t, IsCreationStudioReplay(ctx))
	for _, ids := range [][2]int64{{0, 9}, {7, 0}, {-1, 9}, {7, -1}} {
		_, _, ok := CreationStudioAPIKey(WithCreationStudioAPIKey(context.Background(), ids[0], ids[1]))
		require.False(t, ok)
	}
}
