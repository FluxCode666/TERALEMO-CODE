//go:build unit

package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/stretchr/testify/require"
)

type scheduledQuestionValidationAccountObserver struct {
	*scheduledQuestionAccountRepoStub
	calls int
}

func (r *scheduledQuestionValidationAccountObserver) GetByID(ctx context.Context, id int64) (*Account, error) {
	r.calls++
	return r.scheduledQuestionAccountRepoStub.GetByID(ctx, id)
}

type scheduledQuestionValidationQuestionObserver struct {
	*scheduledQuestionQuestionRepoStub
	calls int
}

func (r *scheduledQuestionValidationQuestionObserver) GetByID(ctx context.Context, id int64) (*model.AccountTestQuestion, error) {
	r.calls++
	return r.scheduledQuestionQuestionRepoStub.GetByID(ctx, id)
}

type scheduledQuestionInvalidDependency struct {
	name     string
	field    string
	reasons  []string
	breakDep func(*scheduledQuestionAccountRepoStub, *scheduledQuestionQuestionRepoStub)
}

func scheduledQuestionInvalidDependencies() []scheduledQuestionInvalidDependency {
	return []scheduledQuestionInvalidDependency{
		{
			name: "deleted account", field: "account_ids", reasons: []string{"account 2", "not found"},
			breakDep: func(accounts *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
				delete(accounts.accounts, 2)
			},
		},
		{
			name: "deleted question", field: "question_ids", reasons: []string{"test question 2", "not found"},
			breakDep: func(_ *scheduledQuestionAccountRepoStub, questions *scheduledQuestionQuestionRepoStub) {
				delete(questions.questions, 2)
			},
		},
		{
			name: "disabled question", field: "question_ids", reasons: []string{"test question 2", "disabled"},
			breakDep: func(_ *scheduledQuestionAccountRepoStub, questions *scheduledQuestionQuestionRepoStub) {
				questions.questions[2].Enabled = false
			},
		},
		{
			name: "mixed platform accounts", field: "account_ids", reasons: []string{ErrAccountBatchTestMixedPlatforms.Message},
			breakDep: func(accounts *scheduledQuestionAccountRepoStub, _ *scheduledQuestionQuestionRepoStub) {
				accounts.accounts[2].Platform = PlatformOpenAI
			},
		},
	}
}

func requireScheduledQuestionDependencyValidation(t *testing.T, err error, dependency scheduledQuestionInvalidDependency) {
	t.Helper()
	var validation *model.ValidationError
	require.ErrorAs(t, err, &validation, "dependency failures must remain mappable to HTTP 400")
	require.Equal(t, dependency.field, validation.Field)
	for _, reason := range dependency.reasons {
		require.Contains(t, err.Error(), reason)
	}
}

