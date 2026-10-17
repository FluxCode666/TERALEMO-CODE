package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type enterpriseIdentityReader interface {
	Identity(context.Context, int64) (*service.EnterpriseIdentity, error)
}
