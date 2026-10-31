//go:build unit

package routes

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestScheduledQuestionRoutesAndModelIntersectionRequireAdmin(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		gin.SetMode(gin.TestMode)
		router := gin.New()
		admin := router.Group("/api/v1/admin")
		if authenticated {
			admin.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyUserRole), service.RoleUser)
			}, middleware.AdminOnly())
		} else {
			admin.Use(gin.HandlerFunc(middleware.NewAdminAuthMiddleware(nil, nil, nil, nil)))
		}
		handlers := accountTestQuestionRoutesHandlers(&accountTestQuestionRoutesRepo{})
		handlers.Admin.ScheduledQuestionTest = adminhandler.NewScheduledQuestionTestHandler(nil)
		registerScheduledQuestionTestRoutes(admin, handlers)
		registerAccountRoutes(admin, handlers, middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() }))
		for _, route := range []struct{ method, path string }{
			{http.MethodGet, "/scheduled-question-tests"},
			{http.MethodGet, "/scheduled-question-tests/1"},
			{http.MethodPost, "/scheduled-question-tests"},
			{http.MethodPut, "/scheduled-question-tests/1"},
			{http.MethodDelete, "/scheduled-question-tests/1"},
			{http.MethodGet, "/scheduled-question-tests/1/runs"},
			{http.MethodPost, "/scheduled-question-tests/1/run-now"},
			{http.MethodPost, "/accounts/model-intersection"},
		} {
			recorder := requestAccountTestQuestionRoute(router, route.method, "/api/v1/admin"+route.path, `{}`, "")
			expected := http.StatusUnauthorized
			if authenticated {
				expected = http.StatusForbidden
			}
			require.Equal(t, expected, recorder.Code, route.method+route.path+": "+recorder.Body.String())
		}
	}
}

func TestScheduledQuestionRoutesRegisteredWithoutConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerScheduledQuestionTestRoutes(router.Group("/api/v1/admin"), &handler.Handlers{Admin: &handler.AdminHandlers{
		ScheduledQuestionTest: adminhandler.NewScheduledQuestionTestHandler(nil),
	}})
	require.Len(t, router.Routes(), 7)
	for _, route := range router.Routes() {
		require.Contains(t, route.Path, "/api/v1/admin/scheduled-question-tests")
	}
	recorder := requestAccountTestQuestionRoute(router, http.MethodGet, "/api/v1/admin/scheduled-question-tests/not-an-id/runs", "", service.RoleAdmin)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	recorder = requestAccountTestQuestionRoute(router, http.MethodPost, "/api/v1/admin/scheduled-question-tests/not-an-id/run-now", "", service.RoleAdmin)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}
