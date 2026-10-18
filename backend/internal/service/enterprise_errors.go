package service

import infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

// Management validation errors are separate from runtime billing/gateway errors.
var ErrEnterpriseFrozenBalancePending = infraerrors.Conflict("ENTERPRISE_FROZEN_BALANCE_PENDING", "企业仍有进行中的冻结任务，请等待结算或释放后再解散。")

var ErrEnterpriseDepartmentCycle = infraerrors.BadRequest("ENTERPRISE_DEPARTMENT_CYCLE", "部门不能移动到自身或子部门下")
var ErrEnterpriseDepartmentDepthExceeded = infraerrors.BadRequest("ENTERPRISE_DEPARTMENT_DEPTH_EXCEEDED", "部门层级不能超过 8 级")

var ErrEnterpriseInvalidInput = infraerrors.BadRequest("INVALID_INPUT", "Invalid enterprise input")
