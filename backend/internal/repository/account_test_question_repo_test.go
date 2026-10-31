//go:build unit

package repository

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/accounttestquestion"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newAccountTestQuestionTestRepository(t *testing.T) (service.AccountTestQuestionRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() {
		_ = client.Close()
		require.NoError(t, mock.ExpectationsWereMet())
	})
	return NewAccountTestQuestionRepository(client), mock
}

func TestAccountTestQuestionRepositoryList(t *testing.T) {
	for _, enabledOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "all", true: "enabled"}[enabledOnly], func(t *testing.T) {
			repo, mock := newAccountTestQuestionTestRepository(t)
			where := ""
			if enabledOnly {
				where = ` WHERE "account_test_questions"."enabled"`
			}
			query := mock.ExpectQuery(`SELECT .* FROM ` + regexp.QuoteMeta(`"account_test_questions"`+where+` ORDER BY "account_test_questions"."sort_order" ASC, "account_test_questions"."id" ASC`))
			now := time.Now()
			query.WillReturnRows(sqlmock.NewRows(accounttestquestion.Columns).
				AddRow(int64(1), now, now, "one", "question", []byte(`["answer"]`), true, -1).
				AddRow(int64(3), now, now, "three", "question", []byte(`[]`), true, 0))
			questions, err := repo.List(context.Background(), enabledOnly)
			require.NoError(t, err)
			require.Len(t, questions, 2)
			require.Equal(t, int64(1), questions[0].ID)
			require.Equal(t, []string{"answer"}, questions[0].AnswerPatterns)
			require.Equal(t, []string{}, questions[1].AnswerPatterns)
			require.WithinDuration(t, now, questions[0].CreatedAt, time.Millisecond)
		})
	}
}

func TestAccountTestQuestionRepositoryGetAndEmptyList(t *testing.T) {
	repo, mock := newAccountTestQuestionTestRepository(t)
	mock.ExpectQuery(`SELECT .* FROM "account_test_questions" WHERE "account_test_questions"."id" = \$1 LIMIT 2`).
		WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows(accounttestquestion.Columns))
	question, err := repo.GetByID(context.Background(), 9)
	require.NoError(t, err)
	require.Nil(t, question)

	mock.ExpectQuery(`SELECT .* FROM "account_test_questions"`).
		WillReturnRows(sqlmock.NewRows(accounttestquestion.Columns))
	questions, err := repo.List(context.Background(), false)
	require.NoError(t, err)
	require.Equal(t, []*model.AccountTestQuestion{}, questions)

	now := time.Now()
	mock.ExpectQuery(`SELECT .* FROM "account_test_questions" WHERE "account_test_questions"."id" = \$1 LIMIT 2`).
		WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows(accounttestquestion.Columns).
		AddRow(int64(2), now, now, "name", "content", []byte(`null`), false, 7))
	question, err = repo.GetByID(context.Background(), 2)
	require.NoError(t, err)
	require.Equal(t, []string{}, question.AnswerPatterns)
	require.False(t, question.Enabled)
	require.Equal(t, 7, question.SortOrder)
}

func TestAccountTestQuestionRepositoryCreate(t *testing.T) {
	repo, mock := newAccountTestQuestionTestRepository(t)
	question := &model.AccountTestQuestion{
		Name: strings.Repeat("题", 100), Content: strings.Repeat("文", 20000), AnswerPatterns: []string{"answer"}, Enabled: true, SortOrder: -3,
	}
	mock.ExpectQuery(`INSERT INTO "account_test_questions" .* RETURNING "id"`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), question.Name, question.Content, []byte(`["answer"]`), true, -3).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(5)))
	created, err := repo.Create(context.Background(), question)
	require.NoError(t, err)
	require.Equal(t, int64(5), created.ID)
	require.Equal(t, question.Name, created.Name)
	require.Equal(t, question.Content, created.Content)
	require.Equal(t, question.AnswerPatterns, created.AnswerPatterns)
	require.Equal(t, question.Enabled, created.Enabled)
	require.Equal(t, question.SortOrder, created.SortOrder)
}

