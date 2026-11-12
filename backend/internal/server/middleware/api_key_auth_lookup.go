package middleware

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func lookupGatewayAPIKey(ctx context.Context, apiKeys *service.APIKeyService, credential string) (*service.APIKey, error) {
	userID, keyID, replay := service.CreationStudioAPIKey(ctx)
	if !replay {
		return apiKeys.GetByKey(ctx, credential)
	}
	key, err := apiKeys.GetByIDForAuth(ctx, keyID)
	if err != nil {
		return nil, err
	}
	if key == nil || key.ID != keyID || key.UserID != userID {
		return nil, service.ErrAPIKeyNotFound
	}
	return key, nil
}
