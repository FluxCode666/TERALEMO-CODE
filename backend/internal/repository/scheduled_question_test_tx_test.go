//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestScheduledQuestionAccountSetUsesVerdictTransactionAndOutbox(t *testing.T) {
	for _, value := range []bool{false, true} {
		t.Run(map[bool]string{false: "pause", true: "resume"}[value], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			mock.ExpectBegin()
			tx, err := db.BeginTx(context.Background(), nil)
			require.NoError(t, err)
			ctx := scheduledQuestionSchedulingTxContext(context.Background(), tx)
			mock.ExpectExec(`UPDATE accounts SET schedulable = \$2, updated_at = NOW\(\)`).
				WithArgs(int64(42), value).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(`INSERT INTO scheduler_outbox`).WithArgs(service.SchedulerOutboxEventAccountChanged, int64(42), nil, nil, sqlmock.AnyArg()).
				WillReturnResult(sqlmock.NewResult(1, 1))
			// A nil client and executor ensure this path cannot silently use another connection.
			repo := newAccountRepositoryWithSQL(nil, nil, nil)
			require.NoError(t, repo.SetSchedulable(ctx, 42, value))
			transaction := ctx.Value(scheduledQuestionSchedulingTxKey{}).(*scheduledQuestionSchedulingTransaction)
			if !value {
				require.Len(t, transaction.afterCommit, 1, "pause cache refresh waits for commit")
			} else {
				require.Empty(t, transaction.afterCommit)
			}
			mock.ExpectCommit()
			require.NoError(t, tx.Commit())
			runScheduledQuestionSchedulingAfterCommit(ctx)
			require.Empty(t, transaction.afterCommit)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestScheduledQuestionAccountSetTransactionFailuresDoNotScheduleCacheRefresh(t *testing.T) {
	for _, stage := range []string{"update", "missing", "rows affected", "outbox"} {
		t.Run(stage, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			mock.ExpectBegin()
			tx, err := db.BeginTx(context.Background(), nil)
			require.NoError(t, err)
			ctx := scheduledQuestionSchedulingTxContext(context.Background(), tx)
			failure := errors.New("injected failure")
			update := mock.ExpectExec(`UPDATE accounts SET schedulable`).WithArgs(int64(42), false)
			switch stage {
			case "update":
				update.WillReturnError(failure)
			case "missing":
				update.WillReturnResult(sqlmock.NewResult(0, 0))
			case "rows affected":
				update.WillReturnResult(sqlmock.NewErrorResult(failure))
			case "outbox":
				update.WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`INSERT INTO scheduler_outbox`).WillReturnError(failure)
			}
			repo := newAccountRepositoryWithSQL(nil, nil, nil)
			err = repo.SetSchedulable(ctx, 42, false)
			if stage == "missing" {
				require.ErrorIs(t, err, service.ErrAccountNotFound)
			} else {
				require.ErrorIs(t, err, failure)
			}
			require.Empty(t, ctx.Value(scheduledQuestionSchedulingTxKey{}).(*scheduledQuestionSchedulingTransaction).afterCommit)
			mock.ExpectRollback()
			require.NoError(t, tx.Rollback())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
