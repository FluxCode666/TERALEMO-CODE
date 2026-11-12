package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyHashAndDisplayHint(t *testing.T) {
	require.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", HashAPIKey("abc"))
	for _, tc := range []struct{ raw, hint string }{
		{"sk-1234567890123456", "sk-1234...3456"},
		{"12345678901", "123..."},
		{"abc", "a..."},
		{"a", "..."},
		{"", "..."},
		{"测试凭据长于十二个字符确实够长", "测试凭据长于十...确实够长"},
	} {
		require.Equal(t, tc.hint, MaskAPIKey(tc.raw))
		require.Equal(t, tc.hint, (&APIKey{Key: tc.raw}).DisplayHint())
	}
	svc := &APIKeyService{}
	require.NoError(t, svc.ValidateCustomKey(strings.Repeat("a", MaxAPIKeyCredentialBytes)))
	require.Error(t, svc.ValidateCustomKey(strings.Repeat("a", MaxAPIKeyCredentialBytes+1)))
}
