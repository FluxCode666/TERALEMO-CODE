//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

type totpRebindUserRepoStub struct {
	UserRepository
	user          *User
	savedSecret   *string
	enableCalled  bool
	disableCalled bool
}

func (s *totpRebindUserRepoStub) GetByID(context.Context, int64) (*User, error) {
	if s.user == nil {
		return nil, errors.New("user not found")
	}
	clone := *s.user
	return &clone, nil
}

func (s *totpRebindUserRepoStub) UpdateTotpSecret(_ context.Context, _ int64, secret *string) error {
	s.savedSecret = secret
	return nil
}

func (s *totpRebindUserRepoStub) EnableTotp(context.Context, int64) error {
	s.enableCalled = true
	return nil
}

func (s *totpRebindUserRepoStub) DisableTotp(context.Context, int64) error {
	s.disableCalled = true
	return nil
}

type totpRebindCacheStub struct {
	TotpCache
	sessions map[int64]*TotpSetupSession
}

func (c *totpRebindCacheStub) GetSetupSession(_ context.Context, userID int64) (*TotpSetupSession, error) {
	return c.sessions[userID], nil
}

func (c *totpRebindCacheStub) SetSetupSession(_ context.Context, userID int64, session *TotpSetupSession, _ time.Duration) error {
	c.sessions[userID] = session
	return nil
}

func (c *totpRebindCacheStub) DeleteSetupSession(_ context.Context, userID int64) error {
	delete(c.sessions, userID)
	return nil
}

type totpRebindEncryptorStub struct{}

func (totpRebindEncryptorStub) Encrypt(plaintext string) (string, error) {
	return "enc:" + plaintext, nil
}
func (totpRebindEncryptorStub) Decrypt(ciphertext string) (string, error) {
	return ciphertext[len("enc:"):], nil
}

func newTotpRebindService(t *testing.T, user *User, forced bool) (*TotpService, *totpRebindUserRepoStub, *totpRebindCacheStub) {
	t.Helper()
	values := map[string]string{SettingKeyTotpEnabled: "true"}
	if forced {
		values[SettingKeyForceTotp] = "true"
	}
	userRepo := &totpRebindUserRepoStub{user: user}
	cache := &totpRebindCacheStub{sessions: map[int64]*TotpSetupSession{}}
	settingSvc := NewSettingService(&totpVMSettingRepoStub{values: values}, nil)
	return NewTotpService(userRepo, totpRebindEncryptorStub{}, cache, settingSvc, nil, nil), userRepo, cache
}

func newTotpRebindUser(t *testing.T) *User {
	t.Helper()
	enabledAt := time.Now().Add(-24 * time.Hour)
	user := &User{ID: 7, Email: "user@example.com", Role: RoleUser, TotpEnabled: true, TotpEnabledAt: &enabledAt}
	require.NoError(t, user.SetPassword("correct-password"))
	return user
}

func TestTotpRebindReplacesSecretWhileForcedWithoutDisabling(t *testing.T) {
	user := newTotpRebindUser(t)
	svc, userRepo, _ := newTotpRebindService(t, user, true)
	ctx := context.Background()

	// 强制 2FA 下停用仍被拒绝。
	err := svc.Disable(ctx, user.ID, "", "correct-password")
	require.Error(t, err)
	require.False(t, userRepo.disableCalled)

	setup, err := svc.InitiateRebind(ctx, user.ID, "", "correct-password")
	require.NoError(t, err)
	require.NotEmpty(t, setup.Secret)

	code, err := totp.GenerateCode(setup.Secret, time.Now())
	require.NoError(t, err)
	require.NoError(t, svc.CompleteSetup(ctx, user.ID, code, setup.SetupToken))

	require.NotNil(t, userRepo.savedSecret)
	require.Equal(t, "enc:"+setup.Secret, *userRepo.savedSecret)
	// 只替换密钥：不重新 Enable（保留 enabled_at），也从未停用。
	require.False(t, userRepo.enableCalled)
	require.False(t, userRepo.disableCalled)
}

func TestTotpRebindRequiresIdentityVerification(t *testing.T) {
	user := newTotpRebindUser(t)
	svc, _, cache := newTotpRebindService(t, user, true)

	_, err := svc.InitiateRebind(context.Background(), user.ID, "", "")
	require.ErrorIs(t, err, ErrPasswordRequired)

	_, err = svc.InitiateRebind(context.Background(), user.ID, "", "wrong-password")
	require.ErrorIs(t, err, ErrPasswordIncorrect)
	require.Empty(t, cache.sessions)
}

func TestTotpRebindRequiresTotpAlreadyEnabled(t *testing.T) {
	user := newTotpRebindUser(t)
	user.TotpEnabled = false
	svc, _, _ := newTotpRebindService(t, user, false)

	_, err := svc.InitiateRebind(context.Background(), user.ID, "", "correct-password")
	require.ErrorIs(t, err, ErrTotpNotSetup)
}

func TestTotpRebindRejectedWhenTotpDisabledBeforeCompletion(t *testing.T) {
	user := newTotpRebindUser(t)
	svc, userRepo, cache := newTotpRebindService(t, user, false)
	ctx := context.Background()

	setup, err := svc.InitiateRebind(ctx, user.ID, "", "correct-password")
	require.NoError(t, err)

	userRepo.user.TotpEnabled = false
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	require.NoError(t, err)

	err = svc.CompleteSetup(ctx, user.ID, code, setup.SetupToken)
	require.ErrorIs(t, err, ErrTotpNotSetup)
	require.Nil(t, userRepo.savedSecret)
	require.Empty(t, cache.sessions)
}

func TestTotpInitialSetupStillEnablesTotp(t *testing.T) {
	user := newTotpRebindUser(t)
	user.TotpEnabled = false
	user.TotpEnabledAt = nil
	svc, userRepo, _ := newTotpRebindService(t, user, true)
	ctx := context.Background()

	setup, err := svc.InitiateSetup(ctx, user.ID, "", "correct-password")
	require.NoError(t, err)
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	require.NoError(t, err)
	require.NoError(t, svc.CompleteSetup(ctx, user.ID, code, setup.SetupToken))

	require.NotNil(t, userRepo.savedSecret)
	require.True(t, userRepo.enableCalled)
}

func TestTotpStatusReportsForcedRequirement(t *testing.T) {
	user := newTotpRebindUser(t)

	svcForced, _, _ := newTotpRebindService(t, user, true)
	status, err := svcForced.GetStatus(context.Background(), user.ID)
	require.NoError(t, err)
	require.True(t, status.Required)
	require.True(t, status.Enabled)

	svcOptional, _, _ := newTotpRebindService(t, user, false)
	status, err = svcOptional.GetStatus(context.Background(), user.ID)
	require.NoError(t, err)
	require.False(t, status.Required)
}
