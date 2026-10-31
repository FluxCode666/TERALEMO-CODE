//go:build unit

package service

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/stretchr/testify/require"
)

type accountTestQuestionRepoStub struct {
	question    *model.AccountTestQuestion
	list        []*model.AccountTestQuestion
	enabledOnly bool
	err         error
	writes      int
	deletedID   int64
	updateMask  AccountTestQuestionUpdateMask
}

func (r *accountTestQuestionRepoStub) List(_ context.Context, enabledOnly bool) ([]*model.AccountTestQuestion, error) {
	r.enabledOnly = enabledOnly
	return r.list, r.err
}

func (r *accountTestQuestionRepoStub) GetByID(_ context.Context, _ int64) (*model.AccountTestQuestion, error) {
	return r.question, r.err
}

func (r *accountTestQuestionRepoStub) Create(_ context.Context, question *model.AccountTestQuestion) (*model.AccountTestQuestion, error) {
	r.writes++
	r.question = question
	return question, r.err
}

func (r *accountTestQuestionRepoStub) Update(_ context.Context, question *model.AccountTestQuestion, fields AccountTestQuestionUpdateMask) (*model.AccountTestQuestion, error) {
	r.updateMask = fields
	r.writes++
	r.question = question
	return question, r.err
}

func (r *accountTestQuestionRepoStub) Delete(_ context.Context, id int64) error {
	r.deletedID = id
	return r.err
}

func TestAccountTestQuestionServiceCreate(t *testing.T) {
	repo := &accountTestQuestionRepoStub{}
	svc := NewAccountTestQuestionService(repo)
	question, err := svc.Create(context.Background(), CreateAccountTestQuestionInput{
		Name: " name ", Content: " content ", AnswerPatterns: []string{" first ", "", "second", "first"}, SortOrder: 9,
	})
	require.NoError(t, err)
	require.Equal(t, "name", question.Name)
	require.Equal(t, "content", question.Content)
	require.Equal(t, []string{"first", "second"}, question.AnswerPatterns)
	require.True(t, question.Enabled)
	require.Equal(t, 9, question.SortOrder)
	require.Equal(t, 1, repo.writes)

	disabled := false
	question, err = svc.Create(context.Background(), CreateAccountTestQuestionInput{Name: "x", Content: "x", Enabled: &disabled})
	require.NoError(t, err)
	require.False(t, question.Enabled)
	require.Equal(t, []string{}, question.AnswerPatterns)
}

func TestAccountTestQuestionServicePartialUpdate(t *testing.T) {
	ctx := context.Background()
	existing := &model.AccountTestQuestion{ID: 7, Name: "old", Content: "content", AnswerPatterns: []string{"pattern"}, Enabled: true, SortOrder: 8}
	repo := &accountTestQuestionRepoStub{question: existing}
	svc := NewAccountTestQuestionService(repo)
	updated, err := svc.Update(ctx, 7, UpdateAccountTestQuestionInput{})
	require.NoError(t, err)
	require.Equal(t, existing, updated)
	require.Equal(t, AccountTestQuestionUpdateMask{}, repo.updateMask)

	name, content, enabled, sortOrder, patterns := " new ", " new content ", false, 0, []string{}
	updated, err = svc.Update(ctx, 7, UpdateAccountTestQuestionInput{
		Name: &name, Content: &content, Enabled: &enabled, SortOrder: &sortOrder, AnswerPatterns: &patterns,
	})
	require.NoError(t, err)
	require.Equal(t, int64(7), updated.ID)
	require.Equal(t, "new", updated.Name)
	require.Equal(t, "new content", updated.Content)
	require.False(t, updated.Enabled)
	require.Zero(t, updated.SortOrder)
	require.Equal(t, []string{}, updated.AnswerPatterns)
	require.Equal(t, AccountTestQuestionUpdateMask{
		Name: true, Content: true, AnswerPatterns: true, Enabled: true, SortOrder: true,
	}, repo.updateMask)
	require.Equal(t, "old", existing.Name, "partial update must not mutate the loaded object")
}

func TestAccountTestQuestionServiceUpdateMask(t *testing.T) {
	name, content, enabled, sortOrder, patterns := " new ", " new content ", false, 0, []string{" (?i)new ", "", "(?i)new"}
	for _, tt := range []struct {
		name  string
		input UpdateAccountTestQuestionInput
		mask  AccountTestQuestionUpdateMask
		want  model.AccountTestQuestion
	}{
		{name: "name", input: UpdateAccountTestQuestionInput{Name: &name}, mask: AccountTestQuestionUpdateMask{Name: true},
			want: model.AccountTestQuestion{ID: 7, Name: "new", Content: "content", AnswerPatterns: []string{"old"}, Enabled: true, SortOrder: 8}},
		{name: "content", input: UpdateAccountTestQuestionInput{Content: &content}, mask: AccountTestQuestionUpdateMask{Content: true},
			want: model.AccountTestQuestion{ID: 7, Name: "old", Content: "new content", AnswerPatterns: []string{"old"}, Enabled: true, SortOrder: 8}},
		{name: "patterns", input: UpdateAccountTestQuestionInput{AnswerPatterns: &patterns}, mask: AccountTestQuestionUpdateMask{AnswerPatterns: true},
			want: model.AccountTestQuestion{ID: 7, Name: "old", Content: "content", AnswerPatterns: []string{"(?i)new"}, Enabled: true, SortOrder: 8}},
		{name: "enabled false", input: UpdateAccountTestQuestionInput{Enabled: &enabled}, mask: AccountTestQuestionUpdateMask{Enabled: true},
			want: model.AccountTestQuestion{ID: 7, Name: "old", Content: "content", AnswerPatterns: []string{"old"}, Enabled: false, SortOrder: 8}},
		{name: "sort order zero", input: UpdateAccountTestQuestionInput{SortOrder: &sortOrder}, mask: AccountTestQuestionUpdateMask{SortOrder: true},
			want: model.AccountTestQuestion{ID: 7, Name: "old", Content: "content", AnswerPatterns: []string{"old"}, Enabled: true, SortOrder: 0}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &accountTestQuestionRepoStub{question: &model.AccountTestQuestion{
				ID: 7, Name: "old", Content: "content", AnswerPatterns: []string{"old"}, Enabled: true, SortOrder: 8,
			}}
			updated, err := NewAccountTestQuestionService(repo).Update(context.Background(), 7, tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.want, *updated)
			require.Equal(t, tt.mask, repo.updateMask)
			require.Equal(t, 1, repo.writes)
		})
	}
}

