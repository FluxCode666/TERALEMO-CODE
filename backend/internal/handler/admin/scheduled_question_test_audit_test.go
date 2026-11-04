//go:build unit

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type scheduledQuestionAuditRepo struct {
	service.AuditLogRepository
	mu   sync.Mutex
	logs []*service.AuditLog
}

func (r *scheduledQuestionAuditRepo) BatchInsert(_ context.Context, entries []*service.AuditLog) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, entry := range entries {
		copy := *entry
		copy.ID = int64(len(r.logs) + 1)
		r.logs = append(r.logs, &copy)
	}
	return int64(len(entries)), nil
}

func (r *scheduledQuestionAuditRepo) GetByID(_ context.Context, id int64) (*service.AuditLog, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id < 1 || id > int64(len(r.logs)) {
		return nil, service.ErrAuditLogNotFound
	}
	return r.logs[id-1], nil
}

func (r *scheduledQuestionAuditRepo) List(context.Context, *service.AuditLogFilter) (*service.AuditLogList, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return &service.AuditLogList{Logs: append([]*service.AuditLog{}, r.logs...), Total: len(r.logs), Page: 1, PageSize: 20}, nil
}

func TestScheduledQuestionRequestsAuditCredentialsAreRedactedAtStorageAndRead(t *testing.T) {
	const token = "AUDIT-canary-private-webhook-token"
	const secret = "AUDIT-canary-private-signing-secret"
	webhook := "https://open.feishu.cn/open-apis/bot/v2/hook/" + token
	repo := &scheduledQuestionAuditRepo{}
	audit := service.NewAuditLogService(repo, nil)
	audit.Start()
	t.Cleanup(audit.Stop)
	stub := &scheduledQuestionTestHandlerStub{plan: &service.ScheduledQuestionTestPlan{ID: 12}}
	planHandler := &ScheduledQuestionTestHandler{service: stub}
	auditHandler := NewAuditLogHandler(audit, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	admin := router.Group("/api/v1/admin")
	admin.Use(gin.HandlerFunc(middleware.NewAuditLogMiddleware(audit)))
	admin.POST("/scheduled-question-tests", planHandler.Create)
	admin.PUT("/scheduled-question-tests/:id", planHandler.Update)
	admin.POST("/scheduled-question-tests/feishu-test", planHandler.TestFeishu)
	admin.GET("/audit-logs/:id", auditHandler.Get)
	admin.GET("/audit-logs", auditHandler.List)
	for _, request := range []struct{ method, path, urlKey, secretKey string }{
		{http.MethodPost, "/scheduled-question-tests", "feishu_webhook_url", "feishu_secret"},
		{http.MethodPut, "/scheduled-question-tests/12", "feishu_webhook_url", "feishu_secret"},
		{http.MethodPost, "/scheduled-question-tests/feishu-test", "webhook_url", "secret"},
	} {
		body, err := json.Marshal(map[string]any{"name": "visible plan", request.urlKey: webhook, request.secretKey: secret})
		require.NoError(t, err)
		req := httptest.NewRequest(request.method, "/api/v1/admin"+request.path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		if request.urlKey == "webhook_url" {
			require.Equal(t, webhook, stub.feishuInput.WebhookURL, "audit capture must restore the handler's raw body")
			require.NotNil(t, stub.feishuInput.Secret)
			require.Equal(t, secret, *stub.feishuInput.Secret)
		} else {
			require.NotNil(t, stub.input.FeishuWebhookURL)
			require.Equal(t, webhook, *stub.input.FeishuWebhookURL)
			require.NotNil(t, stub.input.FeishuSecret)
			require.Equal(t, secret, *stub.input.FeishuSecret)
		}
	}
	audit.Stop()
	require.Len(t, repo.logs, 3)
	for _, entry := range repo.logs {
		require.NotContains(t, entry.RequestBody, token)
		require.NotContains(t, entry.RequestBody, secret)
		require.NotContains(t, entry.RequestBody, webhook)
		require.Contains(t, entry.RequestBody, "visible plan")
		require.Contains(t, entry.RequestBody, "***")
	}

	// The database can still contain a record written before the new key table.
	legacy := &service.AuditLog{ID: 4, CreatedAt: time.Now(), RequestBody: fmt.Sprintf(`{"webhook_url":%q,"secret":%q}`, webhook, secret)}
	repo.logs = append(repo.logs, legacy)
	for _, path := range []string{"/audit-logs/1", "/audit-logs/2", "/audit-logs/3", "/audit-logs/4", "/audit-logs"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin"+path, nil))
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Contains(t, recorder.Body.String(), `request_body`)
		require.NotContains(t, recorder.Body.String(), token)
		require.NotContains(t, recorder.Body.String(), secret)
		require.NotContains(t, recorder.Body.String(), webhook)
	}
	require.Contains(t, legacy.RequestBody, token, "read projection must not mutate the stored record")
}
