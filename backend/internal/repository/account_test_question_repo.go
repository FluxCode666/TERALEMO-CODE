package repository

import (
	"context"

	"entgo.io/ent/dialect/sql/sqlgraph"
	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/accounttestquestion"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type accountTestQuestionRepository struct {
	client *ent.Client
}

func NewAccountTestQuestionRepository(client *ent.Client) service.AccountTestQuestionRepository {
	return &accountTestQuestionRepository{client: client}
}

func (r *accountTestQuestionRepository) List(ctx context.Context, enabledOnly bool) ([]*model.AccountTestQuestion, error) {
	query := r.client.AccountTestQuestion.Query().
		Order(ent.Asc(accounttestquestion.FieldSortOrder), ent.Asc(accounttestquestion.FieldID))
	if enabledOnly {
		query.Where(accounttestquestion.EnabledEQ(true))
	}
	questions, err := query.All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*model.AccountTestQuestion, len(questions))
	for i, question := range questions {
		result[i] = accountTestQuestionToModel(question)
	}
	return result, nil
}

func (r *accountTestQuestionRepository) GetByID(ctx context.Context, id int64) (*model.AccountTestQuestion, error) {
	question, err := r.client.AccountTestQuestion.Get(ctx, id)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return accountTestQuestionToModel(question), nil
}

func (r *accountTestQuestionRepository) Create(ctx context.Context, question *model.AccountTestQuestion) (*model.AccountTestQuestion, error) {
	created, err := r.client.AccountTestQuestion.Create().
		SetName(question.Name).
		SetContent(question.Content).
		SetAnswerPatterns(question.AnswerPatterns).
		SetEnabled(question.Enabled).
		SetSortOrder(question.SortOrder).
		Save(ctx)
	if err != nil {
		return nil, accountTestQuestionRepositoryError(err)
	}
	return accountTestQuestionToModel(created), nil
}

func (r *accountTestQuestionRepository) Update(ctx context.Context, question *model.AccountTestQuestion, fields service.AccountTestQuestionUpdateMask) (*model.AccountTestQuestion, error) {
	update := r.client.AccountTestQuestion.UpdateOneID(question.ID)
	if fields.Name {
		update.SetName(question.Name)
	}
	if fields.Content {
		update.SetContent(question.Content)
	}
	if fields.AnswerPatterns {
		update.SetAnswerPatterns(question.AnswerPatterns)
	}
	if fields.Enabled {
		update.SetEnabled(question.Enabled)
	}
	if fields.SortOrder {
		update.SetSortOrder(question.SortOrder)
	}
	updated, err := update.Save(ctx)
	if err != nil {
		return nil, accountTestQuestionRepositoryError(err)
	}
	return accountTestQuestionToModel(updated), nil
}

func (r *accountTestQuestionRepository) Delete(ctx context.Context, id int64) error {
	return accountTestQuestionRepositoryError(r.client.AccountTestQuestion.DeleteOneID(id).Exec(ctx))
}

func accountTestQuestionRepositoryError(err error) error {
	if ent.IsNotFound(err) {
		return service.ErrAccountTestQuestionNotFound
	}
	if sqlgraph.IsUniqueConstraintError(err) {
		return service.ErrAccountTestQuestionNameExists
	}
	return err
}

func accountTestQuestionToModel(question *ent.AccountTestQuestion) *model.AccountTestQuestion {
	patterns := question.AnswerPatterns
	if patterns == nil {
		patterns = []string{}
	}
	return &model.AccountTestQuestion{
		ID: question.ID, Name: question.Name, Content: question.Content,
		AnswerPatterns: patterns, Enabled: question.Enabled, SortOrder: question.SortOrder,
		CreatedAt: question.CreatedAt, UpdatedAt: question.UpdatedAt,
	}
}
