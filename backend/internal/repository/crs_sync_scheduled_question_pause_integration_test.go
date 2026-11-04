//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCRSSyncStatusErrorClearsScheduledQuestionOwnershipIntegration(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := newAccountRepositoryWithSQL(client, integrationDB, nil)
	crsID := t.Name()
	account := &service.Account{
		Name: crsID, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "test-api-key"}, Extra: map[string]any{"crs_account_id": crsID},
	}
	require.NoError(t, repo.Create(ctx, account))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id = $1", account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", account.ID)
	})
	_, plan := newScheduledQuestionTestIntegrationPlan(t)
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO scheduled_question_test_account_states
		(plan_id, account_id, state, auto_paused, auto_paused_at) VALUES ($1, $2, 'alert', true, NOW())`, plan.ID, account.ID)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/web/auth/login" {
			_, _ = writer.Write([]byte(`{"success":true,"token":"test-token"}`))
			return
		}
		require.Equal(t, "/admin/sync/export-accounts", request.URL.Path)
		require.NoError(t, json.NewEncoder(writer).Encode(map[string]any{"success": true, "data": map[string]any{
			"openaiResponsesAccounts": []any{map[string]any{
				"id": crsID, "name": crsID, "isActive": true, "status": "error", "schedulable": true,
				"credentials": map[string]any{"api_key": "test-api-key"},
			}},
		}}))
	}))
	defer server.Close()
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	svc := service.NewCRSSyncService(repo, nil, nil, nil, nil, cfg)
	result, err := svc.SyncFromCRS(ctx, service.SyncFromCRSInput{BaseURL: server.URL, Username: "admin", Password: "password"})
	require.NoError(t, err)
	require.Equal(t, 1, result.Updated)
	require.Zero(t, result.Failed)
	var schedulable, owned bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT schedulable FROM accounts WHERE id = $1", account.ID).Scan(&schedulable))
	require.False(t, schedulable, "repository status=error must override the requested true")
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT auto_paused FROM scheduled_question_test_account_states
		WHERE plan_id = $1 AND account_id = $2`, plan.ID, account.ID).Scan(&owned))
	require.False(t, owned, "the actual scheduling change must release the old plan's ownership")
}
