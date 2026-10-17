//go:build unit

package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseEmailFirstBindSucceedsWithoutBalanceGift(t *testing.T) {
	for _, status := range []string{service.StatusActive, service.StatusDisabled} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			cache := &emailBindCacheStub{data: &service.VerificationCodeData{Code: "123456", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}}
			s, _, client := newAuthServiceForEmailBind(t, map[string]string{
				service.SettingKeyAuthSourceDefaultEmailBalance:          "8.5",
				service.SettingKeyAuthSourceDefaultEmailConcurrency:      "4",
				service.SettingKeyAuthSourceDefaultEmailGrantOnFirstBind: "true",
			}, cache, nil)
			u, err := client.User.Create().SetEmail("enterprise-oauth" + service.LinuxDoConnectSyntheticEmailDomain).SetPasswordHash("hash").SetBalance(2.5).SetConcurrency(1).Save(ctx)
			require.NoError(t, err)
			e, err := client.Enterprise.Create().SetName("enterprise-first-bind").Save(ctx)
			require.NoError(t, err)
			m, err := client.EnterpriseMember.Create().SetEnterpriseID(e.ID).SetUserID(u.ID).SetStatus(status).Save(ctx)
			require.NoError(t, err)
			updated, err := s.BindEmailIdentity(ctx, u.ID, "bound@example.com", "123456", "new-password")
			require.NoError(t, err)
			require.Equal(t, "bound@example.com", updated.Email)
			stored, err := client.User.Get(ctx, u.ID)
			require.NoError(t, err)
			require.Equal(t, 2.5, stored.Balance)
			require.Equal(t, 5, stored.Concurrency)
			require.Equal(t, 1, countProviderGrantRecords(t, client, u.ID, "email", "first_bind"))
			// Skipping the gift is permanent, including after membership ends.
			_, err = client.EnterpriseMember.UpdateOneID(m.ID).SetDeletedAt(time.Now()).Save(ctx)
			require.NoError(t, err)
			require.NoError(t, s.ApplyProviderDefaultSettingsOnFirstBind(ctx, u.ID, "email"))
			stored, err = client.User.Get(ctx, u.ID)
			require.NoError(t, err)
			require.Equal(t, 2.5, stored.Balance)
		})
	}
}

func TestEnterpriseOAuthFirstBindSkipsOnlyBalanceGift(t *testing.T) {
	ctx := context.Background()
	assigner := &emailBindDefaultSubAssignerStub{}
	s, _, client := newAuthServiceForEmailBind(t, map[string]string{
		service.SettingKeyAuthSourceDefaultOIDCBalance:          "10",
		service.SettingKeyAuthSourceDefaultOIDCConcurrency:      "2",
		service.SettingKeyAuthSourceDefaultOIDCSubscriptions:    `[{"group_id":11,"validity_days":30}]`,
		service.SettingKeyAuthSourceDefaultOIDCGrantOnFirstBind: "true",
	}, nil, assigner)
	u, err := client.User.Create().SetEmail("enterprise-oidc@example.com").SetPasswordHash("hash").SetBalance(3).SetConcurrency(1).Save(ctx)
	require.NoError(t, err)
	e, err := client.Enterprise.Create().SetName("enterprise-oidc-grant").Save(ctx)
	require.NoError(t, err)
	_, err = client.EnterpriseMember.Create().SetEnterpriseID(e.ID).SetUserID(u.ID).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, s.ApplyProviderDefaultSettingsOnFirstBind(ctx, u.ID, "oidc"))
	require.NoError(t, s.ApplyProviderDefaultSettingsOnFirstBind(ctx, u.ID, "oidc"))
	stored, err := client.User.Get(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, 3.0, stored.Balance)
	require.Equal(t, 3, stored.Concurrency)
	require.Len(t, assigner.calls, 1)
	require.Equal(t, 1, countProviderGrantRecords(t, client, u.ID, "oidc", "first_bind"))
}