func TestScheduledQuestionValidationInvalidDependenciesAllowMaintenance(t *testing.T) {
	updates := []struct {
		name  string
		input ScheduledQuestionTestPlanInput
	}{
		{name: "disable", input: ScheduledQuestionTestPlanInput{Enabled: scheduledQuestionPtr(false)}},
		{name: "rename", input: ScheduledQuestionTestPlanInput{Name: scheduledQuestionPtr(" renamed ")}},
		{name: "reschedule", input: ScheduledQuestionTestPlanInput{CronExpression: scheduledQuestionPtr(" */10 * * * * ")}},
		{name: "threshold", input: ScheduledQuestionTestPlanInput{MinPassRatio: scheduledQuestionPtr(0)}},
		{name: "cooldown", input: ScheduledQuestionTestPlanInput{CooldownMinutes: scheduledQuestionPtr(0)}},
		{name: "notify off", input: ScheduledQuestionTestPlanInput{NotifyEnabled: scheduledQuestionPtr(false)}},
		{name: "notify on", input: ScheduledQuestionTestPlanInput{NotifyEnabled: scheduledQuestionPtr(true)}},
		{name: "model", input: ScheduledQuestionTestPlanInput{ModelID: scheduledQuestionPtr(" custom-model ")}},
		{name: "empty partial update", input: ScheduledQuestionTestPlanInput{}},
	}
	for _, dependency := range scheduledQuestionInvalidDependencies() {
		for _, update := range updates {
			for _, enabled := range []bool{false, true} {
				label := "disabled plan"
				if enabled {
					label = "enabled plan"
				}
				t.Run(dependency.name+"/"+update.name+"/"+label, func(t *testing.T) {
					svc, repo, accounts, questions := newScheduledQuestionFixture()
					existing := scheduledQuestionValidPlan(1)
					existing.Enabled = enabled
					next := svc.now().Add(time.Hour)
					existing.NextRunAt = &next
					repo.plans[1] = existing
					dependency.breakDep(accounts, questions)
					accountObserver := &scheduledQuestionValidationAccountObserver{scheduledQuestionAccountRepoStub: accounts}
					questionObserver := &scheduledQuestionValidationQuestionObserver{scheduledQuestionQuestionRepoStub: questions}
					svc.accounts, svc.questions = accountObserver, NewAccountTestQuestionService(questionObserver)

					updated, err := svc.Update(context.Background(), 1, update.input)
					require.NoError(t, err)
					require.NotNil(t, updated)
					require.Equal(t, 1, repo.updated)
					require.Zero(t, accountObserver.calls, "maintenance must not depend on account availability")
					require.Zero(t, questionObserver.calls, "maintenance must not depend on question availability")
					require.Equal(t, []int64{2, 1}, updated.AccountIDs)
					require.Equal(t, []int64{2, 1}, updated.QuestionIDs)
					if update.input.Enabled != nil {
						require.False(t, updated.Enabled)
					} else {
						require.Equal(t, enabled, updated.Enabled)
					}
					if update.input.Name != nil {
						require.Equal(t, "renamed", updated.Name)
					}
					if update.input.ModelID != nil {
						require.Equal(t, "custom-model", updated.ModelID)
					}
					if update.input.CronExpression != nil {
						require.Equal(t, "*/10 * * * *", updated.CronExpression)
						require.Equal(t, svc.now().Add(10*time.Minute), *updated.NextRunAt)
					} else {
						require.Equal(t, &next, updated.NextRunAt)
					}
					require.Equal(t, enabled, existing.Enabled, "updates must not mutate the loaded object")
				})
			}
		}
	}
}

func TestScheduledQuestionValidationAlreadyEnabledDoesNotRecheckDependencies(t *testing.T) {
	for _, dependency := range scheduledQuestionInvalidDependencies() {
		t.Run(dependency.name, func(t *testing.T) {
			svc, repo, accounts, questions := newScheduledQuestionFixture()
			repo.plans[1] = scheduledQuestionValidPlan(1)
			dependency.breakDep(accounts, questions)
			accountObserver := &scheduledQuestionValidationAccountObserver{scheduledQuestionAccountRepoStub: accounts}
			questionObserver := &scheduledQuestionValidationQuestionObserver{scheduledQuestionQuestionRepoStub: questions}
			svc.accounts, svc.questions = accountObserver, NewAccountTestQuestionService(questionObserver)

			updated, err := svc.Update(context.Background(), 1, ScheduledQuestionTestPlanInput{Enabled: scheduledQuestionPtr(true)})
			require.NoError(t, err)
			require.True(t, updated.Enabled)
			require.Equal(t, 1, repo.updated)
			require.Zero(t, accountObserver.calls)
			require.Zero(t, questionObserver.calls)
		})
	}
}

func TestScheduledQuestionValidationReenableRechecksDependencies(t *testing.T) {
	for _, dependency := range scheduledQuestionInvalidDependencies() {
		t.Run(dependency.name, func(t *testing.T) {
			svc, repo, accounts, questions := newScheduledQuestionFixture()
			plan := scheduledQuestionValidPlan(1)
			plan.Enabled = false
			repo.plans[1] = plan
			dependency.breakDep(accounts, questions)

			_, err := svc.Update(context.Background(), 1, ScheduledQuestionTestPlanInput{Enabled: scheduledQuestionPtr(true)})
			requireScheduledQuestionDependencyValidation(t, err, dependency)
			require.Zero(t, repo.updated)
			require.False(t, plan.Enabled)
		})
	}
	t.Run("valid dependencies enable successfully", func(t *testing.T) {
		svc, repo, _, _ := newScheduledQuestionFixture()
		plan := scheduledQuestionValidPlan(1)
		plan.Enabled = false
		repo.plans[1] = plan
		updated, err := svc.Update(context.Background(), 1, ScheduledQuestionTestPlanInput{Enabled: scheduledQuestionPtr(true)})
		require.NoError(t, err)
		require.True(t, updated.Enabled)
		require.Equal(t, svc.now().Add(5*time.Minute), *updated.NextRunAt)
		require.Equal(t, 1, repo.updated)
	})
}

