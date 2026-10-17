package service

import infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

// Management validation errors are separate from runtime billing/gateway errors.
var ErrEnterpriseInvalidInput = infraerrors.BadRequest("INVALID_INPUT", "Invalid enterprise input")
