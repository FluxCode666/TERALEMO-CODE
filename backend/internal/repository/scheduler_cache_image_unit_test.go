//go:build unit

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Feed catalog consumers the same deserialized candidate projection that
// scheduling and reservation use, rather than a full-credential fixture.
type imageSnapshotCatalogRepo struct {
	service.AccountRepository
	snapshot *service.SchedulerSnapshotService
}

func (r imageSnapshotCatalogRepo) ListSchedulableByGroupID(ctx context.Context, id int64) ([]service.Account, error) {
	accounts, _, err := r.snapshot.ListSchedulableAccounts(ctx, &id, service.PlatformImage, false)
	return accounts, err
}

func (r imageSnapshotCatalogRepo) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	return r.snapshot.GetAccount(ctx, id)
}

func TestSchedulerCacheImageProtocolConsumers(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	const gid int64 = 17
	account := service.Account{ID: 4, Platform: service.PlatformImage, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{gid},
		Credentials: map[string]any{"image_protocol": "gemini", "base_url": "https://relay.example/v1", "api_key": "test-secret", "unneeded": "discard"}}
	bucket := service.SchedulerBucket{GroupID: gid, Platform: service.PlatformImage, Mode: service.SchedulerModeSingle}
	token, err := cache.CaptureBucketWriteToken(ctx, bucket)
	require.NoError(t, err)
	require.NoError(t, cache.SetSnapshot(ctx, bucket, token, []service.Account{account}))
	candidates, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Len(t, candidates, 1)
	require.Equal(t, "gemini", candidates[0].ImageProtocol())
	require.Equal(t, account.ImageBaseURL(), candidates[0].ImageBaseURL())
	require.Equal(t, "test-secret", candidates[0].GetCredential("api_key"))
	require.NotContains(t, candidates[0].Credentials, "unneeded", "must exercise the filtered projection")
	require.Empty(t, candidates[0].GetModelMapping())
	require.True(t, candidates[0].IsModelSupported("gemini-2.5-flash-image"))
	require.Equal(t, service.DefaultImageModelIDs("gemini"), candidates[0].ImageModelIDs())

	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	snapshot := service.NewSchedulerSnapshotService(cache, nil, nil, nil, cfg)
	repo := imageSnapshotCatalogRepo{snapshot: snapshot}
	gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, snapshot, nil, service.NewBillingService(cfg, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	selection, _, err := gateway.SelectAccountWithSchedulerForImages(ctx, new(gid), "", "gemini-2.5-flash-image", nil, service.OpenAIImagesCapabilityNative, service.PlatformImage)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, account.ID, selection.Account.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
	p1, p2, p4 := .01, .02, .04
	key := &service.APIKey{ID: 9, UserID: 7, Status: service.StatusActive, GroupID: new(gid), User: &service.User{ID: 7},
		Group: &service.Group{ID: gid, Platform: service.PlatformImage, Status: service.StatusActive, RateMultiplier: 1, AllowImageGeneration: true, ImagePrice1K: &p1, ImagePrice2K: &p2, ImagePrice4K: &p4}}
	estimate := gateway.EstimateCreationImageCost(ctx, key, 7, "gemini-2.5-flash-image", "1536x1024", 2, 100)
	require.True(t, estimate.Priced)
	require.Equal(t, "1K", estimate.Tier)
	require.InDelta(t, .02, estimate.Amount, 1e-9)
	require.InDelta(t, .01, estimate.Amount/2, 1e-9)
	amount, priced := gateway.EstimateInflightReservation(ctx, key, service.InflightEstimateRequest{Model: "gemini-2.5-flash-image", ImageSize: "1536x1024", Units: 2, Kind: service.InflightEstimateImage})
	require.True(t, priced)
	require.InDelta(t, .02, amount, 1e-9)

	options, err := gateway.CreationOptions(ctx, key, nil)
	require.NoError(t, err)
	models := make([]string, 0, len(options.Models))
	for _, model := range options.Models {
		models = append(models, model.Model)
	}
	require.ElementsMatch(t, service.DefaultImageModelIDs("gemini"), models)
	modelsGateway := service.NewGatewayService(repo, nil, nil, nil, nil, nil, nil, nil, cfg, snapshot, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.ElementsMatch(t, service.DefaultImageModelIDs("gemini"), modelsGateway.GetAvailableModels(ctx, new(gid), service.PlatformImage))
}
