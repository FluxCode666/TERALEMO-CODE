//go:build unit

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/outbound"
	"github.com/stretchr/testify/require"
)

func TestAccountOutboundURLsRejectedBeforeWrites(t *testing.T) {
	require.NoError(t, outbound.Configure(nil))
	t.Cleanup(func() { require.NoError(t, outbound.Configure([]string{"127.0.0.0/8", "::1/128"})) })
	for _, writePath := range []string{"create", "create-helper", "update", "bulk", "account-create", "account-update", "crs-create", "crs-update"} {
		t.Run(writePath, func(t *testing.T) {
			credentials := map[string]any{"base_url": "http://127.0.0.1:8080"}
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: credentials}
			repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{1: account}}
			admin := &adminServiceImpl{accountRepo: repo}
			accounts := NewAccountService(repo, nil)
			crs := &CRSSyncService{accountRepo: repo}
			var err error
			switch writePath {
			case "create":
				_, err = admin.CreateAccount(t.Context(), &CreateAccountInput{Credentials: credentials})
			case "create-helper":
				_, err = buildAccountForCreate(&CreateAccountInput{Credentials: credentials}, nil)
			case "update":
				_, err = admin.UpdateAccount(t.Context(), 1, &UpdateAccountInput{Credentials: credentials})
			case "bulk":
				_, err = admin.BulkUpdateAccounts(t.Context(), &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Credentials: credentials})
			case "account-create":
				_, err = accounts.Create(t.Context(), CreateAccountRequest{Credentials: credentials})
			case "account-update":
				_, err = accounts.Update(t.Context(), 1, UpdateAccountRequest{Credentials: &credentials})
			case "crs-create":
				err = crs.createCRSAccount(t.Context(), account)
			case "crs-update":
				err = crs.updateExistingCRSAccount(t.Context(), account, false)
			}
			require.ErrorContains(t, err, "not allowed")
			require.Nil(t, repo.createAccount)
			require.Empty(t, repo.updatedAccounts)
			require.Zero(t, repo.bulkUpdateCalls)
		})
	}
	for _, credentials := range []map[string]any{
		{"image_base_url": "http://[::1]"},
		{"api_base_urls": map[string]any{"chat_completions": "http://169.254.169.254"}},
	} {
		require.ErrorContains(t, validateAccountOutboundURLs(credentials), "not allowed")
	}
	require.NoError(t, validateAccountOutboundURLs(map[string]any{"base_url": "http://8.8.8.8:8787"}))
	require.NoError(t, outbound.Configure([]string{"127.0.0.1:8787"}))
	require.NoError(t, validateAccountOutboundURLs(map[string]any{"base_url": "http://127.0.0.1:8787"}))
}

func TestOIDCOutboundSettingsRejectedBeforeWrite(t *testing.T) {
	require.NoError(t, outbound.Configure(nil))
	t.Cleanup(func() { require.NoError(t, outbound.Configure([]string{"127.0.0.0/8", "::1/128"})) })
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, nil)
	err := svc.UpdateSettings(t.Context(), &SystemSettings{OIDCConnectTokenURL: "http://127.0.0.1:8080/token"})
	require.ErrorContains(t, err, "not allowed")
	require.Nil(t, repo.updates)
}
