package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// buildMassEmailRouter 用最小依赖搭出邮件群发相关路由。
func buildMassEmailRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	v1 := engine.Group("/api/v1")

	massEmailHandler := admin.NewMassEmailHandler(service.NewMassEmailService(nil, nil, nil, nil), nil)
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{MassEmail: massEmailHandler}}

	// 公开像素路由（与 RegisterUserRoutes 中的注册方式一致）。
	v1.GET("/mail/o/:token.gif", massEmailHandler.Pixel)
	// 管理路由（含 /:id 与 /preview-recipients、/test 静态兄弟节点）。
	registerMassEmailRoutes(v1.Group("/admin"), handlers)
	return engine
}

func TestMassEmailRoutesRegisterWithoutConflict(t *testing.T) {
	require.NotPanics(t, func() {
		buildMassEmailRouter(t)
	}, "gin 不允许 /:id 与静态路径冲突，注册必须成功")
}

func TestMassEmailPixelAlwaysReturnsGIF(t *testing.T) {
	engine := buildMassEmailRouter(t)

	for _, path := range []string{
		"/api/v1/mail/o/0123456789abcdef0123456789abcdef.gif",
		"/api/v1/mail/o/unknown-token.gif",
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		engine.ServeHTTP(recorder, request)

		require.Equal(t, http.StatusOK, recorder.Code, path)
		require.Equal(t, "image/gif", recorder.Header().Get("Content-Type"))
		require.Contains(t, recorder.Header().Get("Cache-Control"), "no-store")
		require.NotEmpty(t, recorder.Body.Bytes())
		// 1x1 透明 GIF 的魔数
		require.Equal(t, []byte("GIF89a"), recorder.Body.Bytes()[:6])
	}
}
