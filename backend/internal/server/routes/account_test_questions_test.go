//go:build unit

package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountTestQuestionRoutesRepo struct {
	question *model.AccountTestQuestion
	calls    int
}

func (r *accountTestQuestionRoutesRepo) List(_ context.Context, enabledOnly bool) ([]*model.AccountTestQuestion, error) {
	r.calls++
	questions := []*model.AccountTestQuestion{}
	if r.question != nil && (!enabledOnly || r.question.Enabled) {
		questions = append(questions, r.question)
	}
	return questions, nil
}

func (r *accountTestQuestionRoutesRepo) GetByID(_ context.Context, id int64) (*model.AccountTestQuestion, error) {
	r.calls++
	if r.question == nil || r.question.ID != id {
		return nil, nil
	}
	return r.question, nil
}

func (r *accountTestQuestionRoutesRepo) Create(_ context.Context, question *model.AccountTestQuestion) (*model.AccountTestQuestion, error) {
	r.calls++
	question.ID = 1
	r.question = question
	return question, nil
}

func (r *accountTestQuestionRoutesRepo) Update(_ context.Context, question *model.AccountTestQuestion, _ service.AccountTestQuestionUpdateMask) (*model.AccountTestQuestion, error) {
	r.calls++
	r.question = question
	return question, nil
}

func (r *accountTestQuestionRoutesRepo) Delete(_ context.Context, id int64) error {
	r.calls++
	if r.question == nil || r.question.ID != id {
		return service.ErrAccountTestQuestionNotFound
	}
	r.question = nil
	return nil
}

func accountTestQuestionRoutesHandlers(repo *accountTestQuestionRoutesRepo) *handler.Handlers {
	questions := service.NewAccountTestQuestionService(repo)
	return &handler.Handlers{Admin: &handler.AdminHandlers{
		AccountTestQuestion: adminhandler.NewAccountTestQuestionHandler(questions),
		AccountBatchTest: adminhandler.NewAccountBatchTestHandler(
			service.NewAccountBatchTestService(nil, nil, questions, nil),
		),
		AccountModelIntersection: adminhandler.NewAccountModelIntersectionHandler(service.NewAccountModelCatalogService(nil, nil)),
		Account:                  &adminhandler.AccountHandler{},
		OAuth:                    &adminhandler.OAuthHandler{},
		OpenAIOAuth:              &adminhandler.OpenAIOAuthHandler{},
	}}
}

func requestAccountTestQuestionRoute(router *gin.Engine, method, path, body, role string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Test-Role", role)
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestAccountTestQuestionRoutesAndBatchRequireAdminAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	admin := router.Group("/api/v1/admin")
	// RegisterAdminRoutes applies this middleware to the shared admin group
	// before calling either registration helper; isolate those helpers here.
	admin.Use(gin.HandlerFunc(middleware.NewAdminAuthMiddleware(nil, nil, nil, nil)))
	repo := &accountTestQuestionRoutesRepo{}
	handlers := accountTestQuestionRoutesHandlers(repo)
	registerAccountTestQuestionRoutes(admin, handlers)
	registerAccountRoutes(admin, handlers, middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() }))

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/account-test-questions"},
		{http.MethodGet, "/account-test-questions/1"},
		{http.MethodPost, "/account-test-questions"},
		{http.MethodPut, "/account-test-questions/1"},
		{http.MethodDelete, "/account-test-questions/1"},
		{http.MethodPost, "/accounts/batch-test"},
	} {
		recorder := requestAccountTestQuestionRoute(router, route.method, "/api/v1/admin"+route.path, `{}`, "")
		require.Equal(t, http.StatusUnauthorized, recorder.Code, route.method+route.path+": "+recorder.Body.String())
		require.Contains(t, recorder.Body.String(), "Authorization required")
	}
	require.Zero(t, repo.calls, "authentication must reject requests before reaching storage")
}

