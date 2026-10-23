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

func TestVideoBillingAdminSensitiveReadsAndResolutionsAreAudited(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &auditCaptureRepository{}
	auditService := service.NewAuditLogService(repository, nil)
	auditService.Start()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyUser), AuthSubject{UserID: 7})
		c.Set(string(ContextKeyUserRole), service.RoleAdmin)
		c.Next()
	})
	router.Use(gin.HandlerFunc(NewAuditLogMiddleware(auditService)))
	router.GET("/api/v1/admin/video-billing-reservations", func(c *gin.Context) {
		// Even without a handler override, the GET whitelist must audit this read.
		c.JSON(http.StatusOK, gin.H{"items": []any{}})
	})
	for _, action := range []string{"release", "settle"} {
		action := action
		router.POST("/api/v1/admin/video-billing-reservations/:reservation_id/"+action, func(c *gin.Context) {
			SetAuditAction(c, "admin.video_billing_reservations."+action)
			SetAuditExtra(c, map[string]any{"reservation_id": c.Param("reservation_id"), "resolution_reason": "verified from provider", "credentials": "not-allowed"})
			c.JSON(http.StatusOK, gin.H{"user_id": 42})
		})
	}
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/admin/video-billing-reservations?state=manual", nil))
	for _, action := range []string{"release", "settle"} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/video-billing-reservations/reservation-1/"+action, bytes.NewBufferString(`{"reason":"verified from provider","duration_seconds":4,"video_count":1}`))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(httptest.NewRecorder(), request)
	}
	auditService.Stop()
	repository.mu.Lock()
	logs := append([]*service.AuditLog(nil), repository.logs...)
	repository.mu.Unlock()
	require.Len(t, logs, 3)
	for _, log := range logs {
		require.NotNil(t, log.ActorUserID)
		require.Equal(t, int64(7), *log.ActorUserID, "keep administrator instead of original payer 42")
		require.Equal(t, service.RoleAdmin, log.ActorRole)
		require.Equal(t, http.StatusOK, log.StatusCode)
		require.NotContains(t, log.Extra, "credentials")
		if log.Method == http.MethodGet {
			require.Equal(t, "admin.video_billing_reservations.list", log.Action)
			require.Empty(t, log.RequestBody)
		} else {
			require.Equal(t, "reservation-1", log.Extra["reservation_id"])
			require.Equal(t, "verified from provider", log.Extra["resolution_reason"])
			require.Contains(t, log.RequestBody, "verified from provider")
		}
	}
}
