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

type accountTestQuestionHandlerRepo struct {
	question    *model.AccountTestQuestion
	enabledOnly bool
	err         error
}

func (r *accountTestQuestionHandlerRepo) List(_ context.Context, enabledOnly bool) ([]*model.AccountTestQuestion, error) {
	r.enabledOnly = enabledOnly
	questions := []*model.AccountTestQuestion{}
	if r.question != nil && (!enabledOnly || r.question.Enabled) {
		questions = append(questions, r.question)
	}
	return questions, r.err
}

func (r *accountTestQuestionHandlerRepo) GetByID(_ context.Context, id int64) (*model.AccountTestQuestion, error) {
	if r.question == nil || r.question.ID != id {
		return nil, r.err
	}
	return r.question, r.err
}

func (r *accountTestQuestionHandlerRepo) Create(_ context.Context, question *model.AccountTestQuestion) (*model.AccountTestQuestion, error) {
	if r.err != nil {
		return nil, r.err
	}
	question.ID = 1
	r.question = question
	return question, nil
}

func (r *accountTestQuestionHandlerRepo) Update(_ context.Context, question *model.AccountTestQuestion, _ service.AccountTestQuestionUpdateMask) (*model.AccountTestQuestion, error) {
	if r.err != nil {
		return nil, r.err
	}
	r.question = question
	return question, nil
}

func (r *accountTestQuestionHandlerRepo) Delete(_ context.Context, id int64) error {
	if r.err != nil {
		return r.err
	}
	if r.question == nil || r.question.ID != id {
		return service.ErrAccountTestQuestionNotFound
	}
	r.question = nil
	return nil
}

func newAccountTestQuestionTestRouter() (*gin.Engine, *accountTestQuestionHandlerRepo) {
	gin.SetMode(gin.TestMode)
	repo := &accountTestQuestionHandlerRepo{}
	handler := NewAccountTestQuestionHandler(service.NewAccountTestQuestionService(repo))
	router := gin.New()
	router.GET("/questions", handler.List)
	router.GET("/questions/:id", handler.GetByID)
	router.POST("/questions", handler.Create)
	router.PUT("/questions/:id", handler.Update)
	router.DELETE("/questions/:id", handler.Delete)
	return router, repo
}

