//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type creationReplayKeyRepo struct {
	service.APIKeyRepository
	key             *service.APIKey
	idLookups       int
	credentialReads int
}

func (r *creationReplayKeyRepo) GetByID(_ context.Context, id int64) (*service.APIKey, error) {
	r.idLookups++
	if r.key == nil || r.key.ID != id {
		return nil, service.ErrAPIKeyNotFound
	}
	return r.key, nil
}

func (r *creationReplayKeyRepo) GetByKeyForAuth(context.Context, string) (*service.APIKey, error) {
	r.credentialReads++
	return nil, service.ErrAPIKeyNotFound
}

func (r *creationReplayKeyRepo) UpdateLastUsed(context.Context, int64, time.Time) error {
	return nil
}

func TestCreationReplayKeepsGatewayAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, google := range []bool{false, true} {
		name := "standard"
		if google {
			name = "google"
		}
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				mutate func(*service.APIKey)
				userID int64
				keyID  int64
				status int
			}{
				{name: "active key without credential", status: http.StatusOK},
				{name: "foreign owner", userID: 8, status: http.StatusUnauthorized},
				{name: "missing key", keyID: 999, status: http.StatusUnauthorized},
				{name: "disabled key", mutate: func(k *service.APIKey) { k.Status = service.StatusAPIKeyDisabled }, status: http.StatusUnauthorized},
				{name: "expired key", mutate: func(k *service.APIKey) { expired := time.Now().Add(-time.Hour); k.ExpiresAt = &expired }, status: http.StatusForbidden},
				{name: "quota exhausted", mutate: func(k *service.APIKey) { k.Quota, k.QuotaUsed = 1, 1 }, status: http.StatusTooManyRequests},
				{name: "IP restriction", mutate: func(k *service.APIKey) { k.IPWhitelist = []string{"192.0.2.1"} }, status: http.StatusForbidden},
				{name: "disabled user", mutate: func(k *service.APIKey) { k.User.Status = service.StatusDisabled }, status: http.StatusUnauthorized},
				{name: "disabled group", mutate: func(k *service.APIKey) { k.Group.Status = service.StatusDisabled }, status: http.StatusForbidden},
				{name: "exclusive group grant revoked", mutate: func(k *service.APIKey) { k.Group.IsExclusive = true }, status: http.StatusForbidden},
			} {
				t.Run(tc.name, func(t *testing.T) {
					group := &service.Group{ID: 3, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard}
					key := &service.APIKey{ID: 9, UserID: 7, Status: service.StatusActive, GroupID: &group.ID, Group: group,
						User: &service.User{ID: 7, Status: service.StatusActive, Role: service.RoleUser, Balance: 100}}
					if tc.mutate != nil {
						tc.mutate(key)
					}
					repo := &creationReplayKeyRepo{key: key}
					cfg := &config.Config{RunMode: config.RunModeStandard}
					keys := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
					router := gin.New()
					if google {
						router.Use(APIKeyAuthWithSubscriptionGoogle(keys, nil, cfg))
					} else {
						router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(keys, nil, cfg)))
					}
					router.POST("/t", func(c *gin.Context) {
						authenticated, ok := GetAPIKeyFromContext(c)
						require.True(t, ok)
						require.Equal(t, key.ID, authenticated.ID)
						require.Empty(t, authenticated.Key)
						c.Status(http.StatusOK)
					})
					userID, keyID := tc.userID, tc.keyID
					if userID == 0 {
						userID = key.UserID
					}
					if keyID == 0 {
						keyID = key.ID
					}
					request := httptest.NewRequest(http.MethodPost, "/t", nil)
					request.RemoteAddr = "203.0.113.9:12345"
					request = request.WithContext(service.WithCreationStudioAPIKey(request.Context(), userID, keyID))
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					require.Equal(t, tc.status, response.Code, response.Body.String())
					require.Equal(t, 1, repo.idLookups)
					require.Zero(t, repo.credentialReads)
				})
			}
		})
	}
}

func TestExternalRequestsCannotSelectCreationReplayKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, google := range []bool{false, true} {
		for _, replayMarker := range []bool{false, true} {
			repo := &creationReplayKeyRepo{}
			cfg := &config.Config{RunMode: config.RunModeStandard}
			keys := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
			router := gin.New()
			if google {
				router.Use(APIKeyAuthWithSubscriptionGoogle(keys, nil, cfg))
			} else {
				router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(keys, nil, cfg)))
			}
			router.POST("/t", func(c *gin.Context) { t.Fatal("unauthenticated request reached gateway") })
			request := httptest.NewRequest(http.MethodPost, "/t?api_key_id=9&user_id=7", nil)
			request.Header.Set("X-Creation-Studio-Key-ID", "9")
			request.Header.Set("X-Creation-Studio-User-ID", "7")
			if replayMarker {
				request = request.WithContext(service.WithCreationStudioReplay(request.Context()))
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
			require.Zero(t, repo.idLookups)
			require.Zero(t, repo.credentialReads)
		}
	}
}
