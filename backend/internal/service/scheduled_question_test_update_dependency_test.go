//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestScheduledQuestionUpdateUnchangedDependenciesAllowCronOnlySave is the
// regression test for the reported bug: the admin UI resends the full
// account_ids / question_ids arrays on every save (possibly reordered and with
// duplicates). That must not count as an edit, so a cron-only change has to
// succeed even when a referenced account was deleted, became unavailable, ended
// up on a different platform, or a question was disabled afterwards.
func TestScheduledQuestionUpdateUnchangedDependenciesAllowCronOnlySave(t *testing.T) {
	cases := []struct {
		name     string
		breakDep func(*scheduledQuestionAccountRepoStub, *scheduledQuestionQuestionRepoStub)
	}{
		{
			name: "deleted account",
			breakDep: func(accounts *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
				delete(accounts.accounts, 2)
			},
		},
		{
			name: "account lookup unavailable",
			breakDep: func(accounts *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
				accounts.err = errors.New("account lookup failed")
			},
		},
		{
			name: "account on another platform",
			breakDep: func(accounts *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
				accounts.accounts[2].Platform = PlatformOpenAI
			},
		},
		{
			name: "disabled question",
			breakDep: func(_ *scheduledQuestionAccountRepoStub, questions *scheduledQuestionQuestionRepoStub) {
				questions.questions[2].Enabled = false
			},
		},
		{
			name: "deleted question",
			breakDep: func(_ *scheduledQuestionAccountRepoStub, questions *scheduledQuestionQuestionRepoStub) {
				delete(questions.questions, 2)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, accounts, questions := newScheduledQuestionFixture()
			repo.plans[1] = scheduledQuestionValidPlan(1) // accounts/questions {2,1}
			tc.breakDep(accounts, questions)
			accountObserver := &scheduledQuestionValidationAccountObserver{scheduledQuestionAccountRepoStub: accounts}
			questionObserver := &scheduledQuestionValidationQuestionObserver{scheduledQuestionQuestionRepoStub: questions}
			svc.accounts, svc.questions = accountObserver, NewAccountTestQuestionService(questionObserver)

			// Same sets, shuffled and with duplicates, exactly as the panel sends them.
			updated, err := svc.Update(context.Background(), 1, ScheduledQuestionTestPlanInput{
				AccountIDs:     scheduledQuestionPtr([]int64{1, 2, 1}),
				QuestionIDs:    scheduledQuestionPtr([]int64{2, 2, 1}),
				CronExpression: scheduledQuestionPtr("*/10 * * * *"),
			})
			require.NoError(t, err)
			require.NotNil(t, updated)
			require.Equal(t, "*/10 * * * *", updated.CronExpression)
			require.Equal(t, svc.now().Add(10*time.Minute), *updated.NextRunAt)
			require.Equal(t, []int64{1, 2}, updated.AccountIDs)
			require.Equal(t, []int64{2, 1}, updated.QuestionIDs)
			require.Equal(t, 1, repo.updated)
			require.Zero(t, accountObserver.calls, "unchanged account ids must not be revalidated")
			require.Zero(t, questionObserver.calls, "unchanged question ids must not be revalidated")
		})
	}
}

