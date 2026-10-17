package handler

import (
	"strconv"
	"strings"

	pkgerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

const enterpriseBillingErrorContextKey = "enterprise_billing_error"

// Preserve metadata for the protocol-specific JSON and SSE writers.
func billingErrorDetailsForContext(c *gin.Context, err error) (int, string, string, int) {
	appErr := pkgerrors.FromError(err)
	if c != nil && appErr != nil && isEnterpriseBillingErrorReason(appErr.Reason) {
		c.Set(enterpriseBillingErrorContextKey, appErr)
	}
	return billingErrorDetails(err)
}

func isEnterpriseBillingErrorReason(reason string) bool {
	return strings.HasPrefix(reason, "ENTERPRISE_") || reason == "DEPARTMENT_QUOTA_EXCEEDED" || reason == "MEMBER_QUOTA_EXCEEDED"
}

func enterpriseBillingError(c *gin.Context) *pkgerrors.ApplicationError {
	if c == nil {
		return nil
	}
	v, _ := c.Get(enterpriseBillingErrorContextKey)
	err, _ := v.(*pkgerrors.ApplicationError)
	return err
}

func withEnterpriseBillingMetadata(c *gin.Context, body gin.H) gin.H {
	if err := enterpriseBillingError(c); err != nil {
		if _, numericCode := body["code"].(int); numericCode {
			body["reason"] = err.Reason
		} else {
			body["code"] = err.Reason
		}
		if len(err.Metadata) > 0 {
			body["metadata"] = err.Metadata
		}
	}
	return body
}

// A scheduling fallback can resolve a group outside the enterprise pool. Keep
// its permission error instead of classifying it as unavailable upstream capacity.
func (h *GatewayHandler) handleEnterpriseSelectionError(c *gin.Context, err error, streamStarted bool) bool {
	appErr := pkgerrors.FromError(err)
	if appErr == nil || !isEnterpriseBillingErrorReason(appErr.Reason) {
		return false
	}
	status, code, message, retry := billingErrorDetailsForContext(c, err)
	if retry > 0 {
		c.Header("Retry-After", strconv.Itoa(retry))
	}
	h.handleStreamingAwareError(c, status, code, message, streamStarted)
	return true
}
