//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type batchTestHandlerQuestionRepo struct {
	service.AccountTestQuestionRepository
	questions map[int64]*model.AccountTestQuestion
}

func (r *batchTestHandlerQuestionRepo) GetByID(_ context.Context, id int64) (*model.AccountTestQuestion, error) {
	return r.questions[id], nil
}

func newBatchTestHandlerRouter(questions map[int64]*model.AccountTestQuestion) (*gin.Engine, *accountTestQuestionRequestAccountRepo) {
	gin.SetMode(gin.TestMode)
	accounts := &accountTestQuestionRequestAccountRepo{}
	questionService := service.NewAccountTestQuestionService(&batchTestHandlerQuestionRepo{questions: questions})
	testService := service.NewAccountTestService(accounts, nil, nil, nil, nil, nil, nil, nil)
	handler := NewAccountBatchTestHandler(service.NewAccountBatchTestService(accounts, testService, questionService, nil))
	router := gin.New()
	router.POST("/accounts/batch-test", handler.Test)
	return router, accounts
}

func TestAccountBatchTestHandlerPreflightBadRequest(t *testing.T) {
	questions := map[int64]*model.AccountTestQuestion{
		1: {ID: 1, Enabled: true}, 2: {ID: 2, Enabled: false},
	}
	for _, tt := range []struct{ name, body, message string }{
		{name: "malformed json", body: `{`, message: "Invalid request"},
		{name: "empty request", body: `{}`, message: "account_ids"},
		{name: "missing accounts", body: `{"question_ids":[1]}`, message: "account_ids"},
		{name: "missing questions", body: `{"account_ids":[7]}`, message: "question_ids"},
		{name: "zero account", body: `{"account_ids":[0],"question_ids":[1]}`, message: "positive integers"},
		{name: "negative account", body: `{"account_ids":[-1],"question_ids":[1]}`, message: "positive integers"},
		{name: "zero question", body: `{"account_ids":[7],"question_ids":[0]}`, message: "positive integers"},
		{name: "fractional account", body: `{"account_ids":[1.5],"question_ids":[1]}`, message: "Invalid request"},
		{name: "string question", body: `{"account_ids":[7],"question_ids":["1"]}`, message: "Invalid request"},
		{name: "invalid concurrency", body: `{"account_ids":[7],"question_ids":[1],"concurrency":"3"}`, message: "Invalid request"},
		{name: "missing question", body: `{"account_ids":[7],"question_ids":[1,99]}`, message: "99 not found"},
		{name: "disabled question", body: `{"account_ids":[7],"question_ids":[1,2]}`, message: "2 is disabled"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			router, accounts := newBatchTestHandlerRouter(questions)
			recorder := requestAccountTestQuestion(router, http.MethodPost, "/accounts/batch-test", tt.body)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
			require.Contains(t, recorder.Body.String(), tt.message)
			require.NotContains(t, recorder.Body.String(), "data:")
			require.Empty(t, accounts.ids, "preflight must finish before any account task is started")
		})
	}
	for _, tt := range []struct {
		name          string
		accountCount  int
		questionCount int
		want          string
	}{
		{name: "account limit", accountCount: 201, questionCount: 1, want: "200"},
		{name: "question limit", accountCount: 1, questionCount: 21, want: "20"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			accountIDs := make([]int64, tt.accountCount)
			questionIDs := make([]int64, tt.questionCount)
			for index := range accountIDs {
				accountIDs[index] = int64(index + 1)
			}
			for index := range questionIDs {
				questionIDs[index] = int64(index + 1)
			}
			body, err := json.Marshal(AccountBatchTestRequest{AccountIDs: accountIDs, QuestionIDs: questionIDs})
			require.NoError(t, err)
			router, accounts := newBatchTestHandlerRouter(questions)
			recorder := requestAccountTestQuestion(router, http.MethodPost, "/accounts/batch-test", string(body))
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Contains(t, recorder.Body.String(), tt.want)
			require.NotContains(t, recorder.Body.String(), "data:")
			require.Empty(t, accounts.ids)
		})
	}
}