// TestScheduledQuestionUpdateChangedAccountSetStillValidates keeps the check in
// place for real edits of account_ids.
func TestScheduledQuestionUpdateChangedAccountSetStillValidates(t *testing.T) {
	t.Run("added account does not exist", func(t *testing.T) {
		svc, repo, _, _ := newScheduledQuestionFixture()
		repo.plans[1] = scheduledQuestionValidPlan(1)
		_, err := svc.Update(context.Background(), 1, ScheduledQuestionTestPlanInput{
			AccountIDs: scheduledQuestionPtr([]int64{2, 1, 999}),
		})
		requireScheduledQuestionDependencyValidation(t, err, scheduledQuestionInvalidDependency{field: "account_ids", reasons: []string{"account 999"}})
		require.Zero(t, repo.updated)
	})

	t.Run("changed to mixed platforms", func(t *testing.T) {
		svc, repo, accounts, _ := newScheduledQuestionFixture()
		repo.plans[1] = scheduledQuestionValidPlan(1)
		accounts.accounts[3].Platform = PlatformOpenAI
		_, err := svc.Update(context.Background(), 1, ScheduledQuestionTestPlanInput{
			AccountIDs: scheduledQuestionPtr([]int64{2, 1, 3}),
		})
		requireScheduledQuestionDependencyValidation(t, err, scheduledQuestionInvalidDependency{
			field: "account_ids", reasons: []string{ErrAccountBatchTestMixedPlatforms.Message},
		})
		require.Zero(t, repo.updated)
	})

	t.Run("removed account shrinks the set", func(t *testing.T) {
		svc, repo, accounts, _ := newScheduledQuestionFixture()
		repo.plans[1] = scheduledQuestionValidPlan(1)
		delete(accounts.accounts, 1)
		// Only 2 is left, so the removed account cannot break the check by itself;
		// deleting the *remaining* one must.
		delete(accounts.accounts, 2)
		updated, err := svc.Update(context.Background(), 1, ScheduledQuestionTestPlanInput{
			AccountIDs: scheduledQuestionPtr([]int64{2}),
		})
		require.Error(t, err)
		require.Nil(t, updated)
		require.Zero(t, repo.updated)
	})
}

// TestScheduledQuestionUpdateChangedQuestionSetStillValidates mirrors the account
// case for question_ids.
func TestScheduledQuestionUpdateChangedQuestionSetStillValidates(t *testing.T) {
	t.Run("added question is disabled", func(t *testing.T) {
		svc, repo, _, questions := newScheduledQuestionFixture()
		repo.plans[1] = scheduledQuestionValidPlan(1)
		questions.questions[3].Enabled = false
		_, err := svc.Update(context.Background(), 1, ScheduledQuestionTestPlanInput{
			QuestionIDs: scheduledQuestionPtr([]int64{2, 1, 3}),
		})
		requireScheduledQuestionDependencyValidation(t, err, scheduledQuestionInvalidDependency{
			field: "question_ids", reasons: []string{"test question 3", "disabled"},
		})
		require.Zero(t, repo.updated)
	})

	t.Run("added question does not exist", func(t *testing.T) {
		svc, repo, _, _ := newScheduledQuestionFixture()
		repo.plans[1] = scheduledQuestionValidPlan(1)
		_, err := svc.Update(context.Background(), 1, ScheduledQuestionTestPlanInput{
			QuestionIDs: scheduledQuestionPtr([]int64{2, 1, 999}),
		})
		requireScheduledQuestionDependencyValidation(t, err, scheduledQuestionInvalidDependency{
			field: "question_ids", reasons: []string{"test question 999", "not found"},
		})
		require.Zero(t, repo.updated)
	})
}

// TestScheduledQuestionUpdateSkipsOnlyTheUnchangedDependencySet documents the
// per-field split: editing one set never revalidates the other one.
func TestScheduledQuestionUpdateSkipsOnlyTheUnchangedDependencySet(t *testing.T) {
	t.Run("account edit does not recheck questions", func(t *testing.T) {
		svc, repo, _, questions := newScheduledQuestionFixture()
		repo.plans[1] = scheduledQuestionValidPlan(1)
		questions.questions[2].Enabled = false // stored question set is stale
		updated, err := svc.Update(context.Background(), 1, ScheduledQuestionTestPlanInput{
			AccountIDs: scheduledQuestionPtr([]int64{2, 1, 3}),
		})
		require.NoError(t, err)
		require.Equal(t, []int64{2, 1}, updated.QuestionIDs)
		require.Equal(t, 1, repo.updated)
	})

	t.Run("question edit does not recheck accounts", func(t *testing.T) {
		svc, repo, accounts, _ := newScheduledQuestionFixture()
		repo.plans[1] = scheduledQuestionValidPlan(1)
		delete(accounts.accounts, 2) // stored account set is stale
		updated, err := svc.Update(context.Background(), 1, ScheduledQuestionTestPlanInput{
			QuestionIDs: scheduledQuestionPtr([]int64{2, 1, 3}),
		})
		require.NoError(t, err)
		require.Equal(t, []int64{2, 1}, updated.AccountIDs)
		require.Equal(t, 1, repo.updated)
	})
}