func TestAccountTestQuestionServiceRejectsInvalidSortOrderBeforeWriting(t *testing.T) {
	for _, sortOrder := range []int{math.MinInt32 - 1, math.MaxInt32 + 1, -3000000000, 3000000000} {
		repo := &accountTestQuestionRepoStub{question: &model.AccountTestQuestion{ID: 7, Name: "name", Content: "content", SortOrder: 8}}
		svc := NewAccountTestQuestionService(repo)
		_, err := svc.Create(context.Background(), CreateAccountTestQuestionInput{Name: "name", Content: "content", SortOrder: sortOrder})
		var validationError *model.ValidationError
		require.ErrorAs(t, err, &validationError)
		require.Equal(t, "sort_order", validationError.Field)
		_, err = svc.Update(context.Background(), 7, UpdateAccountTestQuestionInput{SortOrder: &sortOrder})
		require.ErrorAs(t, err, &validationError)
		require.Equal(t, "sort_order", validationError.Field)
		require.Zero(t, repo.writes)
		require.Equal(t, 8, repo.question.SortOrder)
	}
}

func TestAccountTestQuestionServiceValidationAndErrors(t *testing.T) {
	ctx := context.Background()
	repo := &accountTestQuestionRepoStub{question: &model.AccountTestQuestion{ID: 7, Name: "name", Content: "content"}}
	svc := NewAccountTestQuestionService(repo)
	_, err := svc.Create(ctx, CreateAccountTestQuestionInput{Name: " ", Content: "content"})
	var validationError *model.ValidationError
	require.ErrorAs(t, err, &validationError)
	empty := " "
	_, err = svc.Update(ctx, 7, UpdateAccountTestQuestionInput{Content: &empty})
	require.ErrorAs(t, err, &validationError)
	require.Zero(t, repo.writes)
	require.Equal(t, "content", repo.question.Content)

	repo.question = nil
	question, err := svc.GetByID(ctx, 99)
	require.NoError(t, err)
	require.Nil(t, question)
	_, err = svc.Update(ctx, 99, UpdateAccountTestQuestionInput{})
	require.ErrorIs(t, err, ErrAccountTestQuestionNotFound)

	repo.err = errors.New("database unavailable")
	_, err = svc.List(ctx, true)
	require.ErrorIs(t, err, repo.err)
	require.True(t, repo.enabledOnly)
	_, err = svc.GetByID(ctx, 1)
	require.ErrorIs(t, err, repo.err)
	_, err = svc.Update(ctx, 1, UpdateAccountTestQuestionInput{})
	require.ErrorIs(t, err, repo.err)
	_, err = svc.Create(ctx, CreateAccountTestQuestionInput{Name: "name", Content: "content"})
	require.ErrorIs(t, err, repo.err)
	require.ErrorIs(t, svc.Delete(ctx, 7), repo.err)
	require.Equal(t, int64(7), repo.deletedID)

	repo.err = nil
	repo.list = []*model.AccountTestQuestion{{ID: 7}}
	listed, err := svc.List(ctx, false)
	require.NoError(t, err)
	require.Equal(t, repo.list, listed)
	require.False(t, repo.enabledOnly)
	require.NoError(t, svc.Delete(ctx, 7))
}

func TestAccountTestQuestionServiceRejectsInvalidPatternsBeforeWriting(t *testing.T) {
	repo := &accountTestQuestionRepoStub{question: &model.AccountTestQuestion{
		ID: 7, Name: "name", Content: "content", AnswerPatterns: []string{"(?i)claude"},
	}}
	svc := NewAccountTestQuestionService(repo)
	invalidPatterns := []string{"", "["}
	_, err := svc.Create(context.Background(), CreateAccountTestQuestionInput{
		Name: "name", Content: "content", AnswerPatterns: invalidPatterns,
	})
	var validationError *model.ValidationError
	require.ErrorAs(t, err, &validationError)
	require.Contains(t, err.Error(), `pattern 2 "["`)
	_, err = svc.Update(context.Background(), 7, UpdateAccountTestQuestionInput{AnswerPatterns: &invalidPatterns})
	require.ErrorAs(t, err, &validationError)
	require.Zero(t, repo.writes)
	require.Equal(t, []string{"(?i)claude"}, repo.question.AnswerPatterns)
}
