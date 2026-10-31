//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type scheduledQuestionTestHandlerStub struct {
	plans    []*service.ScheduledQuestionTestPlan
	plan     *service.ScheduledQuestionTestPlan
	runs     []*service.ScheduledQuestionTestRun
	accepted *service.ScheduledQuestionTestAccepted
	err      error

	ctx    context.Context
	method string
	id     int64
	limit  int
	input  service.ScheduledQuestionTestPlanInput
}

func (s *scheduledQuestionTestHandlerStub) List(ctx context.Context) ([]*service.ScheduledQuestionTestPlan, error) {
	s.ctx, s.method = ctx, "List"
	return s.plans, s.err
}

func (s *scheduledQuestionTestHandlerStub) GetByID(ctx context.Context, id int64) (*service.ScheduledQuestionTestPlan, error) {
	s.ctx, s.method, s.id = ctx, "GetByID", id
	return s.plan, s.err
}

func (s *scheduledQuestionTestHandlerStub) Create(ctx context.Context, input service.ScheduledQuestionTestPlanInput) (*service.ScheduledQuestionTestPlan, error) {
	s.ctx, s.method, s.input = ctx, "Create", input
	return s.plan, s.err
}

func (s *scheduledQuestionTestHandlerStub) Update(ctx context.Context, id int64, input service.ScheduledQuestionTestPlanInput) (*service.ScheduledQuestionTestPlan, error) {
	s.ctx, s.method, s.id, s.input = ctx, "Update", id, input
	return s.plan, s.err
}

func (s *scheduledQuestionTestHandlerStub) Delete(ctx context.Context, id int64) error {
	s.ctx, s.method, s.id = ctx, "Delete", id
	return s.err
}

func (s *scheduledQuestionTestHandlerStub) ListRuns(ctx context.Context, id int64, limit int) ([]*service.ScheduledQuestionTestRun, error) {
	s.ctx, s.method, s.id, s.limit = ctx, "ListRuns", id, limit
	return s.runs, s.err
}

func (s *scheduledQuestionTestHandlerStub) RunNow(ctx context.Context, id int64) (*service.ScheduledQuestionTestAccepted, error) {
	s.ctx, s.method, s.id = ctx, "RunNow", id
	return s.accepted, s.err
}

func newScheduledQuestionTestHandlerRouter(stub *scheduledQuestionTestHandlerStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	handler := &ScheduledQuestionTestHandler{service: stub}
	router := gin.New()
	router.GET("/plans", handler.List)
	router.GET("/plans/:id", handler.GetByID)
	router.POST("/plans", handler.Create)
	router.PUT("/plans/:id", handler.Update)
	router.DELETE("/plans/:id", handler.Delete)
	router.GET("/plans/:id/runs", handler.ListRuns)
	router.POST("/plans/:id/run-now", handler.RunNow)
	return router
}