func requestAccountTestQuestion(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestAccountTestQuestionHandlerCRUD(t *testing.T) {
	router, repo := newAccountTestQuestionTestRouter()
	recorder := requestAccountTestQuestion(router, http.MethodPost, "/questions", `{"name":" name ","content":" content ","answer_patterns":[" one ","","one","two"],"sort_order":8,"id":999}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, int64(1), repo.question.ID)
	require.Equal(t, "name", repo.question.Name)
	require.True(t, repo.question.Enabled)
	require.Equal(t, []string{"one", "two"}, repo.question.AnswerPatterns)

	recorder = requestAccountTestQuestion(router, http.MethodPut, "/questions/1", `{"enabled":false,"sort_order":0,"answer_patterns":[]}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "name", repo.question.Name)
	require.Equal(t, "content", repo.question.Content)
	require.False(t, repo.question.Enabled)
	require.Zero(t, repo.question.SortOrder)
	require.Equal(t, []string{}, repo.question.AnswerPatterns)

	recorder = requestAccountTestQuestion(router, http.MethodPut, "/questions/1", `{}`)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "content", repo.question.Content)

	recorder = requestAccountTestQuestion(router, http.MethodGet, "/questions/1", "")
	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Code int                       `json:"code"`
		Data model.AccountTestQuestion `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.Equal(t, repo.question, &envelope.Data)

	recorder = requestAccountTestQuestion(router, http.MethodGet, "/questions?enabled=true", "")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, repo.enabledOnly)
	require.Contains(t, recorder.Body.String(), `"data":[]`)
	recorder = requestAccountTestQuestion(router, http.MethodGet, "/questions?enabled=false", "")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.False(t, repo.enabledOnly)
	require.Contains(t, recorder.Body.String(), `"id":1`)
	recorder = requestAccountTestQuestion(router, http.MethodGet, "/questions", "")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.False(t, repo.enabledOnly)

	recorder = requestAccountTestQuestion(router, http.MethodDelete, "/questions/1", "")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Nil(t, repo.question)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		recorder = requestAccountTestQuestion(router, method, "/questions/1", `{}`)
		require.Equal(t, http.StatusNotFound, recorder.Code, method)
	}
}

func TestAccountTestQuestionHandlerValidation(t *testing.T) {
	router, repo := newAccountTestQuestionTestRouter()
	repo.question = &model.AccountTestQuestion{ID: 1, Name: "name", Content: "content", Enabled: true, AnswerPatterns: []string{}}
	for _, id := range []string{"invalid", "0", "-1", "9223372036854775808"} {
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			recorder := requestAccountTestQuestion(router, method, "/questions/"+id, `{}`)
			require.Equal(t, http.StatusBadRequest, recorder.Code, "%s %s", method, id)
		}
	}
	for _, body := range []string{`{`, `{}`, `{"name":"x"}`, `{"name":" ","content":"x"}`, `{"name":"x","content":" "}`, `{"name":"x","content":"x","enabled":"false"}`, `{"name":"x","content":"x","answer_patterns":"x"}`} {
		recorder := requestAccountTestQuestion(router, http.MethodPost, "/questions", body)
		require.Equal(t, http.StatusBadRequest, recorder.Code, body)
	}
	for _, body := range []string{`{`, `{"name":" "}`, `{"content":" "}`, `{"enabled":"false"}`, `{"sort_order":1.5}`, `{"answer_patterns":[1]}`} {
		recorder := requestAccountTestQuestion(router, http.MethodPut, "/questions/1", body)
		require.Equal(t, http.StatusBadRequest, recorder.Code, body)
	}
	recorder := requestAccountTestQuestion(router, http.MethodGet, "/questions?enabled=invalid", "")
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "name", repo.question.Name)

	recorder = requestAccountTestQuestion(router, http.MethodPost, "/questions", `{"name":"new","content":"content","enabled":false}`)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.False(t, repo.question.Enabled)
}

func TestAccountTestQuestionHandlerConflict(t *testing.T) {
	router, repo := newAccountTestQuestionTestRouter()
	repo.err = service.ErrAccountTestQuestionNameExists
	recorder := requestAccountTestQuestion(router, http.MethodPost, "/questions", `{"name":"same","content":"content"}`)
	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), "test question name already exists")
}

func TestAccountTestQuestionHandlerSortOrderRange(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		for _, sortOrder := range []struct {
			value string
			valid bool
		}{
			{value: "-2147483648", valid: true},
			{value: "2147483647", valid: true},
			{value: "-2147483649"},
			{value: "2147483648"},
			{value: "-3000000000"},
			{value: "3000000000"},
		} {
			t.Run(method+"/"+sortOrder.value, func(t *testing.T) {
				router, repo := newAccountTestQuestionTestRouter()
				existing := &model.AccountTestQuestion{ID: 1, Name: "old", Content: "content", SortOrder: 8}
				repo.question = existing
				path, body := "/questions", `{"name":"new","content":"content","sort_order":`+sortOrder.value+`}`
				if method == http.MethodPut {
					path, body = "/questions/1", `{"sort_order":`+sortOrder.value+`}`
				}
				recorder := requestAccountTestQuestion(router, method, path, body)
				if sortOrder.valid {
					require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					require.Contains(t, recorder.Body.String(), `"sort_order":`+sortOrder.value)
					return
				}
				require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
				require.Contains(t, recorder.Body.String(), "sort_order must be between -2147483648 and 2147483647")
				require.Same(t, existing, repo.question, "invalid sort order must not write to the repository")
				require.Equal(t, 8, repo.question.SortOrder)
			})
		}
	}
}

func TestAccountTestQuestionHandlerPatterns(t *testing.T) {
	router, repo := newAccountTestQuestionTestRouter()
	recorder := requestAccountTestQuestion(router, http.MethodPost, "/questions", `{"name":"name","content":"content","answer_patterns":[" (?i)claude ","(?s)^first.*last$","^[a-z]{1,3},[0-9]+$"]}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, []string{"(?i)claude", "(?s)^first.*last$", "^[a-z]{1,3},[0-9]+$"}, repo.question.AnswerPatterns)
	require.Contains(t, recorder.Body.String(), `"answer_patterns"`)

	for _, method := range []string{http.MethodPost, http.MethodPut} {
		path := "/questions"
		if method == http.MethodPut {
			path += "/1"
		}
		recorder = requestAccountTestQuestion(router, method, path, `{"name":"name","content":"content","answer_patterns":["","valid","valid"," (?=claude) "]}`)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
		var envelope struct {
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
		require.Contains(t, envelope.Message, `pattern 4 " (?=claude) "`)
		require.Contains(t, envelope.Message, "invalid or unsupported Perl syntax")
	}
	require.Equal(t, []string{"(?i)claude", "(?s)^first.*last$", "^[a-z]{1,3},[0-9]+$"}, repo.question.AnswerPatterns)
}