func TestScheduledQuestionValidationExplicitIDArraysRecheckAllDependencies(t *testing.T) {
	for _, dependency := range scheduledQuestionInvalidDependencies() {
		for _, field := range []string{"account_ids", "question_ids"} {
			for _, disable := range []bool{false, true} {
				label := "unchanged enabled"
				if disable {
					label = "also disabling"
				}
				t.Run(dependency.name+"/"+field+"/"+label, func(t *testing.T) {
					svc, repo, accounts, questions := newScheduledQuestionFixture()
					repo.plans[1] = scheduledQuestionValidPlan(1)
					dependency.breakDep(accounts, questions)
					input := ScheduledQuestionTestPlanInput{}
					if field == "account_ids" {
						input.AccountIDs = scheduledQuestionPtr([]int64{2, 1})
					} else {
						input.QuestionIDs = scheduledQuestionPtr([]int64{2, 1})
					}
					if disable {
						input.Enabled = scheduledQuestionPtr(false)
					}
					_, err := svc.Update(context.Background(), 1, input)
					requireScheduledQuestionDependencyValidation(t, err, dependency)
					require.Zero(t, repo.updated)
				})
			}
		}
	}
}

func TestScheduledQuestionValidationCreateRequiresDependenciesEvenWhenDisabled(t *testing.T) {
	for _, dependency := range scheduledQuestionInvalidDependencies() {
		for _, enabled := range []bool{false, true} {
			label := "disabled"
			if enabled {
				label = "enabled"
			}
			t.Run(dependency.name+"/"+label, func(t *testing.T) {
				svc, repo, accounts, questions := newScheduledQuestionFixture()
				dependency.breakDep(accounts, questions)
				input := scheduledQuestionValidInput()
				input.Enabled = scheduledQuestionPtr(enabled)
				_, err := svc.Create(context.Background(), input)
				requireScheduledQuestionDependencyValidation(t, err, dependency)
				require.Zero(t, repo.created)
			})
		}
	}
}