func requestScheduledQuestionTest(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func scheduledQuestionTestResponseData(t *testing.T, recorder *httptest.ResponseRecorder) json.RawMessage {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.Equal(t, "success", envelope.Message)
	require.NotEmpty(t, envelope.Data)
	return envelope.Data
}

func TestScheduledQuestionTestHandlerCRUDEnvelope(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	plan := &service.ScheduledQuestionTestPlan{
		ID: 12, Name: "Daily questions", AccountIDs: []int64{1, 2}, QuestionIDs: []int64{4},
		ModelID: "model", CronExpression: "0 9 * * *", Enabled: true, MinPassRatio: 80,
		CooldownMinutes: 360, NotifyEnabled: true, LastRunAt: &now, NextRunAt: &now,
		CreatedAt: now, UpdatedAt: now, LastStatus: "success", PassedAccounts: 1,
		TotalAccounts: 2, AlertedAccounts: 1,
	}
	stub := &scheduledQuestionTestHandlerStub{plan: plan, plans: []*service.ScheduledQuestionTestPlan{plan}}
	router := newScheduledQuestionTestHandlerRouter(stub)

	recorder := requestScheduledQuestionTest(router, http.MethodGet, "/plans", "")
	var plans []*service.ScheduledQuestionTestPlan
	require.NoError(t, json.Unmarshal(scheduledQuestionTestResponseData(t, recorder), &plans))
	require.Equal(t, []*service.ScheduledQuestionTestPlan{plan}, plans)
	require.Equal(t, "List", stub.method)

	for _, request := range []struct{ method, path, body, serviceMethod string }{
		{http.MethodGet, "/plans/12", "", "GetByID"},
		{http.MethodPost, "/plans", `{"name":"Daily questions","account_ids":[1,2],"question_ids":[4],"cron_expression":"0 9 * * *"}`, "Create"},
		{http.MethodPut, "/plans/12", `{"name":"Daily questions"}`, "Update"},
	} {
		t.Run(request.serviceMethod, func(t *testing.T) {
			recorder := requestScheduledQuestionTest(router, request.method, request.path, request.body)
			var result service.ScheduledQuestionTestPlan
			require.NoError(t, json.Unmarshal(scheduledQuestionTestResponseData(t, recorder), &result))
			require.Equal(t, plan, &result)
			require.Equal(t, request.serviceMethod, stub.method)
			if request.method != http.MethodPost {
				require.Equal(t, int64(12), stub.id)
			}
		})
	}

	recorder = requestScheduledQuestionTest(router, http.MethodDelete, "/plans/12", "")
	require.JSONEq(t, `{"message":"Scheduled question test plan deleted successfully"}`, string(scheduledQuestionTestResponseData(t, recorder)))
	require.Equal(t, "Delete", stub.method)
	require.Equal(t, int64(12), stub.id)
}

func TestScheduledQuestionTestHandlerRunningJSON(t *testing.T) {
	for _, running := range []bool{false, true} {
		for _, path := range []string{"/plans", "/plans/12"} {
			t.Run(fmt.Sprintf("%s/running=%t", path, running), func(t *testing.T) {
				plan := &service.ScheduledQuestionTestPlan{ID: 12, Running: running}
				stub := &scheduledQuestionTestHandlerStub{plan: plan, plans: []*service.ScheduledQuestionTestPlan{plan}}
				recorder := requestScheduledQuestionTest(newScheduledQuestionTestHandlerRouter(stub), http.MethodGet, path, "")
				scheduledQuestionTestResponseData(t, recorder)
				require.Contains(t, recorder.Body.String(), fmt.Sprintf(`"running":%t`, running))
			})
		}
	}
}

func TestScheduledQuestionTestHandlerInputPresence(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			stub := &scheduledQuestionTestHandlerStub{plan: &service.ScheduledQuestionTestPlan{ID: 12}}
			router := newScheduledQuestionTestHandlerRouter(stub)
			path := "/plans"
			if method == http.MethodPut {
				path += "/12"
			}
			recorder := requestScheduledQuestionTest(router, method, path, `{
				"name":" Plan ","account_ids":[2,1],"question_ids":[9,8],"model_id":"",
				"cron_expression":"*/5 * * * *","enabled":false,"min_pass_ratio":0,
				"cooldown_minutes":0,"notify_enabled":false,"id":999,"last_run_at":"malicious",
				"next_run_at":"malicious","passed_accounts":999,"last_status":"malicious"
			}`)
			scheduledQuestionTestResponseData(t, recorder)
			require.NotNil(t, stub.input.Name)
			require.Equal(t, " Plan ", *stub.input.Name)
			require.NotNil(t, stub.input.AccountIDs)
			require.Equal(t, []int64{2, 1}, *stub.input.AccountIDs)
			require.NotNil(t, stub.input.QuestionIDs)
			require.Equal(t, []int64{9, 8}, *stub.input.QuestionIDs)
			require.NotNil(t, stub.input.ModelID)
			require.Empty(t, *stub.input.ModelID)
			require.NotNil(t, stub.input.CronExpression)
			require.Equal(t, "*/5 * * * *", *stub.input.CronExpression)
			require.NotNil(t, stub.input.Enabled)
			require.False(t, *stub.input.Enabled)
			require.NotNil(t, stub.input.MinPassRatio)
			require.Zero(t, *stub.input.MinPassRatio)
			require.NotNil(t, stub.input.CooldownMinutes)
			require.Zero(t, *stub.input.CooldownMinutes)
			require.NotNil(t, stub.input.NotifyEnabled)
			require.False(t, *stub.input.NotifyEnabled)
			require.Equal(t, int64(12), stub.plan.ID)
		})
	}

	for _, body := range []string{`{}`, `{"enabled":false}`, `{"account_ids":[],"question_ids":[]}`} {
		t.Run(body, func(t *testing.T) {
			stub := &scheduledQuestionTestHandlerStub{plan: &service.ScheduledQuestionTestPlan{ID: 12}}
			recorder := requestScheduledQuestionTest(newScheduledQuestionTestHandlerRouter(stub), http.MethodPut, "/plans/12", body)
			scheduledQuestionTestResponseData(t, recorder)
			require.Nil(t, stub.input.Name)
			require.Nil(t, stub.input.ModelID)
			require.Nil(t, stub.input.CronExpression)
			require.Nil(t, stub.input.MinPassRatio)
			require.Nil(t, stub.input.CooldownMinutes)
			require.Nil(t, stub.input.NotifyEnabled)
			switch body {
			case `{}`:
				require.Nil(t, stub.input.AccountIDs)
				require.Nil(t, stub.input.QuestionIDs)
				require.Nil(t, stub.input.Enabled)
			case `{"enabled":false}`:
				require.NotNil(t, stub.input.Enabled)
				require.False(t, *stub.input.Enabled)
				require.Nil(t, stub.input.AccountIDs)
				require.Nil(t, stub.input.QuestionIDs)
			default:
				require.NotNil(t, stub.input.AccountIDs)
				require.Empty(t, *stub.input.AccountIDs)
				require.NotNil(t, stub.input.QuestionIDs)
				require.Empty(t, *stub.input.QuestionIDs)
			}
		})
	}
}

