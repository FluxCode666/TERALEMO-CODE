//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type scheduledQuestionDeleteRepo struct {
	*scheduledQuestionRepoStub
	deleteCalls   int
	deleteStarted chan struct{}
	deleteRelease <-chan struct{}
}

func (r *scheduledQuestionDeleteRepo) Delete(ctx context.Context, id int64) error {
	r.mu.Lock()
	r.deleteCalls++
	r.mu.Unlock()
	if r.deleteStarted != nil {
		r.deleteStarted <- struct{}{}
	}
	if r.deleteRelease != nil {
		select {
		case <-r.deleteRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = id
	delete(r.plans, id)
	remaining := r.runs[:0]
	for _, run := range r.runs {
		if run.PlanID != id {
			remaining = append(remaining, run)
		}
	}
	r.runs = remaining
	return nil
}

func scheduledQuestionDeleteWait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for controlled scheduling operation")
	}
}

func TestScheduledQuestionDeleteRejectsRunningRoundsAndSucceedsAfterCompletion(t *testing.T) {
	for _, mode := range []string{"manual", "scheduled"} {
		t.Run(mode, func(t *testing.T) {
			svc, base, _, _ := newScheduledQuestionFixture()
			plan := scheduledQuestionValidPlan(1)
			base.plans[plan.ID] = plan
			history := &ScheduledQuestionTestRun{ID: 81, PlanID: plan.ID, Status: "success"}
			base.runs = []*ScheduledQuestionTestRun{history}
			repo := &scheduledQuestionDeleteRepo{scheduledQuestionRepoStub: base}
			svc.repo = repo
			started, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(func() {
				unblock()
				svc.Stop()
			})
			svc.batch = &scheduledQuestionBatchStub{run: func(ctx context.Context, _ *AccountBatchTestPlan, _ func(AccountBatchTestEvent) error) error {
				close(started)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}}
			var scheduledDone chan error
			if mode == "manual" {
				accepted, err := svc.RunNow(context.Background(), plan.ID)
				require.NoError(t, err)
				require.True(t, accepted.Accepted)
			} else {
				scheduledDone = make(chan error, 1)
				go func() {
					_, err := svc.runScheduledPlan(context.Background(), plan.ID)
					scheduledDone <- err
				}()
			}
			scheduledQuestionDeleteWait(t, started)
			err := svc.Delete(context.Background(), plan.ID)
			require.ErrorIs(t, err, ErrScheduledQuestionTestRunning)
			require.Equal(t, 409, infraerrors.Code(err))
			repo.mu.Lock()
			calls := repo.deleteCalls
			repo.mu.Unlock()
			require.Zero(t, calls, "reject before invoking repository deletion or cascading history")
			current, err := svc.GetByID(context.Background(), plan.ID)
			require.NoError(t, err)
			require.True(t, current.Running)
			histories, err := svc.ListRuns(context.Background(), plan.ID, 10)
			require.NoError(t, err)
			require.Equal(t, []*ScheduledQuestionTestRun{history}, histories)
			unblock()
			joined := make(chan struct{})
			go func() {
				svc.active.Wait()
				close(joined)
			}()
			scheduledQuestionDeleteWait(t, joined)
			if scheduledDone != nil {
				require.NoError(t, <-scheduledDone)
			}
			require.NoError(t, svc.Delete(context.Background(), plan.ID), "round completion must release the deletion gate")
			require.Equal(t, 1, repo.deleteCalls)
			require.Equal(t, plan.ID, repo.deleted)
			_, err = svc.GetByID(context.Background(), plan.ID)
			require.ErrorIs(t, err, ErrScheduledQuestionTestNotFound)
			require.Empty(t, repo.runs, "successful deletion reaches the simulated history cascade")
		})
	}
}

func TestScheduledQuestionDeleteInProgressRejectsRunNow(t *testing.T) {
	svc, base, _, _ := newScheduledQuestionFixture()
	plan := scheduledQuestionValidPlan(1)
	base.plans[plan.ID] = plan
	started, release := make(chan struct{}, 1), make(chan struct{})
	repo := &scheduledQuestionDeleteRepo{scheduledQuestionRepoStub: base, deleteStarted: started, deleteRelease: release}
	svc.repo = repo
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() {
		unblock()
		svc.Stop()
	})
	done := make(chan error, 1)
	go func() { done <- svc.Delete(context.Background(), plan.ID) }()
	scheduledQuestionDeleteWait(t, started)
	accepted, err := svc.RunNow(context.Background(), plan.ID)
	require.Nil(t, accepted)
	require.ErrorIs(t, err, ErrScheduledQuestionTestRunning)
	require.Equal(t, 409, infraerrors.Code(err), "deletion must own the same execution gate as manual and scheduled rounds")
	current, err := svc.GetByID(context.Background(), plan.ID)
	require.NoError(t, err)
	require.True(t, current.Running)
	unblock()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("deletion did not finish after releasing repository barrier")
	}
	require.Equal(t, 1, repo.deleteCalls)
	_, err = svc.GetByID(context.Background(), plan.ID)
	require.ErrorIs(t, err, ErrScheduledQuestionTestNotFound)
}

func TestScheduledQuestionDeleteAfterStopReturnsStoppedWithoutRepositoryCall(t *testing.T) {
	svc, base, _, _ := newScheduledQuestionFixture()
	plan := scheduledQuestionValidPlan(1)
	base.plans[plan.ID] = plan
	history := &ScheduledQuestionTestRun{ID: 81, PlanID: plan.ID, Status: "success"}
	base.runs = []*ScheduledQuestionTestRun{history}
	repo := &scheduledQuestionDeleteRepo{scheduledQuestionRepoStub: base}
	svc.repo = repo
	svc.Stop()
	err := svc.Delete(context.Background(), plan.ID)
	require.ErrorIs(t, err, ErrScheduledQuestionTestStopped)
	require.Equal(t, 503, infraerrors.Code(err))
	require.Zero(t, repo.deleteCalls)
	require.Equal(t, plan, repo.plans[plan.ID])
	require.Equal(t, []*ScheduledQuestionTestRun{history}, repo.runs)
}
