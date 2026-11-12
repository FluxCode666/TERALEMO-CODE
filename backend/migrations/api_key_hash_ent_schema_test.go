package migrations_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func TestAPIKeyHashGeneratedSchemaRejectsPlaintext(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:api_key_hash_schema?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.ExecContext(ctx, "PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	user, err := client.User.Create().SetEmail("api-key-schema@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	raw := "sk-generated-schema-test-1234"
	sum := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(sum[:])
	key, err := client.APIKey.Create().SetUserID(user.ID).SetName("hashed").SetKeyHash(hash).SetKeyHint("sk-gene...1234").Save(ctx)
	require.NoError(t, err)
	loaded, err := client.APIKey.Get(ctx, key.ID)
	require.NoError(t, err)
	require.Empty(t, loaded.Key)
	require.Equal(t, hash, loaded.KeyHash)
	require.Equal(t, "sk-gene...1234", loaded.KeyHint)
	var stored sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, "SELECT key FROM api_keys WHERE id = ?", key.ID).Scan(&stored))
	require.False(t, stored.Valid)
	encoded, err := json.Marshal(loaded)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), hash, "credential hashes must not leak through entity serialization")
	require.NotContains(t, string(encoded), raw)
	require.Contains(t, string(encoded), "sk-gene...1234")

	_, err = client.APIKey.Create().SetUserID(user.ID).SetName("duplicate").SetKeyHash(hash).SetKeyHint("hint").Save(ctx)
	require.True(t, dbent.IsConstraintError(err))
	_, err = client.APIKey.UpdateOneID(key.ID).SetKey(raw).Save(ctx)
	require.True(t, dbent.IsConstraintError(err), "the generated schema must enforce the no-plaintext invariant")
}