func TestScheduledQuestionTestHandlerRejectsInvalidJSON(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		for _, body := range []string{
			"", `{`, `[]`, `{"name":1}`, `{"account_ids":"1"}`, `{"account_ids":[1.5]}`,
			`{"question_ids":["1"]}`, `{"model_id":false}`, `{"cron_expression":0}`,
			`{"enabled":"false"}`, `{"min_pass_ratio":0.5}`, `{"cooldown_minutes":"1"}`,
			`{"notify_enabled":1}`, `{"min_pass_ratio":9223372036854775808}`,
		} {
			t.Run(method+"/"+body, func(t *testing.T) {
				stub := &scheduledQuestionTestHandlerStub{}
				path := "/plans"
				if method == http.MethodPut {
					path += "/12"
				}
				recorder := requestScheduledQuestionTest(newScheduledQuestionTestHandlerRouter(stub), method, path, body)
				require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
				require.Contains(t, recorder.Body.String(), "Invalid request")
				require.Empty(t, stub.method)
			})
		}
	}
}

func TestScheduledQuestionTestHandlerRejectsInvalidID(t *testing.T) {
	for _, id := range []string{"invalid", "0", "-1", "9223372036854775808"} {
		for _, request := range []struct{ method, suffix string }{
			{http.MethodGet, ""}, {http.MethodPut, ""}, {http.MethodDelete, ""},
			{http.MethodGet, "/runs"}, {http.MethodPost, "/run-now"},
		} {
			t.Run(request.method+"/"+id+request.suffix, func(t *testing.T) {
				stub := &scheduledQuestionTestHandlerStub{}
				recorder := requestScheduledQuestionTest(newScheduledQuestionTestHandlerRouter(stub), request.method, "/plans/"+id+request.suffix, `{}`)
				require.Equal(t, http.StatusBadRequest, recorder.Code)
				require.Empty(t, stub.method)
			})
		}
	}
}

