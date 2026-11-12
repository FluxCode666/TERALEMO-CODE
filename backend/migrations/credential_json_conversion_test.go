package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredentialMigrationsLimitJSONConversionFallback(t *testing.T) {
	for _, name := range []string{"264_api_key_hash_storage.sql", "265_credential_hint_cleanup.sql"} {
		t.Run(name, func(t *testing.T) {
			content, err := FS.ReadFile(name)
			require.NoError(t, err)
			require.Contains(t, string(content), "EXCEPTION WHEN invalid_text_representation OR untranslatable_character THEN")
			require.NotContains(t, string(content), "WHEN OTHERS", "unexpected database errors must abort the migration")
		})
	}
}
