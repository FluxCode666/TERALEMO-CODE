//go:build unit

package service

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestAdminBulkVideoAccountCapabilitiesNormalizePersistedDelta(t *testing.T) {
	account := videoCapabilitiesTestAccount(map[string]any{"video_protocol": VideoProtocolMiniMax, "video_max_duration_seconds": 30})
	account.ID = 1
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{account}}
	svc := &adminServiceImpl{accountRepo: repo}
	_, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Credentials: map[string]any{
		"video_resolutions": []any{"1080P", "480P", "1080p"}, "video_min_duration_seconds": float64(4),
	}})
	require.NoError(t, err)
	require.Equal(t, 1, repo.bulkUpdateCalls)
	require.Equal(t, []string{"480p", "1080p"}, repo.lastBulkUpdate.Credentials["video_resolutions"])
	require.Equal(t, 4, repo.lastBulkUpdate.Credentials["video_min_duration_seconds"])
	require.NotContains(t, repo.lastBulkUpdate.Credentials, "video_max_duration_seconds", "do not persist a derived/default bound in the bulk delta")
	require.NotContains(t, account.Credentials, "video_resolutions", "validation must not mutate scheduler account credentials")
}

func TestAdminBulkVideoAccountCapabilitiesValidateMergedBounds(t *testing.T) {
	account := videoCapabilitiesTestAccount(map[string]any{"video_max_duration_seconds": 10})
	account.ID = 1
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{account}}
	svc := &adminServiceImpl{accountRepo: repo}
	_, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Credentials: map[string]any{"video_min_duration_seconds": 20}})
	require.Error(t, err)
	require.Equal(t, "INVALID_VIDEO_ACCOUNT", infraerrors.Reason(err))
	require.Equal(t, 400, infraerrors.Code(err))
	require.Zero(t, repo.bulkUpdateCalls)
}
