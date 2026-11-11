//go:build unit

package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type wsModelAllowlistRepo struct {
	service.SettingRepository
	values map[string]string
}

func (repo *wsModelAllowlistRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := map[string]string{}
	for _, key := range keys {
		if value, exists := repo.values[key]; exists {
			values[key] = value
		}
	}
	return values, nil
}

func (repo *wsModelAllowlistRepo) Set(_ context.Context, key, value string) error {
	repo.values[key] = value
	return nil
}

func TestOpenAIWSModelAllowlistRechecksPolicyBetweenTurns(t *testing.T) {
	ctx := context.Background()
	settings := service.NewSettingService(&wsModelAllowlistRepo{values: map[string]string{}}, &config.Config{})
	require.NoError(t, settings.SaveModelAllowlistEntries(ctx, service.ModelAllowlistSettings{Entries: []string{"192.0.2.1"}}))
	keys := &service.APIKeyService{}
	keys.SetModelAllowlistSettings(settings)
	handler := &OpenAIGatewayHandler{apiKeyService: keys}
	key := &service.APIKey{ID: 3, User: &service.User{ID: 4}}
	require.NoError(t, handler.checkOpenAIWSModelRequestIP(ctx, "203.0.113.9", key))
	require.NoError(t, settings.SetModelAllowlistEnabled(ctx, true))
	require.ErrorContains(t, handler.checkOpenAIWSModelRequestIP(ctx, "203.0.113.9", key), "203.0.113.9")
	require.NoError(t, handler.checkOpenAIWSModelRequestIP(ctx, "192.0.2.1", key))
	require.NoError(t, settings.SetModelAllowlistEnabled(ctx, false))
	require.NoError(t, handler.checkOpenAIWSModelRequestIP(ctx, "203.0.113.9", key))
}
