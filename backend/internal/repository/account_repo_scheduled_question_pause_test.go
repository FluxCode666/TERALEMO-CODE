//go:build unit

package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestClearScheduledQuestionAutoPauseClearsAllPlanMarkersForOneAccount(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := newAccountRepositoryWithSQL(nil, db, nil)
	mock.ExpectBegin()
	expectManualAutoPauseLocks(mock, 42)
	mock.ExpectExec(`(?s)UPDATE scheduled_question_test_account_states.*SET auto_paused = false, auto_paused_at = NULL, updated_at = NOW\(\).*WHERE account_id = \$1 AND`).
		WithArgs(int64(42)).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()
	require.NoError(t, repo.ClearScheduledQuestionAutoPause(context.Background(), 42))
	require.NoError(t, mock.ExpectationsWereMet())
}

func expectManualAutoPauseLocks(mock sqlmock.Sqlmock, accountID int64) {
	mock.ExpectExec(regexp.QuoteMeta(`SELECT pg_advisory_xact_lock(hashtextextended('scheduled-question-auto-pause:' || $1::text, 0))`)).
		WithArgs(accountID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)SELECT plan_id, auto_paused FROM scheduled_question_test_account_states.*WHERE account_id = \$1 ORDER BY plan_id FOR UPDATE`).
		WithArgs(accountID).WillReturnRows(sqlmock.NewRows([]string{"plan_id", "auto_paused"}).AddRow(10, true).AddRow(20, true)).RowsWillBeClosed()
}

func TestClearScheduledQuestionAutoPauseRollbackAndFailurePropagation(t *testing.T) {
	for _, stage := range []string{"begin", "advisory", "rows", "update", "commit"} {
		t.Run(stage, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			failure := errors.New("injected " + stage + " failure")
			if stage == "begin" {
				mock.ExpectBegin().WillReturnError(failure)
			} else {
				mock.ExpectBegin()
				lock := mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(int64(42))
				if stage == "advisory" {
					lock.WillReturnError(failure)
				} else {
					lock.WillReturnResult(sqlmock.NewResult(0, 1))
					rows := mock.ExpectQuery("SELECT plan_id, auto_paused").WithArgs(int64(42))
					if stage == "rows" {
						rows.WillReturnError(failure)
					} else {
						rows.WillReturnRows(sqlmock.NewRows([]string{"plan_id", "auto_paused"}))
						update := mock.ExpectExec("UPDATE scheduled_question_test_account_states").WithArgs(int64(42))
						if stage == "update" {
							update.WillReturnError(failure)
						} else {
							update.WillReturnResult(sqlmock.NewResult(0, 1))
						}
					}
				}
				if stage == "commit" {
					mock.ExpectCommit().WillReturnError(failure)
				} else {
					mock.ExpectRollback()
				}
			}
			repo := newAccountRepositoryWithSQL(nil, db, nil)
			require.ErrorIs(t, repo.ClearScheduledQuestionAutoPause(context.Background(), 42), failure)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestClearScheduledQuestionAutoPauseReusesCallerTransactions(t *testing.T) {
	for _, kind := range []string{"sql", "ent", "context ent", "client fallback"} {
		t.Run(kind, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			mock.ExpectBegin()
			ctx := context.Background()
			var repo *accountRepository
			var rollback func() error
			if kind == "sql" {
				tx, err := db.BeginTx(ctx, nil)
				require.NoError(t, err)
				repo, rollback = newAccountRepositoryWithSQL(nil, tx, nil), tx.Rollback
			} else {
				client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
				tx, err := client.Tx(ctx)
				require.NoError(t, err)
				rollback = tx.Rollback
				if kind == "context ent" {
					ctx = dbent.NewTxContext(ctx, tx)
					repo = newAccountRepositoryWithSQL(client, db, nil)
				} else if kind == "client fallback" {
					repo = newAccountRepositoryWithSQL(tx.Client(), nil, nil)
				} else {
					repo = newAccountRepositoryWithSQL(tx.Client(), tx, nil)
				}
			}
			expectManualAutoPauseLocks(mock, 42)
			mock.ExpectExec("UPDATE scheduled_question_test_account_states").WithArgs(int64(42)).WillReturnResult(sqlmock.NewResult(0, 2))
			require.NoError(t, repo.ClearScheduledQuestionAutoPause(ctx, 42))
			mock.ExpectRollback()
			require.NoError(t, rollback(), "clear must leave the caller transaction open")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestClearScheduledQuestionAutoPauseRequiresTransactionalExecutor(t *testing.T) {
	exec := &recordingSQLExecutor{result: rowsAffectedResult(3)}
	repo := newAccountRepositoryWithSQL(nil, exec, nil)
	require.ErrorContains(t, repo.ClearScheduledQuestionAutoPause(context.Background(), 42), "requires a transaction")
	require.Empty(t, exec.execQueries, "never clear ownership in autocommit mode")
}