func TestAccountTestQuestionRepositoryUpdate(t *testing.T) {
	repo, mock := newAccountTestQuestionTestRepository(t)
	ctx := context.Background()
	question := &model.AccountTestQuestion{ID: 5, Name: "updated", Content: "content", AnswerPatterns: []string{}, Enabled: false, SortOrder: 0}
	now := time.Now()
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "account_test_questions" SET .* WHERE "id" = \$7`).
		WithArgs(sqlmock.AnyArg(), question.Name, question.Content, []byte(`[]`), false, 0, int64(5)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT .* FROM "account_test_questions" WHERE "id" = \$1`).
		WithArgs(int64(5)).WillReturnRows(sqlmock.NewRows(accounttestquestion.Columns).
		AddRow(int64(5), now, now, "updated", "content", []byte(`[]`), false, 0))
	mock.ExpectCommit()
	updated, err := repo.Update(ctx, question, service.AccountTestQuestionUpdateMask{
		Name: true, Content: true, AnswerPatterns: true, Enabled: true, SortOrder: true,
	})
	require.NoError(t, err)
	require.Equal(t, question.Name, updated.Name)
	require.Equal(t, []string{}, updated.AnswerPatterns)
	require.False(t, updated.Enabled)
	require.Zero(t, updated.SortOrder)
}

func TestAccountTestQuestionRepositoryUpdateEnabledOnlyPreservesConcurrentFields(t *testing.T) {
	repo, mock := newAccountTestQuestionTestRepository(t)
	stale := &model.AccountTestQuestion{
		ID: 5, Name: "old name", Content: "old content", AnswerPatterns: []string{"old"}, Enabled: false, SortOrder: 8,
	}
	now := time.Now()
	mock.ExpectBegin()
	mock.ExpectExec("^"+regexp.QuoteMeta(`UPDATE "account_test_questions" SET "updated_at" = $1, "enabled" = $2 WHERE "id" = $3`)+"$").
		WithArgs(sqlmock.AnyArg(), false, int64(5)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT .* FROM "account_test_questions" WHERE "id" = \$1`).
		WithArgs(int64(5)).WillReturnRows(sqlmock.NewRows(accounttestquestion.Columns).
		AddRow(int64(5), now, now, "concurrent name", "concurrent content", []byte(`["concurrent"]`), false, 9))
	mock.ExpectCommit()
	updated, err := repo.Update(context.Background(), stale, service.AccountTestQuestionUpdateMask{Enabled: true})
	require.NoError(t, err)
	require.False(t, updated.Enabled)
	require.Equal(t, "concurrent name", updated.Name)
	require.Equal(t, "concurrent content", updated.Content)
	require.Equal(t, []string{"concurrent"}, updated.AnswerPatterns)
	require.Equal(t, 9, updated.SortOrder)
}

func TestAccountTestQuestionRepositoryDeleteNotFound(t *testing.T) {
	repo, mock := newAccountTestQuestionTestRepository(t)
	for _, affected := range []int64{1, 0} {
		mock.ExpectExec(`DELETE FROM "account_test_questions" WHERE "account_test_questions"."id" = \$1`).
			WithArgs(int64(5)).WillReturnResult(sqlmock.NewResult(0, affected))
		err := repo.Delete(context.Background(), 5)
		if affected == 0 {
			require.ErrorIs(t, err, service.ErrAccountTestQuestionNotFound)
		} else {
			require.NoError(t, err)
		}
	}
}

func TestAccountTestQuestionRepositoryDuplicateName(t *testing.T) {
	repo, mock := newAccountTestQuestionTestRepository(t)
	mock.ExpectQuery(`INSERT INTO "account_test_questions"`).
		WillReturnError(errors.New(`pq: duplicate key value violates unique constraint "account_test_questions_name_key"`))
	_, err := repo.Create(context.Background(), &model.AccountTestQuestion{Name: "duplicate", Content: "content", AnswerPatterns: []string{}})
	require.ErrorIs(t, err, service.ErrAccountTestQuestionNameExists)
}
