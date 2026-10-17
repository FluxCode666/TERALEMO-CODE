//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEnterprisePromptTooLongFallbackPreservesOriginalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, allowed := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied", true: "allowed"}[allowed], func(t *testing.T) {
			user := &service.User{Enterprise: &service.EnterpriseMemberContext{GroupPoolConfigured: true, GroupPoolIDs: []int64{10}}}
			if allowed {
				user.Enterprise.GroupPoolIDs = append(user.Enterprise.GroupPoolIDs, 11)
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			h := &GatewayHandler{antigravityGatewayService: &service.AntigravityGatewayService{}}
			rejected := h.rejectEnterprisePromptTooLongFallback(c, user, &service.Group{ID: 11}, &service.Account{ID: 1, Platform: service.PlatformAntigravity}, &service.PromptTooLongError{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"message":"Prompt is too long"}}`)})
			require.Equal(t, !allowed, rejected)
			if allowed {
				require.False(t, c.Writer.Written())
			} else {
				require.Equal(t, http.StatusBadRequest, recorder.Code)
				require.Contains(t, recorder.Body.String(), "Prompt is too long")
				require.NotContains(t, recorder.Body.String(), "ENTERPRISE_GROUP_NOT_ALLOWED")
			}
		})
	}
}
