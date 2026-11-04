//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type crsSchedulingAccountRepo struct {
	*crsLongContextAccountRepo
	updateErr error
	clearErr  error
	cleared   []int64
	updated   int
	actual    map[int64]bool
}

func (r *crsSchedulingAccountRepo) Update(ctx context.Context, account *Account) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.updated++
	if r.actual == nil {
		r.actual = make(map[int64]bool)
	}
	r.actual[account.ID] = account.Schedulable && account.Status != StatusError
	return r.crsLongContextAccountRepo.Update(ctx, account)
}

func (r *crsSchedulingAccountRepo) ClearScheduledQuestionAutoPause(_ context.Context, id int64) error {
	r.cleared = append(r.cleared, id)
	return r.clearErr
}

func TestCRSSyncSchedulableChangesReleaseOwnershipForEveryExistingAccountKind(t *testing.T) {
	for _, source := range []struct {
		collection  string
		platform    string
		accountType string
		credentials map[string]any
	}{
		{"claudeAccounts", PlatformAnthropic, AccountTypeOAuth, map[string]any{"access_token": "test-access"}},
		{"claudeConsoleAccounts", PlatformAnthropic, AccountTypeAPIKey, map[string]any{"api_key": "test-key"}},
		{"openaiOAuthAccounts", PlatformOpenAI, AccountTypeOAuth, map[string]any{"access_token": "test-access"}},
		{"openaiResponsesAccounts", PlatformOpenAI, AccountTypeAPIKey, map[string]any{"api_key": "test-key"}},
		{"geminiOAuthAccounts", PlatformGemini, AccountTypeOAuth, map[string]any{"refresh_token": "test-refresh"}},
		{"geminiApiKeyAccounts", PlatformGemini, AccountTypeAPIKey, map[string]any{"api_key": "test-key"}},
	} {
		for _, scenario := range []struct {
			name       string
			previous   bool
			next       bool
			status     string
			failUpdate bool
			failClear  bool
			create     bool
		}{
			{name: "resume", previous: false, next: true},
			{name: "pause", previous: true, next: false},
			{name: "unchanged paused", previous: false, next: false},
			{name: "unchanged schedulable", previous: true, next: true},
			{name: "update failure", previous: false, next: true, failUpdate: true},
			{name: "clear failure", previous: true, next: false, failClear: true},
			{name: "status error forces unschedulable", previous: true, next: true, status: "error"},
			{name: "status error stays unschedulable", previous: false, next: true, status: "error"},
			{name: "status error write failure", previous: true, next: true, status: "error", failUpdate: true},
			{name: "status error clear failure", previous: true, next: true, status: "error", failClear: true},
			{name: "new account", next: true, create: true},
		} {
			t.Run(source.collection+"/"+scenario.name, func(t *testing.T) {
				var existing []*Account
				if !scenario.create {
					existing = []*Account{{ID: 41, Platform: source.platform, Type: source.accountType, Status: StatusActive,
						Schedulable: scenario.previous, Extra: map[string]any{"crs_account_id": "crs-scheduling-1"}}}
				}
				repo := &crsSchedulingAccountRepo{crsLongContextAccountRepo: newCRSLongContextAccountRepo(existing...)}
				if scenario.failUpdate {
					repo.updateErr = errors.New("injected update failure")
				}
				if scenario.failClear {
					repo.clearErr = errors.New("injected ownership clear failure")
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if request.URL.Path == "/web/auth/login" {
						_, _ = w.Write([]byte(`{"success":true,"token":"test-admin-token"}`))
						return
					}
					require.Equal(t, "/admin/sync/export-accounts", request.URL.Path)
					require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{
						source.collection: []any{map[string]any{
							"id": "crs-scheduling-1", "name": "Scheduling account", "kind": source.collection,
							"isActive": true, "schedulable": scenario.next, "status": scenario.status, "credentials": source.credentials,
						}},
					}}))
				}))
				t.Cleanup(server.Close)
				cfg := &config.Config{}
				cfg.Security.URLAllowlist.AllowInsecureHTTP = true
				svc := NewCRSSyncService(repo, nil, nil, nil, nil, cfg)
				result, err := svc.SyncFromCRS(context.Background(), SyncFromCRSInput{BaseURL: server.URL, Username: "admin", Password: "password"})
				require.NoError(t, err)
				require.Len(t, result.Items, 1)
				if scenario.failUpdate || scenario.failClear {
					require.Equal(t, 1, result.Failed)
					require.Zero(t, result.Updated)
					require.Equal(t, "failed", result.Items[0].Action)
					if scenario.failClear {
						require.Contains(t, result.Items[0].Error, "injected ownership clear failure")
					}
				} else if scenario.create {
					require.Equal(t, 1, result.Created)
				} else {
					require.Equal(t, 1, result.Updated)
					require.Equal(t, "updated", result.Items[0].Action)
				}
				actualSchedulable := scenario.next
				if scenario.status == "error" {
					actualSchedulable = false
				}
				if scenario.previous != actualSchedulable && !scenario.failUpdate && !scenario.create {
					require.Equal(t, []int64{41}, repo.cleared)
				} else {
					require.Empty(t, repo.cleared)
				}
				if !scenario.create && !scenario.failUpdate {
					require.Equal(t, actualSchedulable, repo.actual[41])
				}
			})
		}
	}
}
