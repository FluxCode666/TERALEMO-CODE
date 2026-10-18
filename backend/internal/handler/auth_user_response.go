package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// authUserResponse keeps enterprise identity scoped to the signed-in user;
// nested user DTOs in unrelated resource responses retain their existing shape.
type authUserResponse struct {
	*dto.User
	Enterprise *service.EnterpriseIdentity `json:"enterprise"`
}

func readAuthEnterpriseIdentity(ctx context.Context, reader enterpriseIdentityReader, userID int64) (*service.EnterpriseIdentity, error) {
	if reader == nil {
		return nil, nil
	}
	identity, err := reader.Identity(ctx, userID)
	if err != nil || identity == nil {
		return nil, err
	}
	copy := *identity
	if !copy.BalanceVisible {
		copy.Balance = nil
		copy.FrozenBalance = nil
	}
	return &copy, nil
}

func buildAuthUserResponse(ctx context.Context, user *service.User, reader enterpriseIdentityReader) (*authUserResponse, error) {
	if err := ensureLoginUserActive(user); err != nil {
		return nil, err
	}
	identity, err := readAuthEnterpriseIdentity(ctx, reader, user.ID)
	if err != nil {
		return nil, err
	}
	return &authUserResponse{User: dto.UserFromService(user), Enterprise: identity}, nil
}
