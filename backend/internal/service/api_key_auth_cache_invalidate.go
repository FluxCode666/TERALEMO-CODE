package service

import "context"

// InvalidateAuthCacheByHash accepts the stored SHA-256 index without hashing again.
func (s *APIKeyService) InvalidateAuthCacheByHash(ctx context.Context, keyHash string) {
	if keyHash == "" {
		return
	}
	s.deleteAuthCache(ctx, keyHash)
}

// InvalidateAuthCacheByUserID 清除用户相关的 API Key 认证缓存
func (s *APIKeyService) InvalidateAuthCacheByUserID(ctx context.Context, userID int64) {
	if userID <= 0 {
		return
	}
	keys, err := s.apiKeyRepo.ListKeyHashesByUserID(ctx, userID)
	if err != nil {
		return
	}
	s.deleteAuthCacheByHashes(ctx, keys)
}

// InvalidateAuthCacheByGroupID 清除分组相关的 API Key 认证缓存
func (s *APIKeyService) InvalidateAuthCacheByGroupID(ctx context.Context, groupID int64) {
	if groupID <= 0 {
		return
	}
	keys, err := s.apiKeyRepo.ListKeyHashesByGroupID(ctx, groupID)
	if err != nil {
		return
	}
	s.deleteAuthCacheByHashes(ctx, keys)
}

func (s *APIKeyService) deleteAuthCacheByHashes(ctx context.Context, keys []string) {
	if len(keys) == 0 {
		return
	}
	for _, key := range keys {
		if key == "" {
			continue
		}
		s.deleteAuthCache(ctx, key)
	}
}
