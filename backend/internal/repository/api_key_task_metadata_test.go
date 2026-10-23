package repository

import (
	"context"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRepositoryTaskMetadataUsesThreeSecretFreeQueries(t *testing.T) {
	var queries []string
	matcher := sqlmock.QueryMatcherFunc(func(expected, actual string) error {
		queries = append(queries, actual)
		return sqlmock.QueryMatcherRegexp.Match(expected, actual)
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := newAPIKeyRepositoryWithSQL(client, db)

	mock.ExpectQuery(`SELECT .* FROM "api_keys"`).
		WithArgs(int64(7), int64(8), int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "name", "group_id"}).
			AddRow(int64(7), int64(10), "first-key", int64(3)).
			AddRow(int64(8), int64(10), "second-key", int64(3)))
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "username"}).
			AddRow(int64(10), "owner@example.com", "Owner"))
	mock.ExpectQuery(`SELECT .* FROM "groups"`).
		WithArgs(int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(int64(3), "Video group"))
	metadata, err := repo.GetTaskMetadataByIDs(context.Background(), []int64{7, 8, 9})
	require.NoError(t, err)
	require.Len(t, metadata, 2, "missing and deleted keys must stay absent")
	require.Equal(t, "first-key", metadata[7].KeyName)
	require.Equal(t, "second-key", metadata[8].KeyName)
	require.Equal(t, "owner@example.com", metadata[7].UserEmail)
	require.Equal(t, "Owner", metadata[8].UserName)
	require.Equal(t, "Video group", metadata[7].GroupName)
	require.Equal(t, int64(3), *metadata[7].GroupID)
	require.NoError(t, mock.ExpectationsWereMet(), "the whole page must require exactly three queries")
	require.Len(t, queries, 3)
	require.Contains(t, queries[0], `"deleted_at" IS NULL`)
	for _, query := range queries {
		projection, _, found := strings.Cut(query, " FROM ")
		require.True(t, found)
		for _, sensitive := range []string{`"key"`, `"password_hash"`, `"credentials"`, `"balance"`, `"quota"`} {
			require.NotContains(t, projection, sensitive)
		}
		require.NotContains(t, query, "enterprise")
	}
	metadata, err = repo.GetTaskMetadataByIDs(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, metadata)
	require.Len(t, queries, 3)
}