func TestAccountBatchTestHandlerDedupAndConcurrency(t *testing.T) {
	for _, tt := range []struct {
		name        string
		concurrency string
		want        int
	}{
		{name: "default", want: 3},
		{name: "zero clamp", concurrency: `,"concurrency":0`, want: 1},
		{name: "negative clamp", concurrency: `,"concurrency":-9`, want: 1},
		{name: "upper clamp", concurrency: `,"concurrency":99`, want: 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			router, _ := newBatchTestHandlerRouter(map[int64]*model.AccountTestQuestion{
				42: {ID: 42, Name: "identity", Content: "Identify yourself", Enabled: true, AnswerPatterns: []string{"(?i)anthropic"}},
			})
			body := `{"account_ids":[7,7],"question_ids":[42,42]` + tt.concurrency + `}`
			recorder := requestAccountTestQuestion(router, http.MethodPost, "/accounts/batch-test", body)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
			require.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))
			require.Equal(t, "no", recorder.Header().Get("X-Accel-Buffering"))
			require.True(t, recorder.Flushed)
			frames := strings.Split(strings.TrimSuffix(recorder.Body.String(), "\n\n"), "\n\n")
			require.Len(t, frames, 3)
			for _, frame := range frames {
				require.True(t, strings.HasPrefix(frame, "data: "), frame)
				require.NotContains(t, frame, "\n", "events must not include gin's event: line")
				require.True(t, json.Valid([]byte(strings.TrimPrefix(frame, "data: "))), frame)
			}
			events := accountTestQuestionRequestEvents(t, recorder)
			require.Len(t, events, 3)
			require.Equal(t, "batch_start", events[0].Type)
			var start service.AccountBatchTestStart
			require.NoError(t, json.Unmarshal(events[0].Data, &start))
			require.Equal(t, service.AccountBatchTestStart{Total: 1, AccountCount: 1, QuestionCount: 1, Concurrency: tt.want}, start)
			require.Equal(t, "item_result", events[1].Type)
			var item service.AccountBatchTestItemResult
			require.NoError(t, json.Unmarshal(events[1].Data, &item))
			require.Zero(t, item.Index)
			require.Equal(t, int64(7), item.AccountID)
			require.Equal(t, int64(42), item.QuestionID)
			require.Equal(t, "identity", item.QuestionName)
			require.Equal(t, "passed", item.Status)
			require.True(t, item.Judged)
			require.Equal(t, []string{"(?i)anthropic"}, item.AnswerPatterns)
			require.Equal(t, []string{"(?i)anthropic"}, item.MatchedPatterns)
			require.NotEmpty(t, item.Model, "empty request model must use the single-account path's default")
			require.Contains(t, item.Answer, "Synthetic Anthropic")
			require.Empty(t, item.Error)
			require.Equal(t, "batch_complete", events[2].Type)
			var summary service.AccountBatchTestSummary
			require.NoError(t, json.Unmarshal(events[2].Data, &summary))
			require.Equal(t, service.AccountBatchTestSummary{Total: 1, Passed: 1}, summary)
		})
	}
}

func TestAccountBatchTestHandlerSerialQuestionResults(t *testing.T) {
	router, _ := newBatchTestHandlerRouter(map[int64]*model.AccountTestQuestion{
		1: {ID: 1, Name: "passes", Content: "First", Enabled: true, AnswerPatterns: []string{"(?i)anthropic"}},
		2: {ID: 2, Name: "fails", Content: "Second", Enabled: true, AnswerPatterns: []string{"^other-answer$"}},
		3: {ID: 3, Name: "manual", Content: "Third", Enabled: true},
	})
	recorder := requestAccountTestQuestion(router, http.MethodPost, "/accounts/batch-test", `{
		"account_ids":[7],"question_ids":[2,1,3],"model_id":"custom-text-model","concurrency":1
	}`)
	require.Equal(t, http.StatusOK, recorder.Code)
	events := accountTestQuestionRequestEvents(t, recorder)
	require.Len(t, events, 5)
	for index, status := range []string{"failed", "passed", "unjudged"} {
		var item service.AccountBatchTestItemResult
		require.NoError(t, json.Unmarshal(events[index+1].Data, &item))
		require.Equal(t, index, item.Index)
		require.Equal(t, status, item.Status)
		require.Equal(t, "custom-text-model", item.Model)
		if status == "failed" {
			require.Equal(t, "Answer did not match any expected pattern", item.Error)
			require.Equal(t, []string{}, item.MatchedPatterns)
		}
		if status == "unjudged" {
			require.False(t, item.Judged)
			require.Equal(t, []string{}, item.AnswerPatterns)
			require.Equal(t, []string{}, item.MatchedPatterns)
		}
	}
	var summary service.AccountBatchTestSummary
	require.NoError(t, json.Unmarshal(events[4].Data, &summary))
	require.Equal(t, service.AccountBatchTestSummary{Total: 3, Passed: 1, Failed: 1, Unjudged: 1}, summary)
}

func TestAccountBatchTestHandlerCancelledRequestDoesNotStream(t *testing.T) {
	router, accounts := newBatchTestHandlerRouter(map[int64]*model.AccountTestQuestion{
		1: {ID: 1, Name: "question", Content: "Question", Enabled: true},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/accounts/batch-test", strings.NewReader(`{"account_ids":[7],"question_ids":[1]}`)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Empty(t, recorder.Body.String())
	require.False(t, recorder.Flushed)
	require.Empty(t, accounts.ids)
}
