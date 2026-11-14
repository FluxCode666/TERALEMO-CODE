//go:build unit

package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminAndModerationCredentialHintsMatchAPIKey(t *testing.T) {
	for _, secret := range []string{"a", "4b9e6a1f80c2d573", "sk-0123456789abcdef", "tenant_" + strings.Repeat("a", 32), AdminAPIKeyPrefix + strings.Repeat("b", 64)} {
		t.Run(secret, func(t *testing.T) {
			settings := NewSettingService(&settingRepoStub{values: map[string]string{SettingKeyAdminAPIKey: secret}}, nil)
			hint, exists, err := settings.GetAdminAPIKeyStatus(t.Context())
			require.NoError(t, err)
			require.True(t, exists)
			require.Equal(t, MaskAPIKey(secret), hint)
			require.Equal(t, MaskAPIKey(secret), maskSecretTail(" "+secret+" "))
		})
	}
	require.Empty(t, maskSecretTail(" "))
}

func TestAdminCredentialHintMissingAndRepositoryError(t *testing.T) {
	for _, values := range []map[string]string{nil, {SettingKeyAdminAPIKey: ""}} {
		settings := NewSettingService(&settingRepoStub{values: values}, nil)
		hint, exists, err := settings.GetAdminAPIKeyStatus(t.Context())
		require.NoError(t, err)
		require.False(t, exists)
		require.Empty(t, hint)
	}
	wantErr := errors.New("settings unavailable")
	settings := NewSettingService(&settingRepoStub{err: wantErr}, nil)
	hint, exists, err := settings.GetAdminAPIKeyStatus(t.Context())
	require.ErrorIs(t, err, wantErr)
	require.False(t, exists)
	require.Empty(t, hint)
}

func TestStoredAuditCredentialFragmentsAreFullyHidden(t *testing.T) {
	actorID := int64(42)
	for _, fragment := range []string{"x-api-key 4b9e6a****d573", "Bearer eyJhbG****1234", "unknown-fragment", "...", ""} {
		entry := &AuditLog{CredentialMasked: fragment, ActorUserID: &actorID, Action: "test"}
		safe := redactStoredAuditLog(entry)
		want := "..."
		if fragment == "" {
			want = ""
		}
		require.Equal(t, want, safe.CredentialMasked)
		require.Equal(t, fragment, entry.CredentialMasked, "read redaction must not mutate repository snapshots")
		require.Equal(t, entry.ActorUserID, safe.ActorUserID)
		require.Equal(t, entry.Action, safe.Action)
	}
}
