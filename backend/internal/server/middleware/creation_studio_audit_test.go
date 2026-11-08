//go:build unit

package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestCreationStudioAuditOmitBodies keeps prompts and multipart payloads out of
// the audit table and locks the two action names of the contract.
func TestCreationStudioAuditOmitBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &auditCaptureRepository{}
	auditService := service.NewAuditLogService(repository, nil)
	auditService.Start()

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyUser), AuthSubject{UserID: 77})
		c.Next()
	})
	router.Use(gin.HandlerFunc(NewAuditLogMiddleware(auditService)))
	handler := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }
	router.POST("/api/v1/user/creation/estimate", handler)
	router.POST("/api/v1/user/creation/uploads", handler)
	router.POST("/api/v1/user/creation/tasks", handler)

	for _, target := range []string{
		"/api/v1/user/creation/estimate",
		"/api/v1/user/creation/uploads",
		"/api/v1/user/creation/tasks",
	} {
		request := httptest.NewRequest(http.MethodPost, target, bytes.NewBufferString(`{"prompt":"audit-canary-prompt"}`))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusOK, recorder.Code)
	}
	auditService.Stop()

	repository.mu.Lock()
	logs := append([]*service.AuditLog(nil), repository.logs...)
	repository.mu.Unlock()
	require.Len(t, logs, 3)

	byAction := make(map[string]*service.AuditLog, len(logs))
	for _, entry := range logs {
		byAction[entry.Action] = entry
		require.Equal(t, "<credential-bearing body omitted>", entry.RequestBody)
		require.NotContains(t, entry.RequestBody, "audit-canary-prompt")
	}
	require.Contains(t, byAction, "user.creation.upload")
	require.Contains(t, byAction, "user.creation.submit")
	require.Equal(t, "<credential-bearing body omitted>", byAction["user.creation.submit"].RequestBody)
}
