//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type showOnceAPIKeyRepository struct {
	service.APIKeyRepository
	keys    map[int64]*service.APIKey
	creates int
}

func (r *showOnceAPIKeyRepository) Create(_ context.Context, key *service.APIKey) error {
	r.creates++
	key.ID = int64(r.creates)
	stored := *key
	stored.Key = ""
	r.keys[key.ID] = &stored
	return nil
}

func (r *showOnceAPIKeyRepository) GetByID(_ context.Context, id int64) (*service.APIKey, error) {
	key, ok := r.keys[id]
	if !ok {
		return nil, service.ErrAPIKeyNotFound
	}
	return key, nil
}

func (r *showOnceAPIKeyRepository) ExistsByKey(_ context.Context, credential string) (bool, error) {
	for _, key := range r.keys {
		if key.KeyHash == service.HashAPIKey(credential) {
			return true, nil
		}
	}
	return false, nil
}

func (r *showOnceAPIKeyRepository) ListByUserID(_ context.Context, userID int64, params pagination.PaginationParams, _ service.APIKeyListFilters) ([]service.APIKey, *pagination.PaginationResult, error) {
	keys := make([]service.APIKey, 0, len(r.keys))
	for _, key := range r.keys {
		if key.UserID == userID {
			keys = append(keys, *key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].ID < keys[j].ID })
	return keys, &pagination.PaginationResult{Total: int64(len(keys)), Page: params.Page, PageSize: params.PageSize}, nil
}

type showOnceUserRepository struct {
	service.UserRepository
}

func (showOnceUserRepository) GetByID(_ context.Context, id int64) (*service.User, error) {
	return &service.User{ID: id, Status: service.StatusActive, Role: service.RoleUser}, nil
}

func newShowOnceAPIKeyRouter(t *testing.T, coordinator *service.IdempotencyCoordinator) (*gin.Engine, *showOnceAPIKeyRepository) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previous := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(coordinator)
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })
	repo := &showOnceAPIKeyRepository{keys: make(map[int64]*service.APIKey)}
	keys := service.NewAPIKeyService(repo, showOnceUserRepository{}, nil, nil, nil, nil, &config.Config{})
	handler := NewAPIKeyHandler(keys)
	router := gin.New()
	router.Use(withUserSubject(7))
	router.POST("/api/v1/api-keys", handler.Create)
	router.GET("/api/v1/api-keys", handler.List)
	router.GET("/api/v1/api-keys/:id", handler.GetByID)
	return router, repo
}

