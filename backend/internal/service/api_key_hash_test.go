package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyHashAndDisplayHint(t *testing.T) {
	require.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", HashAPIKey("abc"))
	for _, tc := range []struct{ raw, hint string }{
		{"sk-1234567890123456", "sk-..."},
		{"12345678901", "..."},
		{"abc", "..."},
		{"a", "..."},
		{"", "..."},
		{"测试凭据长于十二个字符确实够长", "..."},
		{strings.Repeat("a", 24), "..."},
		{strings.Repeat("a", 25), "...a"},
		{strings.Repeat("a", 26), "...aa"},
		{strings.Repeat("a", 27), "...aaa"},
		{strings.Repeat("a", 28), "...aaaa"},
		{"sk-" + strings.Repeat("a", 64), "sk-...aaaa"},
	} {
		require.Equal(t, tc.hint, MaskAPIKey(tc.raw))
		require.Equal(t, tc.hint, (&APIKey{Key: tc.raw}).DisplayHint())
	}
	svc := &APIKeyService{}
	require.ErrorIs(t, svc.ValidateCustomKey(strings.Repeat("a", 31)), ErrAPIKeyTooShort)
	require.NoError(t, svc.ValidateCustomKey(strings.Repeat("a", 32)))
	require.NoError(t, svc.ValidateCustomKey(strings.Repeat("a", MaxAPIKeyCredentialBytes)))
	require.ErrorIs(t, svc.ValidateCustomKey(strings.Repeat("a", MaxAPIKeyCredentialBytes+1)), ErrAPIKeyTooLong)
}

func TestAPIKeyHintPreservesHiddenCharactersAtEveryLength(t *testing.T) {
	for _, char := range []string{"a", "凭"} {
		for _, prefix := range []string{"", "sk-"} {
			for length := 0; length <= 256; length++ {
				hint := MaskAPIKey(prefix + strings.Repeat(char, length))
				require.True(t, strings.HasPrefix(hint, prefix+"..."))
				visible := len([]rune(strings.TrimPrefix(hint, prefix+"...")))
				hidden := length - visible
				require.LessOrEqual(t, visible, 4)
				if length < 24 {
					require.Zero(t, visible, "short legacy credentials must hide every character")
				} else {
					require.GreaterOrEqual(t, hidden, 24)
				}
				require.GreaterOrEqual(t, hidden*4, length*3, "prefix=%q length=%d", prefix, length)
			}
		}
	}
	// An arbitrary delimiter or user-selected prefix is not public metadata.
	require.Equal(t, "...", MaskAPIKey("tenant_0123456789abcdef"))
}
