package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/model"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrAccountTestQuestionNotFound   = infraerrors.NotFound("ACCOUNT_TEST_QUESTION_NOT_FOUND", "test question not found")
	ErrAccountTestQuestionNameExists = infraerrors.Conflict("ACCOUNT_TEST_QUESTION_NAME_EXISTS", "test question name already exists")
)

// AccountTestQuestionRepository 定义账号测试题目的数据访问接口。
type AccountTestQuestionRepository interface {
	List(ctx context.Context, enabledOnly bool) ([]*model.AccountTestQuestion, error)
	GetByID(ctx context.Context, id int64) (*model.AccountTestQuestion, error)
	Create(ctx context.Context, question *model.AccountTestQuestion) (*model.AccountTestQuestion, error)
	Update(ctx context.Context, question *model.AccountTestQuestion, fields AccountTestQuestionUpdateMask) (*model.AccountTestQuestion, error)
	Delete(ctx context.Context, id int64) error
}

// CreateAccountTestQuestionInput 创建题目时仅允许设置可写字段。
type CreateAccountTestQuestionInput struct {
	Name           string
	Content        string
	AnswerPatterns []string
	Enabled        *bool
	SortOrder      int
}

// UpdateAccountTestQuestionInput 使用指针区分未传值和显式零值。
type UpdateAccountTestQuestionInput struct {
	Name           *string
	Content        *string
	AnswerPatterns *[]string
	Enabled        *bool
	SortOrder      *int
}

// AccountTestQuestionUpdateMask prevents a partial update from overwriting fields read before a concurrent edit.
type AccountTestQuestionUpdateMask struct {
	Name           bool
	Content        bool
	AnswerPatterns bool
	Enabled        bool
	SortOrder      bool
}

// AccountTestQuestionService 管理账号测试题目。
type AccountTestQuestionService struct {
	repo AccountTestQuestionRepository
}

func NewAccountTestQuestionService(repo AccountTestQuestionRepository) *AccountTestQuestionService {
	return &AccountTestQuestionService{repo: repo}
}

func (s *AccountTestQuestionService) List(ctx context.Context, enabledOnly bool) ([]*model.AccountTestQuestion, error) {
	return s.repo.List(ctx, enabledOnly)
}

// GetByID 找不到题目时返回 nil，以便账号测试和管理接口分别处理错误响应。
func (s *AccountTestQuestionService) GetByID(ctx context.Context, id int64) (*model.AccountTestQuestion, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *AccountTestQuestionService) Create(ctx context.Context, input CreateAccountTestQuestionInput) (*model.AccountTestQuestion, error) {
	question := &model.AccountTestQuestion{
		Name: input.Name, Content: input.Content, AnswerPatterns: input.AnswerPatterns,
		Enabled: true, SortOrder: input.SortOrder,
	}
	if input.Enabled != nil {
		question.Enabled = *input.Enabled
	}
	if err := question.NormalizeAndValidate(); err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, question)
}

func (s *AccountTestQuestionService) Update(ctx context.Context, id int64, input UpdateAccountTestQuestionInput) (*model.AccountTestQuestion, error) {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, ErrAccountTestQuestionNotFound
	}

	question := *existing
	if input.Name != nil {
		question.Name = *input.Name
	}
	if input.Content != nil {
		question.Content = *input.Content
	}
	if input.AnswerPatterns != nil {
		question.AnswerPatterns = *input.AnswerPatterns
	}
	if input.Enabled != nil {
		question.Enabled = *input.Enabled
	}
	if input.SortOrder != nil {
		question.SortOrder = *input.SortOrder
	}
	if err := question.NormalizeAndValidate(); err != nil {
		return nil, err
	}
	return s.repo.Update(ctx, &question, AccountTestQuestionUpdateMask{
		Name: input.Name != nil, Content: input.Content != nil, AnswerPatterns: input.AnswerPatterns != nil,
		Enabled: input.Enabled != nil, SortOrder: input.SortOrder != nil,
	})
}

func (s *AccountTestQuestionService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}
