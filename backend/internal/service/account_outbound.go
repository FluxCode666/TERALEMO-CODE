package service

import (
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/outbound"
)

func validateAccountOutboundURLs(credentials map[string]any) error {
	for key, value := range credentials {
		if key == "base_url" || strings.HasSuffix(key, "_base_url") {
			if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
				if err := outbound.ValidateLiteralURL(raw); err != nil {
					return infraerrors.BadRequest("INVALID_UPSTREAM_URL", err.Error())
				}
			}
		}
		if key == "api_base_urls" {
			if urls, ok := value.(map[string]any); ok {
				for _, value := range urls {
					if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
						if err := outbound.ValidateLiteralURL(raw); err != nil {
							return infraerrors.BadRequest("INVALID_UPSTREAM_URL", err.Error())
						}
					}
				}
			}
		}
	}
	return nil
}