func TestScheduledQuestionTestHandlerListRuns(t *testing.T) {
	run := &service.ScheduledQuestionTestRun{
		ID: 5, PlanID: 12, Status: "success", TotalAccounts: 1,
		Summary: []service.ScheduledQuestionTestAccountSummary{{AccountID: 2, Total: 3, Ratio: nil, Alerting: false, StateAfter: "ok", Error: "manual review"}},
	}
	for _, limit := range []struct {
		query string
		value int
	}{
		{"", 50}, {"?limit=1", 1}, {"?limit=200", 200},
	} {
		t.Run(limit.query, func(t *testing.T) {
			stub := &scheduledQuestionTestHandlerStub{runs: []*service.ScheduledQuestionTestRun{run}}
			recorder := requestScheduledQuestionTest(newScheduledQuestionTestHandlerRouter(stub), http.MethodGet, "/plans/12/runs"+limit.query, "")
			var result []*service.ScheduledQuestionTestRun
			require.NoError(t, json.Unmarshal(scheduledQuestionTestResponseData(t, recorder), &result))
			require.Equal(t, []*service.ScheduledQuestionTestRun{run}, result)
			require.Equal(t, "ListRuns", stub.method)
			require.Equal(t, int64(12), stub.id)
			require.Equal(t, limit.value, stub.limit)
			require.Contains(t, recorder.Body.String(), `"ratio":null`)
			require.Contains(t, recorder.Body.String(), `"alerting":false`)
			require.Contains(t, recorder.Body.String(), `"state_after":"ok"`)
		})
	}
	for _, limit := range []string{"", "0", "-1", "201", "invalid", "1.5", "9223372036854775808"} {
		t.Run("invalid/"+limit, func(t *testing.T) {
			stub := &scheduledQuestionTestHandlerStub{}
			recorder := requestScheduledQuestionTest(newScheduledQuestionTestHandlerRouter(stub), http.MethodGet, "/plans/12/runs?limit="+limit, "")
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Empty(t, stub.method)
		})
	}
}

func TestScheduledQuestionTestHandlerEmptyLists(t *testing.T) {
	for _, path := range []string{"/plans", "/plans/12/runs"} {
		stub := &scheduledQuestionTestHandlerStub{}
		recorder := requestScheduledQuestionTest(newScheduledQuestionTestHandlerRouter(stub), http.MethodGet, path, "")
		require.JSONEq(t, `[]`, string(scheduledQuestionTestResponseData(t, recorder)))
	}
}

func TestScheduledQuestionTestHandlerRunNow(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			stub := &scheduledQuestionTestHandlerStub{
				plan:     &service.ScheduledQuestionTestPlan{ID: 12, Enabled: enabled},
				accepted: &service.ScheduledQuestionTestAccepted{Accepted: true, PlanID: 12},
			}
			recorder := requestScheduledQuestionTest(newScheduledQuestionTestHandlerRouter(stub), http.MethodPost, "/plans/12/run-now", "")
			require.Equal(t, http.StatusAccepted, recorder.Code, recorder.Body.String())
			require.JSONEq(t, `{"code":0,"message":"accepted","data":{"accepted":true,"plan_id":12}}`, recorder.Body.String())
			require.Equal(t, "RunNow", stub.method)
			require.Equal(t, int64(12), stub.id)
		})
	}
}

