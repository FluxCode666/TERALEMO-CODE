//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountTestQuestionRequestRepo struct {
	service.AccountTestQuestionRepository
	question *model.AccountTestQuestion
	err      error
	ids      []int64
	contexts []context.Context
}

func (r *accountTestQuestionRequestRepo) GetByID(ctx context.Context, id int64) (*model.AccountTestQuestion, error) {
	r.ids = append(r.ids, id)
	r.contexts = append(r.contexts, ctx)
	return r.question, r.err
}

type accountTestQuestionRequestAccountRepo struct {
	service.AccountRepository
	ids           []int64
	status        string
	clearedErrors []int64
}

func (r *accountTestQuestionRequestAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	r.ids = append(r.ids, id)
	return &service.Account{
		ID: id, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: r.status,
		Extra: map[string]any{"synthetic_ui_test": true},
	}, nil
}

func (r *accountTestQuestionRequestAccountRepo) ClearError(_ context.Context, id int64) error {
	r.clearedErrors = append(r.clearedErrors, id)
	return nil
}

func newAccountTestQuestionRequestRouter(questionRepo *accountTestQuestionRequestRepo) (*gin.Engine, *accountTestQuestionRequestAccountRepo) {
	gin.SetMode(gin.TestMode)
	accountRepo := &accountTestQuestionRequestAccountRepo{}
	testService := service.NewAccountTestService(accountRepo, nil, nil, nil, nil, nil, nil, nil)
	handler := NewAccountHandler(nil, nil, nil, nil, nil, nil, nil, nil, testService, nil, nil, nil, nil, nil)
	if questionRepo != nil {
		handler.SetAccountTestQuestionService(service.NewAccountTestQuestionService(questionRepo))
	}
	router := gin.New()
	router.POST("/accounts/:id/test", handler.Test)
	return router, accountRepo
}

type accountTestQuestionRequestEvent struct {
	Type    string          `json:"type"`
	Model   string          `json:"model"`
	Text    string          `json:"text"`
	Error   string          `json:"error"`
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
}

func accountTestQuestionRequestEvents(t *testing.T, recorder *httptest.ResponseRecorder) []accountTestQuestionRequestEvent {
	t.Helper()
	events := []accountTestQuestionRequestEvent{}
	for _, line := range strings.Split(recorder.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event accountTestQuestionRequestEvent
		require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event))
		events = append(events, event)
	}
	return events
}

func TestAccountTestQuestionRequestBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name string
		body string
		id   int64
	}{
		{name: "omitted", body: `{}`},
		{name: "zero", body: `{"test_question_id":0}`},
		{name: "negative", body: `{"test_question_id":-1}`, id: -1},
		{name: "selected", body: `{"test_question_id":42}`, id: 42},
		{name: "int64", body: `{"test_question_id":9223372036854775807}`, id: 9223372036854775807},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/accounts/7/test", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")
			var request TestAccountRequest
			require.NoError(t, c.ShouldBindJSON(&request))
			require.Equal(t, tt.id, request.TestQuestionID)
		})
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/accounts/7/test", strings.NewReader(`{
		"test_question_id":42,"model_id":"custom-text-model","prompt":"ignored prompt","mode":"compact",
		"image_data_url":"invalid-image","audio_data_url":"invalid-audio"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	var request TestAccountRequest
	require.NoError(t, c.ShouldBindJSON(&request))
	require.Equal(t, TestAccountRequest{
		TestQuestionID: 42, ModelID: "custom-text-model", Prompt: "ignored prompt", Mode: "compact",
		ImageDataURL: "invalid-image", AudioDataURL: "invalid-audio",
	}, request)
}

func TestAccountHandlerTestQuestionLookupErrors(t *testing.T) {
	for _, tt := range []struct {
		name      string
		repo      *accountTestQuestionRequestRepo
		wantError string
	}{
		{name: "missing service", wantError: "Test question not found"},
		{name: "missing question", repo: &accountTestQuestionRequestRepo{}, wantError: "Test question not found"},
		{name: "disabled question", repo: &accountTestQuestionRequestRepo{
			question: &model.AccountTestQuestion{ID: 42, Enabled: false},
		}, wantError: "Test question is disabled"},
		{name: "repository error", repo: &accountTestQuestionRequestRepo{
			err: errors.New("database unavailable"),
		}, wantError: "Failed to load test question"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			router, accountRepo := newAccountTestQuestionRequestRouter(tt.repo)
			recorder := requestAccountTestQuestion(router, http.MethodPost, "/accounts/7/test", `{"test_question_id":42}`)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
			require.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))
			require.Equal(t, "no", recorder.Header().Get("X-Accel-Buffering"))
			require.Equal(t, "data: {\"type\":\"error\",\"error\":\""+tt.wantError+"\"}\n\n", recorder.Body.String())
			require.True(t, recorder.Flushed)
			require.Equal(t, []accountTestQuestionRequestEvent{{Type: "error", Error: tt.wantError}}, accountTestQuestionRequestEvents(t, recorder))
			require.Empty(t, accountRepo.ids, "invalid questions must stop before account testing")
			if tt.repo != nil {
				require.Equal(t, []int64{42}, tt.repo.ids)
			}
		})
	}
}

