package service

import infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

// Runtime errors have distinct Go names from enterprise management errors while
// sharing the public error codes defined in the enterprise API contract.
var (
	ErrEnterpriseGatewayDisabled                = infraerrors.Forbidden("ENTERPRISE_DISABLED", "Enterprise is disabled.")
	ErrEnterpriseGatewayMemberDisabled          = infraerrors.Forbidden("ENTERPRISE_MEMBER_DISABLED", "Enterprise member is disabled.")
	ErrEnterpriseGatewayGroupNotAllowed         = infraerrors.Forbidden("ENTERPRISE_GROUP_NOT_ALLOWED", "Group is not allowed for this enterprise member.")
	ErrEnterpriseBillingBalanceInsufficient     = infraerrors.Forbidden("ENTERPRISE_BALANCE_INSUFFICIENT", "Enterprise balance is insufficient.")
	ErrEnterpriseBillingQuotaExceeded           = infraerrors.TooManyRequests("ENTERPRISE_QUOTA_EXCEEDED", "Enterprise usage quota exceeded.")
	ErrEnterpriseBillingDepartmentQuotaExceeded = infraerrors.TooManyRequests("DEPARTMENT_QUOTA_EXCEEDED", "Department usage quota exceeded.")
	ErrEnterpriseBillingMemberQuotaExceeded     = infraerrors.TooManyRequests("MEMBER_QUOTA_EXCEEDED", "Member usage quota exceeded.")
	ErrEnterpriseBillingLegacyUnsupported       = infraerrors.ServiceUnavailable("BILLING_SERVICE_ERROR", "Enterprise billing requires transactional usage settlement.")
)
