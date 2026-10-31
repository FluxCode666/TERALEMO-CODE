//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type modelIntersectionHandlerRepo struct {
	service.AccountRepository
	accounts map[int64]*service.Account
	err      error
}

func (r *modelIntersectionHandlerRepo) GetByIDs(_ context.Context, ids []int64) ([]*service.Account, error) {
	if r.err != nil {
		return nil, r.err
	}
	accounts := make([]*service.Account, 0, len(ids))
	for _, id := range ids {
		if account := r.accounts[id]; account != nil {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func setupModelIntersectionRouter(repo *modelIntersectionHandlerRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAccountModelIntersectionHandler(service.NewAccountModelCatalogService(repo, nil))
	router.POST("/api/v1/admin/accounts/model-intersection", handler.Get)
	return router
}

func TestAccountModelIntersectionHandlerValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		mixed   bool
		message string
	}{
		{name: "invalid JSON", body: `{`, message: "Invalid request"},
		{name: "string ID", body: `{"account_ids":["1"]}`, message: "Invalid request"},
		{name: "fractional ID", body: `{"account_ids":[1.5]}`, message: "Invalid request"},
		{name: "missing IDs", body: `{}`, message: "between 1 and 100"},
		{name: "null IDs", body: `{"account_ids":null}`, message: "between 1 and 100"},
		{name: "zero ID", body: `{"account_ids":[0]}`, message: "positive integers"},
		{name: "missing account", body: `{"account_ids":[1,3]}`, message: "Account 3 not found"},
		{name: "mixed platforms", body: `{"account_ids":[1,2]}`, mixed: true, message: "Batch test requires accounts from the same platform"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &modelIntersectionHandlerRepo{accounts: map[int64]*service.Account{
				1: {ID: 1, Platform: service.PlatformAnthropic},
				2: {ID: 2, Platform: service.PlatformAnthropic},
			}}
			if tc.mixed {
				repo.accounts[2].Platform = service.PlatformGemini
			}
			rec := httptest.NewRecorder()
			setupModelIntersectionRouter(repo).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/model-intersection", strings.NewReader(tc.body)))
			require.Equal(t, http.StatusBadRequest, rec.Code)
			var payload struct {
				Message string `json:"message"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
			require.Contains(t, payload.Message, tc.message)
		})
	}
}

func TestAccountModelIntersectionHandlerResponse(t *testing.T) {
	repo := &modelIntersectionHandlerRepo{accounts: map[int64]*service.Account{
		1: {ID: 1, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Credentials: map[string]any{
			"model_mapping": map[string]any{"shared": "upstream", "first-only": "upstream"},
		}},
		2: {ID: 2, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Credentials: map[string]any{
			"model_mapping": map[string]any{"shared": "upstream", "second-only": "upstream"},
		}},
	}}
	rec := httptest.NewRecorder()
	setupModelIntersectionRouter(repo).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/model-intersection", strings.NewReader(`{"account_ids":[2,1,2]}`)))
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"code":0,"message":"success","data":{"platform":"anthropic","models":[{"id":"shared","display_name":"shared"}],"account_count":2,"failed_account_ids":[]}}`, rec.Body.String())
}

func TestAccountModelIntersectionHandlerRepositoryFailure(t *testing.T) {
	rec := httptest.NewRecorder()
	repo := &modelIntersectionHandlerRepo{err: errors.New("database failure")}
	setupModelIntersectionRouter(repo).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/model-intersection", strings.NewReader(`{"account_ids":[1]}`)))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), "database failure")
}

func TestAccountAvailableModelsHandlerUsesInjectedCatalog(t *testing.T) {
	adminSvc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{
		ID: 1, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{"test-alias": "upstream"}},
	}}
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.SetAccountModelCatalogService(service.NewAccountModelCatalogService(nil, nil))
	router := gin.New()
	router.GET("/api/v1/admin/accounts/:id/models", handler.GetAvailableModels)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/1/models", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"code":0,"message":"success","data":[{"id":"test-alias","type":"model","display_name":"test-alias","created_at":""}]}`, rec.Body.String())
}

type modelIntersectionBatchAccountRepo struct {
	service.AccountRepository
}

func (r *modelIntersectionBatchAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	platform := service.PlatformAnthropic
	if id == 2 {
		platform = service.PlatformOpenAI
	}
	return &service.Account{ID: id, Platform: platform}, nil
}

func TestAccountBatchTestHandlerMixedPlatformsBadRequest(t *testing.T) {
	accounts := &modelIntersectionBatchAccountRepo{}
	questions := service.NewAccountTestQuestionService(&batchTestHandlerQuestionRepo{questions: map[int64]*model.AccountTestQuestion{
		1: {ID: 1, Enabled: true, Content: "Identify yourself"},
	}})
	handler := NewAccountBatchTestHandler(service.NewAccountBatchTestService(accounts, nil, questions, nil))
	router := gin.New()
	router.POST("/accounts/batch-test", handler.Test)
	rec := requestAccountTestQuestion(router, http.MethodPost, "/accounts/batch-test", `{"account_ids":[1,2],"question_ids":[1]}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.JSONEq(t, `{"code":400,"message":"Batch test requires accounts from the same platform","reason":"BATCH_TEST_MIXED_PLATFORMS"}`, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "data:")
}
