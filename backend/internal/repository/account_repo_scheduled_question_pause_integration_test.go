//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestAccountRepositoryManualPauseOwnershipClearAndAutomaticSetIsolation(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	repo := newAccountRepositoryWithSQL(tx.Client(), tx, nil)
	account := mustCreateAccount(t, tx.Client(), &service.Account{Name: t.Name(), Schedulable: true})
	now := time.Now().UTC().Truncate(time.Microsecond)
	planIDs := make([]int64, 0, 2)
	for range 2 {
		var planID int64
		err := integrationDB.QueryRowContext(ctx, `INSERT INTO scheduled_question_test_plans
			(name, account_ids, question_ids, cron_expression) VALUES ($1, '[1]', '[1]', '0 * * * *') RETURNING id`, t.Name()).Scan(&planID)
		require.NoError(t, err)
		planIDs = append(planIDs, planID)
		t.Cleanup(func() {
			_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduled_question_test_plans WHERE id = $1", planID)
		})
		_, err = integrationDB.ExecContext(ctx, `INSERT INTO scheduled_question_test_account_states
			(plan_id, account_id, state, last_alert_at, auto_paused, auto_paused_at)
			VALUES ($1, $2, 'alert', $3, true, $3)`, planID, account.ID, now)
		require.NoError(t, err)
	}
	for _, value := range []bool{false, true} {
		require.NoError(t, repo.SetSchedulable(ctx, account.ID, value))
		var held int
		require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduled_question_test_account_states WHERE account_id = $1 AND auto_paused = true", account.ID).Scan(&held))
		require.Equal(t, 2, held, "automatic SetSchedulable must leave every plan's ownership intact")
	}
	// Clear uses the production SQL executor and affects every plan, not accounts.
	clearRepo := newAccountRepositoryWithSQL(nil, integrationDB, nil)
	require.NoError(t, clearRepo.ClearScheduledQuestionAutoPause(ctx, account.ID))
	for _, planID := range planIDs {
		var paused bool
		var pausedAt *time.Time
		var state string
		var alertAt time.Time
		err := integrationDB.QueryRowContext(ctx, `SELECT auto_paused, auto_paused_at, state, last_alert_at
			FROM scheduled_question_test_account_states WHERE plan_id = $1 AND account_id = $2`, planID, account.ID).Scan(&paused, &pausedAt, &state, &alertAt)
		require.NoError(t, err)
		require.False(t, paused)
		require.Nil(t, pausedAt)
		require.Equal(t, "alert", state)
		require.True(t, now.Equal(alertAt))
	}
}

type manualPauseObservedBeginner struct {
	*sql.DB
	backendPID chan int
}

func (b manualPauseObservedBeginner) BeginTx(ctx context.Context, options *sql.TxOptions) (*sql.Tx, error) {
	tx, err := b.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	var pid int
	if err := tx.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	b.backendPID <- pid
	return tx, nil
}

func TestAccountRepositoryManualPauseClearWaitsForUncommittedFirstClaim(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, plan := newScheduledQuestionTestIntegrationPlan(t)
	accountID := plan.ID + 980000000
	claim, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = claim.Rollback() })
	_, err = lockScheduledQuestionAutoPauseStates(ctx, claim, accountID)
	require.NoError(t, err)
	_, err = claim.ExecContext(ctx, `INSERT INTO scheduled_question_test_account_states
		(plan_id, account_id, state, auto_paused, auto_paused_at) VALUES ($1, $2, 'alert', true, NOW())`, plan.ID, accountID)
	require.NoError(t, err)

	pids := make(chan int, 1)
	clearRepo := newAccountRepositoryWithSQL(nil, manualPauseObservedBeginner{DB: integrationDB, backendPID: pids}, nil)
	done := make(chan error, 1)
	go func() { done <- clearRepo.ClearScheduledQuestionAutoPause(ctx, accountID) }()
	var pid int
	select {
	case pid = <-pids:
	case <-ctx.Done():
		t.Fatal("manual clear did not begin: ", ctx.Err())
	}
	require.Eventually(t, func() bool {
		var waiting bool
		err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE pid = $1 AND wait_event_type = 'Lock' AND wait_event = 'advisory')`, pid).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond, "manual clear must wait for the first claim's transaction lock")
	require.NoError(t, claim.Commit())
	require.NoError(t, <-done)
	var paused bool
	var pausedAt *time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT auto_paused, auto_paused_at
		FROM scheduled_question_test_account_states WHERE plan_id = $1 AND account_id = $2`, plan.ID, accountID).Scan(&paused, &pausedAt))
	require.False(t, paused, "the first claim must be visible and released after its commit")
	require.Nil(t, pausedAt)
}