// TestScheduledQuestionUpdateReenableRechecksEverything keeps the existing rule:
// going from disabled to enabled validates both sets even when they did not
// change.
func TestScheduledQuestionUpdateReenableRechecksEverything(t *testing.T) {
	newDisabled := func(t *testing.T) (*ScheduledQuestionTestService, *scheduledQuestionRepoStub, *scheduledQuestionAccountRepoStub, *scheduledQuestionQuestionRepoStub) {
		t.Helper()
		svc, repo, accounts, questions := newScheduledQuestionFixture()
		plan := scheduledQuestionValidPlan(1)
		plan.Enabled = false
		repo.plans[1] = plan
		return svc, repo, accounts, questions
	}

	t.Run("stale account blocks enabling", func(t *testing.T) {
		svc, repo, accounts, _ := newDisabled(t)
		delete(accounts.accounts, 2)
		_, err := svc.Update(context.Background(), 1, ScheduledQuestionTestPlanInput{
			Enabled:     scheduledQuestionPtr(true),
			AccountIDs:  scheduledQuestionPtr([]int64{2, 1}),
			QuestionIDs: scheduledQuestionPtr([]int64{2, 1}),
		})
		requireScheduledQuestionDependencyValidation(t, err, scheduledQuestionInvalidDependency{field: "account_ids", reasons: []string{"account 2"}})
		require.Zero(t, repo.updated)
	})

	t.Run("stale question blocks enabling", func(t *testing.T) {
		svc, repo, _, questions := newDisabled(t)
		questions.questions[2].Enabled = false
		_, err := svc.Update(context.Background(), 1, ScheduledQuestionTestPlanInput{
			Enabled: scheduledQuestionPtr(true),
		})
		requireScheduledQuestionDependencyValidation(t, err, scheduledQuestionInvalidDependency{
			field: "question_ids", reasons: []string{"test question 2", "disabled"},
		})
		require.Zero(t, repo.updated)
	})

	t.Run("valid dependencies enable", func(t *testing.T) {
		svc, repo, _, _ := newDisabled(t)
		updated, err := svc.Update(context.Background(), 1, ScheduledQuestionTestPlanInput{Enabled: scheduledQuestionPtr(true)})
		require.NoError(t, err)
		require.True(t, updated.Enabled)
		require.Equal(t, 1, repo.updated)
	})
}

// TestScheduledQuestionCreateAlwaysValidatesDependencies pins Create's unchanged
// behaviour: it validates both dependency sets regardless of the submitted shape.
func TestScheduledQuestionCreateAlwaysValidatesDependencies(t *testing.T) {
	for _, dependency := range scheduledQuestionInvalidDependencies() {
		t.Run(dependency.name, func(t *testing.T) {
			svc, repo, accounts, questions := newScheduledQuestionFixture()
			dependency.breakDep(accounts, questions)
			_, err := svc.Create(context.Background(), scheduledQuestionValidInput())
			requireScheduledQuestionDependencyValidation(t, err, dependency)
			require.Zero(t, repo.created)
		})
	}

	t.Run("valid input creates", func(t *testing.T) {
		svc, repo, _, _ := newScheduledQuestionFixture()
		plan, err := svc.Create(context.Background(), scheduledQuestionValidInput())
		require.NoError(t, err)
		require.Equal(t, []int64{2, 1}, plan.AccountIDs)
		require.Equal(t, 1, repo.created)
	})
}

// TestScheduledQuestionIDSetsEqual covers the comparison helper directly.
func TestScheduledQuestionIDSetsEqual(t *testing.T) {
	cases := []struct {
		name  string
		left  []int64
		right []int64
		want  bool
	}{
		{name: "identical", left: []int64{1, 2}, right: []int64{1, 2}, want: true},
		{name: "reordered", left: []int64{1, 2}, right: []int64{2, 1}, want: true},
		{name: "duplicates", left: []int64{2, 1, 2}, right: []int64{2, 1}, want: true},
		{name: "different member", left: []int64{1, 2}, right: []int64{1, 3}, want: false},
		{name: "subset", left: []int64{1}, right: []int64{1, 2}, want: false},
		{name: "empty both", left: nil, right: []int64{}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, scheduledQuestionIDSetsEqual(tc.left, tc.right))
			require.Equal(t, tc.want, scheduledQuestionIDSetsEqual(tc.right, tc.left))
		})
	}
}
