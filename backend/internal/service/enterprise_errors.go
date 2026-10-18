package service

import infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

// Management validation errors are separate from runtime billing/gateway errors.
var ErrEnterpriseFrozenBalancePending = infraerrors.Conflict("ENTERPRISE_FROZEN_BALANCE_PENDING", "企业仍有进行中的冻结任务，请等待结算或释放后再解散。")

var ErrEnterpriseInvalidInput = infraerrors.BadRequest("INVALID_INPUT", "Invalid enterprise input")