func TestAccountRepositoryManualPauseClearAndResumeReversePhysicalRowsDoNotDeadlock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo, first := newScheduledQuestionTestIntegrationPlan(t)
	_, second := newScheduledQuestionTestIntegrationPlan(t)
	accountID := first.ID + 990000000
	for _, planID := range []int64{second.ID, first.ID} {
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO scheduled_question_test_account_states
			(plan_id, account_id, state, auto_paused, auto_paused_at) VALUES ($1, $2, 'alert', true, NOW())`, planID, accountID)
		require.NoError(t, err)
	}
	var insertedFirst int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT plan_id FROM scheduled_question_test_account_states
		WHERE account_id = $1 ORDER BY ctid LIMIT 1`, accountID).Scan(&insertedFirst))
	require.Equal(t, second.ID, insertedFirst, "physical order must oppose the shared plan_id locking order")
	clearRepo := newAccountRepositoryWithSQL(nil, integrationDB, nil)
	for iteration := range 6 {
		_, err := integrationDB.ExecContext(ctx, `UPDATE scheduled_question_test_account_states
			SET auto_paused = true, auto_paused_at = NOW() WHERE account_id = $1`, accountID)
		require.NoError(t, err)
		start := make(chan struct{})
		done := make(chan error, 2)
		go func() {
			<-start
			done <- clearRepo.ClearScheduledQuestionAutoPause(ctx, accountID)
		}()
		go func() {
			<-start
			_, err := repo.ApplyAccountVerdict(ctx, first.ID, accountID, "ok", false, true, time.Now(), func(context.Context, bool) error { return nil })
			done <- err
		}()
		close(start)
		for range 2 {
			require.NoError(t, <-done, fmt.Sprintf("reverse-order resume/clear iteration %d", iteration))
		}
		var remaining int
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduled_question_test_account_states
			WHERE account_id = $1 AND (auto_paused OR auto_paused_at IS NOT NULL)`, accountID).Scan(&remaining))
		require.Zero(t, remaining)
	}
}

func TestAccountRepositoryManualPauseClearLocksAdvisoryAndRowsInOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, first := newScheduledQuestionTestIntegrationPlan(t)
	_, second := newScheduledQuestionTestIntegrationPlan(t)
	accountID := first.ID + 995000000
	for _, planID := range []int64{second.ID, first.ID} {
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO scheduled_question_test_account_states
			(plan_id, account_id, state, auto_paused, auto_paused_at) VALUES ($1, $2, 'alert', true, NOW())`, planID, accountID)
		require.NoError(t, err)
	}
	gate, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = gate.Rollback() }()
	var gatePID, clearPID int
	require.NoError(t, gate.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&gatePID))
	var locked int64
	require.NoError(t, gate.QueryRowContext(ctx, `SELECT plan_id FROM scheduled_question_test_account_states
		WHERE plan_id = $1 AND account_id = $2 FOR UPDATE`, second.ID, accountID).Scan(&locked))
	clearTx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() {
		cancel()
		_ = gate.Rollback()
		_ = clearTx.Rollback()
	}()
	require.NoError(t, clearTx.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&clearPID))
	// The old un-ordered UPDATE would block on the physically first, higher plan.
	_, err = clearTx.ExecContext(ctx, "SET LOCAL enable_indexscan = off; SET LOCAL enable_bitmapscan = off")
	require.NoError(t, err)
	clearRepo := newAccountRepositoryWithSQL(nil, clearTx, nil)
	done := make(chan error, 1)
	go func() { done <- clearRepo.ClearScheduledQuestionAutoPause(ctx, accountID) }()
	require.Eventually(t, func() bool {
		var blocked bool
		queryErr := integrationDB.QueryRowContext(ctx, "SELECT $1::integer = ANY(pg_blocking_pids($2))", gatePID, clearPID).Scan(&blocked)
		return queryErr == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)

	probe, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = probe.Rollback() }()
	var acquired bool
	require.NoError(t, probe.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('scheduled-question-auto-pause:' || $1::text, 0))`, accountID).Scan(&acquired))
	require.False(t, acquired, "manual clear must already hold the shared account advisory lock")
	err = probe.QueryRowContext(ctx, `SELECT plan_id FROM scheduled_question_test_account_states
		WHERE plan_id = $1 AND account_id = $2 FOR UPDATE NOWAIT`, first.ID, accountID).Scan(&locked)
	var postgresError *pq.Error
	require.ErrorAs(t, err, &postgresError)
	require.Equal(t, pq.ErrorCode("55P03"), postgresError.Code, "the lower plan must be locked before waiting for the higher plan")
	require.NoError(t, probe.Rollback())
	require.NoError(t, gate.Commit())
	require.NoError(t, <-done)
	require.NoError(t, clearTx.Commit())
	var remaining int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduled_question_test_account_states
		WHERE account_id = $1 AND (auto_paused OR auto_paused_at IS NOT NULL)`, accountID).Scan(&remaining))
	require.Zero(t, remaining)
}
