package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type scheduledQuestionSchedulingTxKey struct{}

type scheduledQuestionSchedulingTransaction struct {
	tx          *sql.Tx
	afterCommit []func()
}

func scheduledQuestionSchedulingTxContext(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, scheduledQuestionSchedulingTxKey{}, &scheduledQuestionSchedulingTransaction{tx: tx})
}

func runScheduledQuestionSchedulingAfterCommit(ctx context.Context) {
	if transaction, ok := ctx.Value(scheduledQuestionSchedulingTxKey{}).(*scheduledQuestionSchedulingTransaction); ok {
		for _, afterCommit := range transaction.afterCommit {
			afterCommit()
		}
		transaction.afterCommit = nil
	}
}

func (r *accountRepository) setScheduledQuestionSchedulable(ctx context.Context, transaction *scheduledQuestionSchedulingTransaction, id int64, schedulable bool) error {
	result, err := transaction.tx.ExecContext(ctx, `UPDATE accounts SET schedulable = $2, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`, id, schedulable)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAccountNotFound
	}
	if err := enqueueSchedulerOutbox(ctx, transaction.tx, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		return err
	}
	if !schedulable {
		// Cache reads must observe the committed account rather than block on this transaction.
		transaction.afterCommit = append(transaction.afterCommit, func() { r.syncSchedulerAccountSnapshotDetached(ctx, id) })
	}
	return nil
}
