package service

import (
	"context"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/imagecompat"
)

func (s *RateLimitService) handleImagePlatformAccountError(ctx context.Context, account *Account, status int, headers http.Header, body []byte) bool {
	typed := sanitizeImageAccountError(account, imagecompat.ConvertUpstreamError(imagecompat.Protocol(account.ImageProtocol()), status, body))
	if !typed.Retryable {
		return false
	}
	if status == 401 || (status == 400 && typed.Code == "upstream_auth_failed") {
		s.handleAuthError(ctx, account, typed.Message)
		return true
	}
	if status == 402 || status == 403 || (status == 400 && typed.Retryable) {
		until := time.Now().Add(5 * time.Minute)
		s.notifyAccountSchedulingBlocked(account, until, "image_account_cooldown")
		if s.accountRepo != nil {
			_ = s.accountRepo.SetTempUnschedulable(ctx, account.ID, until, typed.Message)
		}
		return true
	}
	if status == 429 {
		s.handle429(ctx, account, headers, typed.OpenAIErrorBody())
	} else if status >= 500 {
		s.handle529(ctx, account)
	}
	return false
}
