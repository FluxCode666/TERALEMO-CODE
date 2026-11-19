package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func modelFilterAccountRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "platform", "type", "credentials", "extra", "parent_account_id"}).
		AddRow(int64(1), service.PlatformAnthropic, service.AccountTypeAPIKey, []byte(`{"model_mapping":{"claude-opus-5":"upstream-opus"}}`), []byte(`{}`), nil).
		AddRow(int64(2), service.PlatformAnthropic, service.AccountTypeAPIKey, []byte(`{"model_mapping":{"claude-haiku-5-5":"upstream-haiku"}}`), []byte(`{}`), nil).
		AddRow(int64(3), service.PlatformAnthropic, service.AccountTypeAPIKey, []byte(`{"model_mapping":{"claude-*":"upstream-claude"}}`), []byte(`{}`), nil).
		AddRow(int64(4), service.PlatformGemini, service.AccountTypeOAuth, nil, []byte(`{}`), nil)
}

func newModelFilterRepository(t *testing.T) (*accountRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	return newAccountRepositoryWithSQL(client, db, nil), mock
}

func TestListWithFilters_ModelFilterAppliesBeforePagination(t *testing.T) {
	repo, mock := newModelFilterRepository(t)

	// 候选集按 SQL 顺序取回后由模型支持语义过滤：精确映射、通配符映射与无映射账号命中，
	// 映射了其它模型的账号被排除；分页在过滤结果之上进行。
	mock.ExpectQuery(`SELECT`).WillReturnRows(modelFilterAccountRows())
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "account_id", "group_id", "priority", "created_at"}))

	accounts, result, err := repo.ListWithFilters(
		context.Background(),
		pagination.PaginationParams{Page: 1, PageSize: 1, SortBy: "name", SortOrder: "asc"},
		"", "", "", "", 0, "", "claude-opus-5",
	)
	require.NoError(t, err)
	require.Equal(t, int64(3), result.Total)
	require.Equal(t, 3, result.Pages)
	require.Len(t, accounts, 1)
	require.Equal(t, int64(1), accounts[0].ID)

	// 第 2 页落在通配符命中的账号上，证明分页发生在过滤之后。
	mock.ExpectQuery(`SELECT`).WillReturnRows(modelFilterAccountRows())
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "account_id", "group_id", "priority", "created_at"}))
	accounts, result, err = repo.ListWithFilters(
		context.Background(),
		pagination.PaginationParams{Page: 2, PageSize: 1, SortBy: "name", SortOrder: "asc"},
		"", "", "", "", 0, "", "claude-opus-5",
	)
	require.NoError(t, err)
	require.Equal(t, int64(3), result.Total)
	require.Len(t, accounts, 1)
	require.Equal(t, int64(3), accounts[0].ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListWithFilters_ModelFilterKeepsUnfilteredPathWhenEmpty(t *testing.T) {
	repo, mock := newModelFilterRepository(t)

	mock.ExpectQuery(`SELECT COUNT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`SELECT`).WillReturnRows(modelFilterAccountRows())
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "account_id", "group_id", "priority", "created_at"}))

	accounts, result, err := repo.ListWithFilters(
		context.Background(),
		pagination.PaginationParams{Page: 1, PageSize: 2, SortBy: "name", SortOrder: "asc"},
		"", "", "", "", 0, "", "",
	)
	require.NoError(t, err)
	// 无模型筛选时仍走 Count + SQL 分页路径（total 来自 COUNT 而非本地过滤）。
	require.Equal(t, int64(2), result.Total)
	require.NotEmpty(t, accounts)
	require.NoError(t, mock.ExpectationsWereMet())
}