func showOnceAPIKeyRequest(router *gin.Engine, method, path, body, idempotencyKey string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func requireShowOnceReadResponses(t *testing.T, router *gin.Engine, credential, hint string) {
	t.Helper()
	for _, read := range []struct {
		path    string
		keyPath string
	}{
		{path: "/api/v1/api-keys/1", keyPath: "data"},
		{path: "/api/v1/api-keys", keyPath: "data.items.0"},
	} {
		response := showOnceAPIKeyRequest(router, http.MethodGet, read.path, "", "")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.Equal(t, hint, gjson.GetBytes(response.Body.Bytes(), read.keyPath+".key").String())
		require.Equal(t, hint, gjson.GetBytes(response.Body.Bytes(), read.keyPath+".key_hint").String())
		require.NotContains(t, response.Body.String(), credential)
		require.NotContains(t, response.Body.String(), service.HashAPIKey(credential))
	}
}

func TestAPIKeyShowOnceCreateThenReadOnlyHint(t *testing.T) {
	for _, custom := range []bool{false, true} {
		name := "generated"
		if custom {
			name = "custom"
		}
		t.Run(name, func(t *testing.T) {
			router, repo := newShowOnceAPIKeyRouter(t, nil)
			payload := map[string]string{"name": "show-once-key"}
			if custom {
				payload["custom_key"] = "sk-show-once-custom-key-12345678"
			}
			body, err := json.Marshal(payload)
			require.NoError(t, err)
			created := showOnceAPIKeyRequest(router, http.MethodPost, "/api/v1/api-keys", string(body), "")
			require.Equal(t, http.StatusOK, created.Code, created.Body.String())
			require.Equal(t, "no-store", created.Header().Get("Cache-Control"))
			credential := gjson.GetBytes(created.Body.Bytes(), "data.key").String()
			require.NotEmpty(t, credential)
			if custom {
				require.Equal(t, payload["custom_key"], credential)
			}
			hint := service.MaskAPIKey(credential)
			require.NotEqual(t, hint, credential)
			require.Equal(t, hint, gjson.GetBytes(created.Body.Bytes(), "data.key_hint").String())
			require.Equal(t, 1, repo.creates)
			require.Empty(t, repo.keys[1].Key)
			require.Equal(t, service.HashAPIKey(credential), repo.keys[1].KeyHash)
			require.Equal(t, hint, repo.keys[1].KeyHint)
			requireShowOnceReadResponses(t, router, credential, hint)
		})
	}
}

func TestAPIKeyShowOnceIdempotencyPersistsAndReplaysOnlyHint(t *testing.T) {
	idempotencyRepo := newUserMemoryIdempotencyRepoStub()
	coordinator := service.NewIdempotencyCoordinator(idempotencyRepo, service.DefaultIdempotencyConfig())
	router, repo := newShowOnceAPIKeyRouter(t, coordinator)
	const credential = "sk-show-once-idempotency-key-12345678"
	const body = `{"name":"idempotent-key","custom_key":"` + credential + `"}`
	hint := service.MaskAPIKey(credential)

	created := showOnceAPIKeyRequest(router, http.MethodPost, "/api/v1/api-keys", body, "create-key-once")
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	require.Equal(t, credential, gjson.GetBytes(created.Body.Bytes(), "data.key").String())
	require.Equal(t, hint, gjson.GetBytes(created.Body.Bytes(), "data.key_hint").String())
	require.Empty(t, created.Header().Get("X-Idempotency-Replayed"))
	require.Equal(t, "no-store", created.Header().Get("Cache-Control"))
	require.Empty(t, repo.keys[1].Key)

	idempotencyRepo.mu.Lock()
	var storedBodies []string
	for _, record := range idempotencyRepo.data {
		if record.ResponseBody != nil {
			storedBodies = append(storedBodies, *record.ResponseBody)
		}
	}
	idempotencyRepo.mu.Unlock()
	require.Len(t, storedBodies, 1)
	require.NotContains(t, storedBodies[0], credential)
	require.NotContains(t, storedBodies[0], service.HashAPIKey(credential))
	// Idempotency storage also redacts sensitive field names; the dedicated hint survives.
	require.Equal(t, "***", gjson.Get(storedBodies[0], "key").String())
	require.Equal(t, hint, gjson.Get(storedBodies[0], "key_hint").String())

	for range 2 {
		replayed := showOnceAPIKeyRequest(router, http.MethodPost, "/api/v1/api-keys", body, "create-key-once")
		require.Equal(t, http.StatusOK, replayed.Code, replayed.Body.String())
		require.Equal(t, "true", replayed.Header().Get("X-Idempotency-Replayed"))
		require.Equal(t, "no-store", replayed.Header().Get("Cache-Control"))
		require.Equal(t, hint, gjson.GetBytes(replayed.Body.Bytes(), "data.key").String())
		require.Equal(t, hint, gjson.GetBytes(replayed.Body.Bytes(), "data.key_hint").String())
		require.NotContains(t, replayed.Body.String(), credential)
		require.Equal(t, 1, repo.creates, "replay must not execute creation again")
	}
	requireShowOnceReadResponses(t, router, credential, hint)
	require.Empty(t, repo.keys[1].Key, "presenting the creation response must not mutate the stored key")
	require.Equal(t, hint, repo.keys[1].KeyHint)
}
