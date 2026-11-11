package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const SettingKeyForceTotp = "force_totp_enabled"

type ForceTotpPolicy struct {
	Enabled     bool      `json:"enabled"`
	Epoch       string    `json:"epoch"`
	ActivatedAt time.Time `json:"activated_at"`
}

var ErrTotpSetupRequired = infraerrors.Forbidden("TOTP_SETUP_REQUIRED", "configure TOTP two-factor authentication to continue")

type TotpEnrollmentUserRepository interface {
	CountUsersWithoutTOTP(context.Context) (int, error)
	ListUsersWithoutTOTP(context.Context, int64, int) ([]int64, error)
}

type UserSessionVersionStore interface {
	GetUserSessionVersion(context.Context, int64) (string, error)
	SetUserSessionVersion(context.Context, int64, string) error
}

func (s *SettingService) ForceTotpEnabled(ctx context.Context) (bool, error) {
	policy, err := s.GetForceTotpPolicy(ctx)
	return policy.Enabled, err
}

func (s *SettingService) GetForceTotpPolicy(ctx context.Context) (ForceTotpPolicy, error) {
	policy := ForceTotpPolicy{}
	if s == nil || s.settingRepo == nil {
		return policy, nil
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyForceTotp)
	if errors.Is(err, ErrSettingNotFound) {
		return policy, nil
	}
	if err != nil {
		return policy, fmt.Errorf("read forced TOTP policy: %w", err)
	}
	if value == "true" || value == "false" || value == "" {
		policy.Enabled = value == "true"
		return policy, nil
	}
	if err := json.Unmarshal([]byte(value), &policy); err != nil {
		return policy, fmt.Errorf("decode forced TOTP policy: %w", err)
	}
	if policy.Enabled && (policy.Epoch == "" || policy.ActivatedAt.IsZero()) {
		return policy, errors.New("invalid forced TOTP policy")
	}
	return policy, nil
}

func (s *AuthService) RequiresTotpSetup(ctx context.Context, user *User) (bool, error) {
	if s == nil || s.settingService == nil || user == nil || user.TotpEnabled {
		return false, nil
	}
	return s.settingService.ForceTotpEnabled(ctx)
}

func (s *AuthService) CountUsersWithoutTOTP(ctx context.Context) (int, error) {
	repo, ok := s.userRepo.(TotpEnrollmentUserRepository)
	if !ok {
		return 0, errors.New("TOTP enrollment repository unavailable")
	}
	return repo.CountUsersWithoutTOTP(ctx)
}

func (s *AuthService) UpdateForceTotp(ctx context.Context, enabled bool) error {
	if enabled && !s.settingService.IsTotpEncryptionKeyConfigured() {
		return infraerrors.BadRequest("TOTP_ENCRYPTION_KEY_REQUIRED", "configure a fixed TOTP encryption key before requiring two-factor authentication")
	}
	policy, err := s.settingService.GetForceTotpPolicy(ctx)
	if err != nil {
		return err
	}
	if enabled && !policy.Enabled {
		repo, ok := s.userRepo.(TotpEnrollmentUserRepository)
		if !ok {
			return errors.New("TOTP enrollment repository unavailable")
		}
		if _, ok := s.refreshTokenCache.(UserSessionVersionStore); !ok {
			return errors.New("session revocation store unavailable")
		}
		epoch, err := randomHexString(16)
		if err != nil {
			return err
		}
		policy = ForceTotpPolicy{Enabled: true, Epoch: epoch, ActivatedAt: time.Now().UTC()}
		if err := s.persistForceTotpPolicy(ctx, policy); err != nil {
			return err
		}
		var cursor int64
		for {
			users, err := repo.ListUsersWithoutTOTP(ctx, cursor, 500)
			if err != nil {
				return fmt.Errorf("list users requiring TOTP: %w", err)
			}
			for _, userID := range users {
				if err := s.RevokeAllUserTokens(ctx, userID); err != nil {
					return fmt.Errorf("revoke user %d sessions: %w", userID, err)
				}
				cursor = userID
			}
			if len(users) < 500 {
				break
			}
		}
	}
	policy.Enabled = enabled
	return s.persistForceTotpPolicy(ctx, policy)
}

func (s *AuthService) persistForceTotpPolicy(ctx context.Context, policy ForceTotpPolicy) error {
	encoded, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	return s.settingService.settingRepo.Set(ctx, SettingKeyForceTotp, string(encoded))
}

func (s *AuthService) ValidateTotpPolicySession(ctx context.Context, user *User, epoch string) error {
	if s.settingService == nil {
		return nil
	}
	security, err := s.tokenSecurity(ctx, user.ID)
	if err != nil {
		return ErrServiceUnavailable
	}
	policy := security.policy
	if policy.Epoch == "" || policy.Epoch == epoch {
		return nil
	}
	if user.TotpEnabled && user.TotpEnabledAt != nil && !user.TotpEnabledAt.After(policy.ActivatedAt) {
		return nil
	}
	return ErrTokenRevoked
}

type tokenSecuritySnapshot struct {
	version string
	policy  ForceTotpPolicy
}
type tokenSecuritySnapshotKey struct{}

func (s *AuthService) tokenSecurity(ctx context.Context, userID int64) (tokenSecuritySnapshot, error) {
	if snapshot, ok := ctx.Value(tokenSecuritySnapshotKey{}).(tokenSecuritySnapshot); ok {
		return snapshot, nil
	}
	version, err := s.userSessionVersion(ctx, userID)
	if err != nil {
		return tokenSecuritySnapshot{}, err
	}
	policy, err := s.settingService.GetForceTotpPolicy(ctx)
	return tokenSecuritySnapshot{version: version, policy: policy}, err
}

func (s *AuthService) userSessionVersion(ctx context.Context, userID int64) (string, error) {
	store, ok := s.refreshTokenCache.(UserSessionVersionStore)
	if !ok {
		return "", nil
	}
	return store.GetUserSessionVersion(ctx, userID)
}

func (s *AuthService) ValidateUserSession(ctx context.Context, userID int64, version string) error {
	security, err := s.tokenSecurity(ctx, userID)
	if err != nil {
		return ErrServiceUnavailable
	}
	if security.version != version {
		return ErrTokenRevoked
	}
	return nil
}

func (s *AuthService) ValidateConsoleSession(ctx context.Context, user *User, claims *JWTClaims) (bool, error) {
	security, err := s.tokenSecurity(ctx, user.ID)
	if err != nil {
		return false, ErrServiceUnavailable
	}
	ctx = context.WithValue(ctx, tokenSecuritySnapshotKey{}, security)
	if err := s.ValidateTotpPolicySession(ctx, user, claims.TotpPolicyEpoch); err != nil {
		return false, err
	}
	if err := s.ValidateUserSession(ctx, user.ID, claims.SessionVersion); err != nil {
		return false, err
	}
	return security.policy.Enabled && !user.TotpEnabled, nil
}
