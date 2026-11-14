package repository

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRepositoryCreationReplayAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	owner := mustCreateAPIKeyRepoUser(t, ctx, client, "replay-owner@example.invalid")
	group, err := client.Group.Create().SetName("Repository replay").SetIsExclusive(true).Save(ctx)
	require.NoError(t, err)
	_, err = client.User.UpdateOneID(owner.ID).SetBalance(5).AddAllowedGroupIDs(group.ID).Save(ctx)
	require.NoError(t, err)
	key := &service.APIKey{UserID: owner.ID, GroupID: &group.ID, Key: "replay-credential-0123456789abcdef", Name: "Replay", Status: service.StatusActive, IPWhitelist: []string{"203.0.113.9"}}
	require.NoError(t, repo.Create(ctx, key))
	cfg := &config.Config{RunMode: config.RunModeStandard}
	keys := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
	request := func(google bool, peer, xff string, userID int64) *httptest.ResponseRecorder {
		router := gin.New()
		router.Use(middleware.TrustedProxyContext(nil, cfg))
		if google {
			router.Use(middleware.APIKeyAuthWithSubscriptionGoogle(keys, nil, cfg))
		} else {
			router.Use(gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(keys, nil, cfg)))
		}
		router.POST("/t", func(c *gin.Context) {
			authenticated, ok := middleware.GetAPIKeyFromContext(c)
			require.True(t, ok)
			require.Empty(t, authenticated.Key)
			require.Equal(t, "203.0.113.9", ip.GetSecurityClientIP(c, false))
			c.Status(http.StatusNoContent)
		})
		r := httptest.NewRequest(http.MethodPost, "/t", nil)
		r.RemoteAddr = peer
		r.Header.Set("X-Forwarded-For", xff)
		r = r.WithContext(service.WithCreationStudioAPIKey(r.Context(), userID, key.ID))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	for _, google := range []bool{false, true} {
		t.Run(fmt.Sprintf("google=%v", google), func(t *testing.T) {
			w := request(google, "203.0.113.9:1234", "198.51.100.7", owner.ID)
			require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
			w = request(google, "198.51.100.7:1234", "203.0.113.9", owner.ID)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			w = request(google, "203.0.113.9:1234", "", owner.ID+1)
			require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
			_, err := client.User.UpdateOneID(owner.ID).SetBalance(0).Save(ctx)
			require.NoError(t, err)
			w = request(google, "203.0.113.9:1234", "", owner.ID)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			_, err = client.User.UpdateOneID(owner.ID).SetBalance(5).ClearAllowedGroups().Save(ctx)
			require.NoError(t, err)
			w = request(google, "203.0.113.9:1234", "", owner.ID)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			_, err = client.User.UpdateOneID(owner.ID).AddAllowedGroupIDs(group.ID).Save(ctx)
			require.NoError(t, err)
		})
	}
	enterprise, err := client.Enterprise.Create().SetName("Replay enterprise").SetGroupPoolConfigured(true).Save(ctx)
	require.NoError(t, err)
	member, err := client.EnterpriseMember.Create().SetEnterpriseID(enterprise.ID).SetUserID(owner.ID).Save(ctx)
	require.NoError(t, err)
	for _, google := range []bool{false, true} {
		w := request(google, "203.0.113.9:1234", "", owner.ID)
		require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	}
	_, err = client.EnterpriseAllowedGroup.Create().SetEnterpriseID(enterprise.ID).SetGroupID(group.ID).Save(ctx)
	require.NoError(t, err)
	for _, google := range []bool{false, true} {
		w := request(google, "203.0.113.9:1234", "", owner.ID)
		require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	}
	_, err = client.EnterpriseMember.UpdateOneID(member.ID).SetStatus(service.EnterpriseMemberStatusDisabled).Save(ctx)
	require.NoError(t, err)
	for _, google := range []bool{false, true} {
		w := request(google, "203.0.113.9:1234", "", owner.ID)
		require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	}
	_, err = client.APIKey.UpdateOneID(key.ID).SetDeletedAt(time.Now()).Save(ctx)
	require.NoError(t, err)
	w := request(false, "203.0.113.9:1234", "", owner.ID)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}