func TestAccountHandlerTestQuestionDefaultPreservesConnectivity(t *testing.T) {
	var baseline string
	for _, tt := range []struct{ name, body string }{
		{name: "empty body"},
		{name: "omitted", body: `{}`},
		{name: "zero", body: `{"test_question_id":0}`},
		{name: "negative", body: `{"test_question_id":-1}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &accountTestQuestionRequestRepo{err: errors.New("must not read questions")}
			router, accountRepo := newAccountTestQuestionRequestRouter(repo)
			recorder := requestAccountTestQuestion(router, http.MethodPost, "/accounts/7/test", tt.body)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Empty(t, repo.ids)
			require.Equal(t, []int64{7}, accountRepo.ids)
			events := accountTestQuestionRequestEvents(t, recorder)
			require.Len(t, events, 3)
			require.Equal(t, "test_start", events[0].Type)
			require.Equal(t, "content", events[1].Type)
			require.Equal(t, "test_complete", events[2].Type)
			require.True(t, events[2].Success)
			if baseline == "" {
				baseline = recorder.Body.String()
			} else {
				require.Equal(t, baseline, recorder.Body.String())
			}
		})
	}
}

func TestAccountHandlerTestQuestionSelected(t *testing.T) {
	repo := &accountTestQuestionRequestRepo{question: &model.AccountTestQuestion{
		ID: 42, Name: "Identity check", Content: "Identify yourself", Enabled: true,
		AnswerPatterns: []string{"(?i)anthropic"},
	}}
	router, accountRepo := newAccountTestQuestionRequestRouter(repo)
	recorder := requestAccountTestQuestion(router, http.MethodPost, "/accounts/7/test", `{
		"test_question_id":42,"model_id":"custom-text-model","prompt":"ignored prompt","mode":"compact",
		"image_data_url":"invalid-image","audio_data_url":"invalid-audio"
	}`)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []int64{42}, repo.ids)
	require.Len(t, repo.contexts, 1)
	require.NoError(t, repo.contexts[0].Err())
	require.Equal(t, []int64{7}, accountRepo.ids)
	events := accountTestQuestionRequestEvents(t, recorder)
	require.Len(t, events, 4)
	require.Equal(t, "test_start", events[0].Type)
	require.Equal(t, "custom-text-model", events[0].Model)
	require.Equal(t, "content", events[1].Type)
	require.Equal(t, "question_result", events[2].Type)
	require.True(t, events[2].Success)
	var result service.AccountTestQuestionResult
	require.NoError(t, json.Unmarshal(events[2].Data, &result))
	require.Equal(t, service.AccountTestQuestionResult{
		QuestionID: 42, QuestionName: "Identity check", Judged: true,
		AnswerPatterns: []string{"(?i)anthropic"}, MatchedPatterns: []string{"(?i)anthropic"},
	}, result)
	require.Equal(t, "test_complete", events[3].Type)
	require.True(t, events[3].Success)
}

func TestAccountHandlerTestQuestionMismatchPreservesConnectivityAndRecovery(t *testing.T) {
	question := &model.AccountTestQuestion{
		ID: 42, Name: "Unmatched question", Content: "Identify yourself", Enabled: true,
		AnswerPatterns: []string{"^not-the-synthetic-response$"},
	}
	assertMismatch := func(t *testing.T, recorder *httptest.ResponseRecorder) {
		t.Helper()
		events := accountTestQuestionRequestEvents(t, recorder)
		require.Len(t, events, 4)
		require.Equal(t, "question_result", events[2].Type)
		require.False(t, events[2].Success)
		var result service.AccountTestQuestionResult
		require.NoError(t, json.Unmarshal(events[2].Data, &result))
		require.True(t, result.Judged)
		require.Equal(t, question.AnswerPatterns, result.AnswerPatterns)
		require.Equal(t, []string{}, result.MatchedPatterns)
		require.Equal(t, "test_complete", events[3].Type)
		require.False(t, events[3].Success)
		require.Equal(t, "Answer did not match any expected pattern", events[3].Error)
	}

	t.Run("real test service returns nil", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		accountRepo := &accountTestQuestionRequestAccountRepo{}
		testService := service.NewAccountTestService(accountRepo, nil, nil, nil, nil, nil, nil, nil)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/accounts/7/test", nil)
		err := testService.TestAccountConnection(c, 7, "custom-text-model", "", "", service.AccountTestOptions{Question: question})
		require.NoError(t, err, "pattern mismatch must not change the connectivity result")
		assertMismatch(t, recorder)
	})

	t.Run("handler still recovers account", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		questionRepo := &accountTestQuestionRequestRepo{question: question}
		accountRepo := &accountTestQuestionRequestAccountRepo{status: service.StatusError}
		testService := service.NewAccountTestService(accountRepo, nil, nil, nil, nil, nil, nil, nil)
		rateLimitService := service.NewRateLimitService(accountRepo, nil, nil, nil, nil)
		handler := NewAccountHandler(nil, nil, nil, nil, nil, nil, rateLimitService, nil, testService, nil, nil, nil, nil, nil)
		handler.SetAccountTestQuestionService(service.NewAccountTestQuestionService(questionRepo))
		router := gin.New()
		router.POST("/accounts/:id/test", handler.Test)
		recorder := requestAccountTestQuestion(router, http.MethodPost, "/accounts/7/test", `{"test_question_id":42,"model_id":"custom-text-model"}`)
		require.Equal(t, http.StatusOK, recorder.Code)
		assertMismatch(t, recorder)
		require.Equal(t, []int64{42}, questionRepo.ids)
		require.Equal(t, []int64{7, 7}, accountRepo.ids, "connectivity and recovery must each load the account")
		require.Equal(t, []int64{7}, accountRepo.clearedErrors, "question mismatch must not skip successful-connectivity recovery")
	})
}

func TestAccountHandlerTestQuestionInvalidAccountID(t *testing.T) {
	repo := &accountTestQuestionRequestRepo{}
	router, accountRepo := newAccountTestQuestionRequestRouter(repo)
	recorder := requestAccountTestQuestion(router, http.MethodPost, "/accounts/invalid/test", `{"test_question_id":42}`)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "Invalid account ID")
	require.Empty(t, repo.ids)
	require.Empty(t, accountRepo.ids)
}
