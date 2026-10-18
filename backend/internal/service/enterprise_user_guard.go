package service

import (
	"context"
	"errors"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrEnterpriseMemberUserDeleteForbidden = infraerrors.Conflict("ENTERPRISE_MEMBER_DELETE_FORBIDDEN", "remove the user from the enterprise before deleting the account")

var errEnterpriseUserRelationshipChanged = errors.New("enterprise user relationship changed")

// SetEnterpriseUserGuard preserves the existing AdminService constructor while
// production wiring supplies the narrow enterprise repository.
func (s *adminServiceImpl) SetEnterpriseUserGuard(members EnterpriseMemberRepository) {
	s.enterpriseMembers = members
}

func (s *adminServiceImpl) enterpriseUserMember(ctx context.Context, userID int64) (*EnterpriseMember, error) {
	m, err := s.enterpriseMembers.GetByUserID(ctx, userID)
	if errors.Is(err, ErrEnterpriseMemberNotFound) {
		return nil, nil
	}
	return m, err
}

// withEnterpriseUserGuard serializes account mutations with enterprise member
// mutations. The order is enterprise, then user. If a formerly personal account
// joined while we waited for its user lock, retry in a new transaction instead
// of acquiring an enterprise lock after the user lock.
func (s *adminServiceImpl) withEnterpriseUserGuard(ctx context.Context, userID int64, fn func(context.Context, *EnterpriseMember, int64) error) error {
	for range 4 {
		err := s.enterpriseUserGuardAttempt(ctx, userID, fn)
		if !errors.Is(err, errEnterpriseUserRelationshipChanged) {
			return err
		}
		if dbent.TxFromContext(ctx) != nil {
			// The caller owns the transaction and must retry it in full.
			return err
		}
	}
	return fmt.Errorf("enterprise membership changed repeatedly; retry the account operation")
}

func (s *adminServiceImpl) enterpriseUserGuardAttempt(ctx context.Context, userID int64, fn func(context.Context, *EnterpriseMember, int64) error) error {
	if s.entClient != nil && dbent.TxFromContext(ctx) == nil {
		tx, err := s.entClient.Tx(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if err := s.enterpriseUserGuardAttempt(dbent.NewTxContext(ctx, tx), userID, fn); err != nil {
			return err
		}
		return tx.Commit()
	}

	member, err := s.enterpriseUserMember(ctx, userID)
	if err != nil {
		return err
	}
	var owners int64
	if member != nil {
		owners, err = s.enterpriseMembers.CountActiveOwners(ctx, member.EnterpriseID)
		if errors.Is(err, ErrEnterpriseNotFound) {
			return errEnterpriseUserRelationshipChanged
		}
		if err != nil {
			return err
		}
	}
	if tx := dbent.TxFromContext(ctx); tx != nil {
		// FOR NO KEY UPDATE 仍与其他守卫及成员写入互斥，但不阻塞引用 users 的外键检查（FOR KEY SHARE）：
		// 账号更新期间由其他仓储写入的并发调整记录、分组倍率等不会因此等到超时。
		rows, err := tx.Client().QueryContext(ctx, `SELECT id FROM users WHERE id = $1 AND deleted_at IS NULL FOR NO KEY UPDATE`, userID)
		if err != nil {
			return err
		}
		found := rows.Next()
		readErr := rows.Err()
		_ = rows.Close()
		if readErr != nil {
			return readErr
		}
		if !found {
			return ErrUserNotFound
		}
	}
	current, err := s.enterpriseUserMember(ctx, userID)
	if err != nil {
		return err
	}
	if (member == nil) != (current == nil) || (member != nil && (member.ID != current.ID || member.EnterpriseID != current.EnterpriseID)) {
		return errEnterpriseUserRelationshipChanged
	}
	return fn(ctx, current, owners)
}

func (s *adminServiceImpl) checkEnterpriseUserUpdate(ctx context.Context, userID int64, input *UpdateUserInput, member *EnterpriseMember, owners int64) error {
	if member == nil {
		return nil
	}
	if input.Role == RoleAdmin {
		return ErrEnterpriseMemberAdminForbidden
	}
	if input.Status == StatusDisabled && member.Role == EnterpriseRoleOwner && member.Status == EnterpriseMemberStatusActive {
		u, err := s.userRepo.GetByID(ctx, userID)
		if err != nil {
			return err
		}
		if u.Status == StatusActive && owners <= 1 {
			return ErrEnterpriseLastOwner
		}
	}
	return nil
}
