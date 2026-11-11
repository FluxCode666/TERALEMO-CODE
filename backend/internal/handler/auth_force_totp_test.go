//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type forceTotpHandlerSettingRepo struct{ service.SettingRepository }

func (repo *forceTotpHandlerSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if key == service.SettingKeyForceTotp {
		return "true", nil
	}
	return "", service.ErrSettingNotFound
}

func TestAuthLoginAndOAuthResponsesRequireTotpSetup(t *testing.T) {
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "test-secret", ExpireHour: 1}}
	settings := service.NewSettingService(&forceTotpHandlerSettingRepo{}, cfg)
	auth := service.NewAuthService(nil, nil, nil, nil, cfg, settings, nil, nil, nil, nil, nil, nil, nil)
	user := &service.User{ID: 8, Email: "user@example.com", Status: service.StatusActive}
	for _, method := range []string{"password/passkey", "oauth"} {
		t.Run(method, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest("POST", "/api/v1/auth/login", nil)
			if method == "oauth" {
				handler := &AuthHandler{authService: auth}
				handler.writeOAuthTokenPairResponse(ctx, &service.TokenPair{AccessToken: "token"}, user)
			} else {
				respondWithTokenPair(ctx, auth, user, nil)
			}
			require.Equal(t, 200, recorder.Code)
			var body map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			if data, ok := body["data"]; ok {
				require.NoError(t, json.Unmarshal(data, &body))
			}
			require.JSONEq(t, "true", string(body["requires_totp_setup"]))
			var returnedUser struct {
				RequiresTotpSetup bool `json:"requires_totp_setup"`
			}
			require.NoError(t, json.Unmarshal(body["user"], &returnedUser))
			require.True(t, returnedUser.RequiresTotpSetup)
		})
	}
}
