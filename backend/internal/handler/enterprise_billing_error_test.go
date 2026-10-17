package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseBillingErrorMappings(t *testing.T) {
	for _, err := range []error{service.ErrEnterpriseGatewayDisabled, service.ErrEnterpriseGatewayMemberDisabled, service.ErrEnterpriseGatewayGroupNotAllowed, service.ErrEnterpriseBillingBalanceInsufficient} {
		status, code, _, retry := billingErrorDetails(err)
		require.Equal(t, http.StatusForbidden, status)
		require.True(t, strings.HasPrefix(code, "ENTERPRISE_"))
		require.Zero(t, retry)
	}
	for _, err := range []error{service.ErrEnterpriseBillingQuotaExceeded, service.ErrEnterpriseBillingDepartmentQuotaExceeded, service.ErrEnterpriseBillingMemberQuotaExceeded} {
		status, _, _, retry := billingErrorDetails(err)
		require.Equal(t, http.StatusTooManyRequests, status)
		require.Positive(t, retry)
	}
}

func TestEnterpriseBillingMetadataAcrossProtocols(t *testing.T) {
	metadata := map[string]string{"scope": "department", "window": "monthly", "window_resets_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
	err := service.ErrEnterpriseBillingDepartmentQuotaExceeded.WithMetadata(metadata)
	for _, protocol := range []string{"anthropic", "openai", "google", "responses", "stream"} {
		t.Run(protocol, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			status, code, message, retry := billingErrorDetailsForContext(c, err)
			require.Greater(t, retry, 3500)
			switch protocol {
			case "anthropic":
				(&GatewayHandler{}).errorResponse(c, status, code, message)
			case "openai":
				(&OpenAIGatewayHandler{}).errorResponse(c, status, code, message)
			case "google":
				googleErrorWithType(c, status, code, "", message)
			case "responses":
				(&GatewayHandler{}).responsesErrorResponse(c, status, code, message)
			case "stream":
				c.Writer.WriteHeader(http.StatusOK)
				c.Writer.Flush()
				(&GatewayHandler{}).handleStreamingAwareError(c, status, code, message, true)
			}
			body := recorder.Body.String()
			if protocol == "stream" {
				body = strings.TrimSpace(strings.TrimPrefix(body, "data: "))
				require.Equal(t, http.StatusOK, recorder.Code)
			} else {
				require.Equal(t, http.StatusTooManyRequests, recorder.Code)
			}
			var decoded map[string]any
			require.NoError(t, json.Unmarshal([]byte(body), &decoded))
			obj, ok := decoded["error"].(map[string]any)
			require.True(t, ok)
			metadata, ok := obj["metadata"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "monthly", metadata["window"])
			require.Contains(t, body, "DEPARTMENT_QUOTA_EXCEEDED")
		})
	}
}