func TestScheduledQuestionValidationFieldsAlwaysChecked(t *testing.T) {
	ids := func(n int) []int64 {
		result := make([]int64, n)
		for i := range result {
			result[i] = int64(i + 1)
		}
		return result
	}
	cases := []struct {
		name  string
		field string
		input ScheduledQuestionTestPlanInput
	}{
		{name: "blank name", field: "name", input: ScheduledQuestionTestPlanInput{Name: scheduledQuestionPtr(" ")}},
		{name: "long name", field: "name", input: ScheduledQuestionTestPlanInput{Name: scheduledQuestionPtr(strings.Repeat("a", 101))}},
		{name: "long model", field: "model_id", input: ScheduledQuestionTestPlanInput{ModelID: scheduledQuestionPtr(strings.Repeat("m", 101))}},
		{name: "bad cron", field: "cron_expression", input: ScheduledQuestionTestPlanInput{CronExpression: scheduledQuestionPtr("61 * * * *")}},
		{name: "empty cron", field: "cron_expression", input: ScheduledQuestionTestPlanInput{CronExpression: scheduledQuestionPtr("")}},
		{name: "seconds cron", field: "cron_expression", input: ScheduledQuestionTestPlanInput{CronExpression: scheduledQuestionPtr("0 */5 * * * *")}},
		{name: "negative threshold", field: "min_pass_ratio", input: ScheduledQuestionTestPlanInput{MinPassRatio: scheduledQuestionPtr(-1)}},
		{name: "excess threshold", field: "min_pass_ratio", input: ScheduledQuestionTestPlanInput{MinPassRatio: scheduledQuestionPtr(101)}},
		{name: "negative cooldown", field: "cooldown_minutes", input: ScheduledQuestionTestPlanInput{CooldownMinutes: scheduledQuestionPtr(-1)}},
		{name: "excess cooldown", field: "cooldown_minutes", input: ScheduledQuestionTestPlanInput{CooldownMinutes: scheduledQuestionPtr(10081)}},
		{name: "empty accounts", field: "account_ids", input: ScheduledQuestionTestPlanInput{AccountIDs: scheduledQuestionPtr([]int64{})}},
		{name: "nonpositive accounts", field: "account_ids", input: ScheduledQuestionTestPlanInput{AccountIDs: scheduledQuestionPtr([]int64{0})}},
		{name: "too many accounts", field: "account_ids", input: ScheduledQuestionTestPlanInput{AccountIDs: scheduledQuestionPtr(ids(201))}},
		{name: "empty questions", field: "question_ids", input: ScheduledQuestionTestPlanInput{QuestionIDs: scheduledQuestionPtr([]int64{})}},
		{name: "nonpositive questions", field: "question_ids", input: ScheduledQuestionTestPlanInput{QuestionIDs: scheduledQuestionPtr([]int64{-1})}},
		{name: "too many questions", field: "question_ids", input: ScheduledQuestionTestPlanInput{QuestionIDs: scheduledQuestionPtr(ids(21))}},
	}
	for _, tc := range cases {
		for _, invalidDependency := range []bool{false, true} {
			label := "valid dependencies"
			if invalidDependency {
				label = "invalid dependencies"
			}
			t.Run(tc.name+"/"+label, func(t *testing.T) {
				svc, repo, accounts, questions := newScheduledQuestionFixture()
				plan := scheduledQuestionValidPlan(1)
				repo.plans[1] = plan
				if invalidDependency {
					delete(accounts.accounts, 2)
					questions.questions[2].Enabled = false
				}
				input := tc.input
				input.Enabled = scheduledQuestionPtr(false)
				_, err := svc.Update(context.Background(), 1, input)
				var validation *model.ValidationError
				require.ErrorAs(t, err, &validation)
				require.Equal(t, tc.field, validation.Field)
				require.Zero(t, repo.updated)
				require.True(t, plan.Enabled, "failed disable-and-edit must not mutate loaded state")
			})
		}
	}
	t.Run("invalid stored fields reject unrelated updates", func(t *testing.T) {
		svc, repo, accounts, _ := newScheduledQuestionFixture()
		plan := scheduledQuestionValidPlan(1)
		plan.Name = " "
		repo.plans[1] = plan
		delete(accounts.accounts, 2)
		_, err := svc.Update(context.Background(), 1, ScheduledQuestionTestPlanInput{NotifyEnabled: scheduledQuestionPtr(false)})
		var validation *model.ValidationError
		require.ErrorAs(t, err, &validation)
		require.Equal(t, "name", validation.Field)
		require.Zero(t, repo.updated)
	})
}

func TestScheduledQuestionValidationDeleteIgnoresInvalidDependencies(t *testing.T) {
	for _, dependency := range scheduledQuestionInvalidDependencies() {
		t.Run(dependency.name, func(t *testing.T) {
			svc, repo, accounts, questions := newScheduledQuestionFixture()
			repo.plans[1] = scheduledQuestionValidPlan(1)
			dependency.breakDep(accounts, questions)
			accountObserver := &scheduledQuestionValidationAccountObserver{scheduledQuestionAccountRepoStub: accounts}
			questionObserver := &scheduledQuestionValidationQuestionObserver{scheduledQuestionQuestionRepoStub: questions}
			svc.accounts, svc.questions = accountObserver, NewAccountTestQuestionService(questionObserver)
			require.NoError(t, svc.Delete(context.Background(), 1))
			require.Equal(t, int64(1), repo.deleted)
			require.Zero(t, accountObserver.calls)
			require.Zero(t, questionObserver.calls)
		})
	}
}
