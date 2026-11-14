//go:build unit

package service

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type forceTotpUserRepo struct {
	UserRepository
	users map[int64]*User
}

func (repo *forceTotpUserRepo) GetByID(_ context.Context, userID int64) (*User, error) {
	return repo.users[userID], nil
}
func (repo *forceTotpUserRepo) CountUsersWithoutTOTP(context.Context) (int, error) {
	count := 0
	for _, user := range repo.users {
		if !user.TotpEnabled {
			count++
		}
	}
	return count, nil
}
func (repo *forceTotpUserRepo) ListUsersWithoutTOTP(_ context.Context, cursor int64, limit int) ([]int64, error) {
	users := []int64{}
	for userID, user := range repo.users {
		if userID > cursor && !user.TotpEnabled {
			users = append(users, userID)
		}
	}
	sort.Slice(users, func(first, second int) bool { return users[first] < users[second] })
	if len(users) > limit {
		users = users[:limit]
	}
	return users, nil
}

type forceTotpTokenCache struct {
	RefreshTokenCache
	versions map[int64]string
	tokens   map[string]*RefreshTokenData
	revoked  []int64
}

func (cache *forceTotpTokenCache) GetUserSessionVersion(_ context.Context, userID int64) (string, error) {
	return cache.versions[userID], nil
}
func (cache *forceTotpTokenCache) SetUserSessionVersion(_ context.Context, userID int64, version string) error {
	cache.versions[userID] = version
	return nil
}
func (cache *forceTotpTokenCache) DeleteUserRefreshTokens(_ context.Context, userID int64) error {
	cache.revoked = append(cache.revoked, userID)
	for hash, data := range cache.tokens {
		if data.UserID == userID {
			delete(cache.tokens, hash)
		}
	}
	return nil
}
func (cache *forceTotpTokenCache) StoreRefreshToken(_ context.Context, hash string, data *RefreshTokenData, _ time.Duration) error {
	cache.tokens[hash] = data
	return nil
}
func (cache *forceTotpTokenCache) GetRefreshToken(_ context.Context, hash string) (*RefreshTokenData, error) {
	data, ok := cache.tokens[hash]
	if !ok {
		return nil, ErrRefreshTokenNotFound
	}
	return data, nil
}
func (cache *forceTotpTokenCache) AddToUserTokenSet(context.Context, int64, string, time.Duration) error {
	return nil
}
func (cache *forceTotpTokenCache) AddToFamilyTokenSet(context.Context, string, string, time.Duration) error {
	return nil
}

func TestForceTotpRevokesOldAccessAndRefreshSessions(t *testing.T) {
	ctx := context.Background()
	settings := newSecurityTestSettings()
	settings.cfg.Totp.EncryptionKeyConfigured = true
	users := &forceTotpUserRepo{users: map[int64]*User{
		1: {ID: 1, Email: "user@example.com", Status: StatusActive},
		2: {ID: 2, Email: "admin@example.com", Role: RoleAdmin, Status: StatusActive},
		3: {ID: 3, Email: "secure@example.com", Status: StatusActive, TotpEnabled: true},
	}}
	totpTime := time.Now().Add(-time.Hour)
	users.users[3].TotpEnabledAt = &totpTime
	cache := &forceTotpTokenCache{versions: map[int64]string{}, tokens: map[string]*RefreshTokenData{}}
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "test-secret", ExpireHour: 1, RefreshTokenExpireDays: 1}}
	auth := NewAuthService(nil, users, nil, cache, cfg, settings, nil, nil, nil, nil, nil, nil, nil)
	old, err := auth.GenerateTokenPair(ctx, users.users[1], "")
	require.NoError(t, err)
	oldClaims, err := auth.ValidateToken(old.AccessToken)
	require.NoError(t, err)
	require.NoError(t, auth.UpdateForceTotp(ctx, true))
	require.ElementsMatch(t, []int64{1, 2}, cache.revoked)
	require.ErrorIs(t, auth.ValidateUserSession(ctx, 1, oldClaims.SessionVersion), ErrTokenRevoked)
	require.ErrorIs(t, auth.ValidateTotpPolicySession(ctx, users.users[1], oldClaims.TotpPolicyEpoch), ErrTokenRevoked)
	_, err = auth.RefreshTokenPair(ctx, old.RefreshToken)
	require.ErrorIs(t, err, ErrRefreshTokenInvalid)
	require.NoError(t, auth.ValidateTotpPolicySession(ctx, users.users[3], ""))
	newPair, err := auth.GenerateTokenPair(ctx, users.users[1], "")
	require.NoError(t, err)
	require.True(t, newPair.RequiresTotpSetup)
	newClaims, err := auth.ValidateToken(newPair.AccessToken)
	require.NoError(t, err)
	require.NoError(t, auth.ValidateUserSession(ctx, 1, newClaims.SessionVersion))
	require.NoError(t, auth.ValidateTotpPolicySession(ctx, users.users[1], newClaims.TotpPolicyEpoch))
	users.users[1].TotpEnabled = true
	now := time.Now()
	users.users[1].TotpEnabledAt = &now
	required, err := auth.RequiresTotpSetup(ctx, users.users[1])
	require.NoError(t, err)
	require.False(t, required)
	require.NoError(t, auth.ValidateTotpPolicySession(ctx, users.users[1], newClaims.TotpPolicyEpoch))
	require.ErrorIs(t, auth.ValidateTotpPolicySession(ctx, users.users[1], ""), ErrTokenRevoked)
	require.NoError(t, auth.UpdateForceTotp(ctx, false))
	require.ErrorIs(t, auth.ValidateTotpPolicySession(ctx, users.users[1], ""), ErrTokenRevoked)
}

func TestForceTotpRejectsMissingFixedEncryptionKey(t *testing.T) {
	settings := newSecurityTestSettings()
	auth := &AuthService{settingService: settings}
	require.ErrorContains(t, auth.UpdateForceTotp(context.Background(), true), "TOTP_ENCRYPTION_KEY_REQUIRED")
	enabled, err := settings.ForceTotpEnabled(context.Background())
	require.NoError(t, err)
	require.False(t, enabled)
}