func TestAccountTestQuestionRoutesCRUDAndAdminRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	admin := router.Group("/api/v1/admin")
	// Inject already-authenticated claims without requiring JWT dependencies.
	admin.Use(func(c *gin.Context) {
		if role := c.GetHeader("X-Test-Role"); role != "" {
			c.Set(string(middleware.ContextKeyUserRole), role)
		}
	}, middleware.AdminOnly())
	repo := &accountTestQuestionRoutesRepo{}
	registerAccountTestQuestionRoutes(admin, accountTestQuestionRoutesHandlers(repo))
	for _, route := range router.Routes() {
		require.True(t, strings.HasPrefix(route.Path, "/api/v1/admin/account-test-questions"))
	}
	require.Len(t, router.Routes(), 5)

	for _, route := range []struct{ method, suffix string }{
		{http.MethodGet, ""}, {http.MethodGet, "/1"}, {http.MethodPost, ""}, {http.MethodPut, "/1"}, {http.MethodDelete, "/1"},
	} {
		recorder := requestAccountTestQuestionRoute(router, route.method, "/api/v1/admin/account-test-questions"+route.suffix, `{}`, service.RoleUser)
		require.Equal(t, http.StatusForbidden, recorder.Code, route.method+route.suffix)
	}
	require.Zero(t, repo.calls)

	recorder := requestAccountTestQuestionRoute(router, http.MethodPost, "/api/v1/admin/account-test-questions",
		`{"name":" Identity ","content":" Identify yourself ","answer_patterns":[" (?i)claude ","(?i)claude"]}`, service.RoleAdmin)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "Identity", repo.question.Name)
	require.Equal(t, []string{"(?i)claude"}, repo.question.AnswerPatterns)
	require.True(t, repo.question.Enabled)

	recorder = requestAccountTestQuestionRoute(router, http.MethodGet, "/api/v1/admin/account-test-questions/1", "", service.RoleAdmin)
	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Code int                       `json:"code"`
		Data model.AccountTestQuestion `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.Equal(t, repo.question, &envelope.Data)

	recorder = requestAccountTestQuestionRoute(router, http.MethodPut, "/api/v1/admin/account-test-questions/1",
		`{"enabled":false,"answer_patterns":[]}`, service.RoleAdmin)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.False(t, repo.question.Enabled)
	require.Equal(t, []string{}, repo.question.AnswerPatterns)
	require.Equal(t, "Identity", repo.question.Name)

	recorder = requestAccountTestQuestionRoute(router, http.MethodGet, "/api/v1/admin/account-test-questions?enabled=true", "", service.RoleAdmin)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"data":[]`)
	recorder = requestAccountTestQuestionRoute(router, http.MethodGet, "/api/v1/admin/account-test-questions", "", service.RoleAdmin)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"id":1`)

	recorder = requestAccountTestQuestionRoute(router, http.MethodDelete, "/api/v1/admin/account-test-questions/1", "", service.RoleAdmin)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Nil(t, repo.question)
	recorder = requestAccountTestQuestionRoute(router, http.MethodGet, "/api/v1/admin/account-test-questions/1", "", service.RoleAdmin)
	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestAccountBatchTestRouteDoesNotConflictWithAccountID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	admin := router.Group("/api/v1/admin")
	admin.Use(func(c *gin.Context) {
		if role := c.GetHeader("X-Test-Role"); role != "" {
			c.Set(string(middleware.ContextKeyUserRole), role)
		}
	}, middleware.AdminOnly())
	registerAccountRoutes(admin, accountTestQuestionRoutesHandlers(&accountTestQuestionRoutesRepo{}),
		middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() }))

	recorder := requestAccountTestQuestionRoute(router, http.MethodPost, "/api/v1/admin/accounts/batch-test", `{}`, service.RoleUser)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	recorder = requestAccountTestQuestionRoute(router, http.MethodPost, "/api/v1/admin/accounts/batch-test", `{}`, service.RoleAdmin)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "account_ids must contain between 1 and 200 unique ids")
	require.NotContains(t, recorder.Body.String(), "Invalid account ID")

	// GET still matches /:id; POST /:id/test remains separately registered.
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/accounts/batch-test"},
		{http.MethodPost, "/api/v1/admin/accounts/not-an-id/test"},
	} {
		recorder = requestAccountTestQuestionRoute(router, route.method, route.path, `{}`, service.RoleAdmin)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Contains(t, recorder.Body.String(), "Invalid account ID")
	}
}