func TestScheduledQuestionTestHandlerRunNowCanceledRequestDoesNotOverrideServiceResult(t *testing.T) {
	for _, deadlineExceeded := range []bool{false, true} {
		for _, result := range []struct {
			name   string
			err    error
			status int
		}{
			{"accepted", nil, http.StatusAccepted},
			{"not found", service.ErrScheduledQuestionTestNotFound, http.StatusNotFound},
			{"running", service.ErrScheduledQuestionTestRunning, http.StatusConflict},
		} {
			t.Run(fmt.Sprintf("deadline=%t/%s", deadlineExceeded, result.name), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				cancel()
				if deadlineExceeded {
					var deadlineCancel context.CancelFunc
					ctx, deadlineCancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
					defer deadlineCancel()
				}
				stub := &scheduledQuestionTestHandlerStub{err: result.err}
				if result.err == nil {
					stub.accepted = &service.ScheduledQuestionTestAccepted{Accepted: true, PlanID: 12}
				}
				r := httptest.NewRequest(http.MethodPost, "/plans/12/run-now", nil).WithContext(ctx)
				recorder := httptest.NewRecorder()
				newScheduledQuestionTestHandlerRouter(stub).ServeHTTP(recorder, r)
				require.Same(t, ctx, stub.ctx)
				require.Equal(t, result.status, recorder.Code, recorder.Body.String())
				if result.err == nil {
					require.JSONEq(t, `{"code":0,"message":"accepted","data":{"accepted":true,"plan_id":12}}`, recorder.Body.String())
				} else {
					require.NotContains(t, recorder.Body.String(), `"data":`)
				}
			})
		}
	}
}

func TestScheduledQuestionTestHandlerErrors(t *testing.T) {
	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/plans"}, {http.MethodGet, "/plans/12"},
		{http.MethodPost, "/plans"}, {http.MethodPut, "/plans/12"},
		{http.MethodDelete, "/plans/12"}, {http.MethodGet, "/plans/12/runs"},
		{http.MethodPost, "/plans/12/run-now"},
	} {
		for _, failure := range []struct {
			name    string
			err     error
			status  int
			message string
		}{
			{"validation", fmt.Errorf("validate: %w", &model.ValidationError{Field: "min_pass_ratio", Message: "must be between 0 and 100"}), http.StatusBadRequest, "min_pass_ratio"},
			{"not found", service.ErrScheduledQuestionTestNotFound, http.StatusNotFound, "not found"},
			{"conflict", service.ErrScheduledQuestionTestRunning, http.StatusConflict, "already running"},
			{"internal", errors.New("database unavailable"), http.StatusInternalServerError, "internal error"},
		} {
			t.Run(request.method+request.path+"/"+failure.name, func(t *testing.T) {
				stub := &scheduledQuestionTestHandlerStub{err: failure.err}
				recorder := requestScheduledQuestionTest(newScheduledQuestionTestHandlerRouter(stub), request.method, request.path, `{}`)
				require.Equal(t, failure.status, recorder.Code, recorder.Body.String())
				require.Contains(t, recorder.Body.String(), failure.message)
				require.NotContains(t, recorder.Body.String(), `"data":`)
				if failure.status == http.StatusInternalServerError {
					require.NotContains(t, recorder.Body.String(), "database unavailable")
				}
			})
		}
	}
}

func TestScheduledQuestionTestHandlerNilDetail(t *testing.T) {
	recorder := requestScheduledQuestionTest(newScheduledQuestionTestHandlerRouter(&scheduledQuestionTestHandlerStub{}), http.MethodGet, "/plans/12", "")
	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestScheduledQuestionTestHandlerPropagatesRequestContext(t *testing.T) {
	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/plans"}, {http.MethodGet, "/plans/12"},
		{http.MethodPost, "/plans"}, {http.MethodPut, "/plans/12"},
		{http.MethodDelete, "/plans/12"}, {http.MethodGet, "/plans/12/runs"},
		{http.MethodPost, "/plans/12/run-now"},
	} {
		t.Run(request.method+request.path, func(t *testing.T) {
			stub := &scheduledQuestionTestHandlerStub{
				plan:     &service.ScheduledQuestionTestPlan{ID: 12},
				accepted: &service.ScheduledQuestionTestAccepted{Accepted: true, PlanID: 12},
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cancel()
			r := httptest.NewRequest(request.method, request.path, strings.NewReader(`{}`)).WithContext(ctx)
			r.Header.Set("Content-Type", "application/json")
			newScheduledQuestionTestHandlerRouter(stub).ServeHTTP(httptest.NewRecorder(), r)
			require.Same(t, ctx, stub.ctx)
			require.ErrorIs(t, stub.ctx.Err(), context.Canceled)
		})
	}
}
