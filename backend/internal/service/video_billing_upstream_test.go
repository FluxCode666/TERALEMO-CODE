//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

// Mirror the production repository's atomic first-writer-wins legacy binding.
func (f *videoTaskBillingFake) BindUpstreamOrigin(_ context.Context, id, origin string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := f.tasks[id]
	if v == nil {
		return "", ErrVideoReservationNotFound
	}
	var snapshot VideoBillingSnapshot
	if err := json.Unmarshal(v.Snapshot, &snapshot); err != nil {
		return "", err
	}
	if snapshot.UpstreamOrigin == "" {
		snapshot.UpstreamOrigin = origin
		var err error
		v.Snapshot, err = json.Marshal(snapshot)
		if err != nil {
			return "", err
		}
	}
	return snapshot.UpstreamOrigin, nil
}

type videoOriginUpstream struct {
	requests []*http.Request
	respond  func(*http.Request) (*http.Response, error)
}

func (u *videoOriginUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.requests = append(u.requests, req)
	return u.respond(req)
}

func (u *videoOriginUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

type videoOriginProtocolCase struct {
	name, protocol, suffix, taskID, pending string
	endpoint                                GrokMediaEndpoint
}

func videoOriginProtocolCases() []videoOriginProtocolCase {
	return []videoOriginProtocolCase{
		{"seedance", VideoProtocolSeedance, "/api/v3", "seedance:task-test", `{"id":"task-test","status":"running"}`, SeedanceEndpointCreate},
		{"gemini", VideoProtocolGemini, "/v1beta", "gemini:models/veo/operations/task-test", `{"name":"models/veo/operations/task-test","done":false}`, GeminiVideoEndpointCreate},
		{"minimax-v1", VideoProtocolMiniMax, "/v1", "minimax:task-test", `{"task_id":"task-test","status":"Processing","base_resp":{"status_code":0}}`, MiniMaxVideoEndpointCreate},
		{"minimax-v2", VideoProtocolMiniMax, "/minimax/v2", "minimax:task-test", `{"task":{"id":"task-test","status":"running"}}`, MiniMaxVideoEndpointCreate},
		{"grok", "grok", "/v1", "task-test", `{"request_id":"task-test","status":"pending"}`, GrokMediaEndpointVideosGenerations},
	}
}

func videoOriginFixture(t *testing.T, tc videoOriginProtocolCase) (*OpenAIGatewayService, *videoTaskBillingFake, *VideoBillingReservation, *Account) {
	t.Helper()
	svc, repo, input := videoTaskServiceFixture(t)
	input.Protocol, input.Endpoint = tc.protocol, tc.endpoint
	input.Account.Platform = PlatformVideo
	if tc.protocol == "grok" {
		input.Account.Platform = PlatformGrok
	}
	input.Account.Credentials["video_protocol"] = tc.protocol
	input.Account.Credentials["base_url"] = "https://trusted.example" + tc.suffix
	v, err := svc.ReserveVideoTask(context.Background(), input)
	require.NoError(t, err)
	require.NoError(t, svc.AcceptVideoTask(context.Background(), v, tc.taskID))
	svc.accountRepo = &videoBoundAccountFake{account: input.Account}
	return svc, repo, v, input.Account
}

func captureVideoOriginLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	previous := slog.Default()
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &output
}

func TestVideoUpstreamOriginNormalization(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{" HTTPS://Relay.Example.:443/private-relay-secret?api_key=query-secret#fragment-secret ", "https://relay.example"},
		{"http://Relay.Example:080/private", "http://relay.example"},
		{"https://Relay.Example:8443/private", "https://relay.example:8443"},
		{"https://[2001:0DB8:0:0::1]:443/private?key=secret", "https://[2001:db8::1]"},
		{"http://[2001:db8::2]:8080/private", "http://[2001:db8::2]:8080"},
		{"http://192.168.1.10:8080/relay", "http://192.168.1.10:8080"}, // Private relays remain supported.
	} {
		t.Run(tc.input, func(t *testing.T) {
			origin, err := NormalizeVideoUpstreamOrigin(tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.want, origin)
			parsed, err := url.Parse(origin)
			require.NoError(t, err)
			require.Nil(t, parsed.User)
			require.Empty(t, parsed.Path)
			require.Empty(t, parsed.RawQuery)
			require.Empty(t, parsed.Fragment)
		})
	}
	for _, input := range []string{
		"https://user:password@relay.example/private?key=secret",
		"https://user@relay.example", "https://@relay.example",
		"https://relay.example:0", "https://relay.example:65536", "https://relay.example:wrong",
		"file:///tmp/secret", "//relay.example/path", "https:///path", "https://relay.example/%zz",
	} {
		t.Run("reject_"+input, func(t *testing.T) {
			origin, err := NormalizeVideoUpstreamOrigin(input)
			require.Error(t, err)
			require.Empty(t, origin)
			for _, secret := range []string{"password", "private", "key=secret", "/tmp/secret"} {
				require.NotContains(t, err.Error(), secret)
			}
		})
	}
}

func TestVideoUpstreamOriginFrozenOnSubmission(t *testing.T) {
	for _, tc := range videoOriginProtocolCases() {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, input := videoTaskServiceFixture(t)
			input.Protocol, input.Endpoint = tc.protocol, tc.endpoint
			input.Account.Platform = PlatformVideo
			if tc.protocol == "grok" {
				input.Account.Platform = PlatformGrok
			}
			input.Account.Credentials["video_protocol"] = tc.protocol
			input.Account.Credentials["base_url"] = "https://Trusted.Example:443" + tc.suffix + "/relay-path-secret?key=query-secret#fragment-secret"
			v, err := svc.ReserveVideoTask(context.Background(), input)
			require.NoError(t, err)
			input.Account.Credentials["base_url"] = "https://changed.example/new-path"
			require.NoError(t, svc.AcceptVideoTask(context.Background(), v, tc.taskID))
			stored, err := repo.Get(context.Background(), v.ID)
			require.NoError(t, err)
			var snapshot VideoBillingSnapshot
			require.NoError(t, json.Unmarshal(stored.Snapshot, &snapshot))
			require.Equal(t, "https://trusted.example", snapshot.UpstreamOrigin)
			for _, secret := range []string{"relay-path-secret", "query-secret", "fragment-secret", input.Account.GetCredential("api_key")} {
				require.NotContains(t, string(stored.Snapshot), secret)
			}
			require.InDelta(t, .6, stored.HoldAmount, 1e-12)
		})
	}
}

func TestVideoUpstreamOriginChangedNeverSendsCredentialsAndRetainsHold(t *testing.T) {
	for _, tc := range videoOriginProtocolCases() {
		for _, changedOrigin := range []string{"https://attacker.example", "http://trusted.example", "https://trusted.example:8443"} {
			t.Run(tc.name+"/"+changedOrigin, func(t *testing.T) {
				logs := captureVideoOriginLogs(t)
				svc, repo, v, account := videoOriginFixture(t, tc)
				account.Credentials["base_url"] = changedOrigin + tc.suffix
				upstream := &videoOriginUpstream{respond: func(*http.Request) (*http.Response, error) {
					t.Fatal("changed origin must be rejected before any upstream request")
					return nil, errors.New("unexpected request")
				}}
				svc.httpUpstream = upstream
				result, err := svc.PollReservedVideoTask(context.Background(), v)
				require.ErrorIs(t, err, ErrVideoUpstreamOriginChanged)
				require.Nil(t, result)
				require.Empty(t, upstream.requests)
				stored, err := repo.Get(context.Background(), v.ID)
				require.NoError(t, err)
				require.Equal(t, VideoReservationManual, stored.State)
				require.Equal(t, v.HoldAmount, stored.HoldAmount)
				require.Empty(t, repo.releases)
				require.Empty(t, repo.settlements)
				require.Equal(t, []string{"上游地址已变更"}, repo.manual)
				require.Contains(t, logs.String(), "video_billing.manual_review_required")
				require.Contains(t, logs.String(), "上游地址已变更")
				require.NotContains(t, logs.String(), account.GetCredential("api_key"))
			})
		}
	}
}

func TestVideoUpstreamOriginLegacyBindsBeforePollingThenRejectsChanges(t *testing.T) {
	for _, tc := range videoOriginProtocolCases() {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureVideoOriginLogs(t)
			svc, repo, v, account := videoOriginFixture(t, tc)
			var snapshot VideoBillingSnapshot
			require.NoError(t, json.Unmarshal(v.Snapshot, &snapshot))
			snapshot.UpstreamOrigin = ""
			var err error
			v.Snapshot, err = json.Marshal(snapshot)
			require.NoError(t, err)
			repo.tasks[v.ID].Snapshot = append(json.RawMessage(nil), v.Snapshot...)
			upstream := &videoOriginUpstream{respond: func(req *http.Request) (*http.Response, error) {
				stored, err := repo.Get(req.Context(), v.ID)
				require.NoError(t, err)
				var beforeRequest VideoBillingSnapshot
				require.NoError(t, json.Unmarshal(stored.Snapshot, &beforeRequest))
				require.Equal(t, "https://trusted.example", beforeRequest.UpstreamOrigin, "must freeze before sending credentials")
				return grokMediaContentStatusResponse(tc.pending), nil
			}}
			svc.httpUpstream = upstream
			result, err := svc.PollReservedVideoTask(context.Background(), v)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.requests, 1)
			require.Empty(t, repo.manual)
			require.Contains(t, logs.String(), "video_billing.legacy_upstream_origin_bound")
			// Retain the stale in-memory snapshot to exercise first-writer-wins.
			account.Credentials["base_url"] = "https://attacker.example" + tc.suffix
			result, err = svc.PollReservedVideoTask(context.Background(), v)
			require.ErrorIs(t, err, ErrVideoUpstreamOriginChanged)
			require.Nil(t, result)
			require.Len(t, upstream.requests, 1)
			require.Empty(t, repo.releases)
			require.Empty(t, repo.settlements)
		})
	}
}

func TestVideoUpstreamOriginLegacyBindingFirstWriterWins(t *testing.T) {
	svc, repo, input := videoTaskServiceFixture(t)
	v := acceptVideoFixture(t, svc, input)
	var snapshot VideoBillingSnapshot
	require.NoError(t, json.Unmarshal(v.Snapshot, &snapshot))
	snapshot.UpstreamOrigin = ""
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	repo.tasks[v.ID].Snapshot = encoded
	const count = 24
	results := make([]string, count)
	errors := make([]error, count)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errors[i] = repo.BindUpstreamOrigin(context.Background(), v.ID, fmt.Sprintf("https://relay-%d.example", i))
		}(i)
	}
	wg.Wait()
	stored, err := repo.Get(context.Background(), v.ID)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(stored.Snapshot, &snapshot))
	require.NotEmpty(t, snapshot.UpstreamOrigin)
	for i := range results {
		require.NoError(t, errors[i])
		require.Equal(t, snapshot.UpstreamOrigin, results[i])
	}
}

func TestVideoUpstreamOriginEquivalentAddressesKeepPolling(t *testing.T) {
	for _, tc := range videoOriginProtocolCases() {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, v, account := videoOriginFixture(t, tc)
			account.Credentials["base_url"] = "https://TRUSTED.EXAMPLE:443" + tc.suffix + "/"
			upstream := &videoOriginUpstream{respond: func(*http.Request) (*http.Response, error) {
				return grokMediaContentStatusResponse(tc.pending), nil
			}}
			svc.httpUpstream = upstream
			result, err := svc.PollReservedVideoTask(context.Background(), v)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.requests, 1)
			require.Empty(t, repo.manual)
			require.Empty(t, repo.releases)
		})
	}
}

func TestVideoUpstreamOriginInvalidAddressRejectsSubmissionAndPolling(t *testing.T) {
	for _, tc := range videoOriginProtocolCases() {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, input := videoTaskServiceFixture(t)
			input.Protocol, input.Endpoint = tc.protocol, tc.endpoint
			input.Account.Platform = PlatformVideo
			if tc.protocol == "grok" {
				input.Account.Platform = PlatformGrok
			}
			input.Account.Credentials["base_url"] = "https://user:password@trusted.example/private?key=secret"
			v, err := svc.ReserveVideoTask(context.Background(), input)
			require.Error(t, err)
			require.Nil(t, v)
			require.Empty(t, repo.tasks)

			svc, repo, v, account := videoOriginFixture(t, tc)
			account.Credentials["base_url"] = "https://user:password@trusted.example" + tc.suffix
			upstream := &videoOriginUpstream{respond: func(*http.Request) (*http.Response, error) {
				t.Fatal("userinfo must be rejected before sending credentials")
				return nil, errors.New("unexpected request")
			}}
			svc.httpUpstream = upstream
			_, err = svc.PollReservedVideoTask(context.Background(), v)
			require.ErrorIs(t, err, ErrVideoUpstreamOriginChanged)
			require.Empty(t, upstream.requests)
			require.Equal(t, VideoReservationManual, repo.tasks[v.ID].State)
			require.Equal(t, []string{"上游地址无效或含用户凭据"}, repo.manual)
			require.Empty(t, repo.releases)
			require.Empty(t, repo.settlements)
		})
	}
}

func TestVideoUpstreamOriginLegacyWithoutAtomicBinderStopsPolling(t *testing.T) {
	svc, repo, input := videoTaskServiceFixture(t)
	v := acceptVideoFixture(t, svc, input)
	var snapshot VideoBillingSnapshot
	require.NoError(t, json.Unmarshal(v.Snapshot, &snapshot))
	snapshot.UpstreamOrigin = ""
	var err error
	v.Snapshot, err = json.Marshal(snapshot)
	require.NoError(t, err)
	// The wrapper deliberately exposes only the original interface, hiding the
	// optional binder that the underlying fake implements.
	svc.SetVideoBillingReservationRepository(struct {
		VideoBillingReservationRepository
	}{repo})
	svc.accountRepo = &videoBoundAccountFake{account: input.Account}
	upstream := &videoOriginUpstream{respond: func(*http.Request) (*http.Response, error) {
		t.Fatal("legacy holds without an atomic origin binder must fail closed")
		return nil, errors.New("unexpected request")
	}}
	svc.httpUpstream = upstream
	_, err = svc.PollReservedVideoTask(context.Background(), v)
	require.ErrorIs(t, err, ErrVideoUpstreamOriginChanged)
	require.Empty(t, upstream.requests)
	require.Equal(t, VideoReservationManual, repo.tasks[v.ID].State)
	require.Equal(t, v.HoldAmount, repo.tasks[v.ID].HoldAmount)
	require.Empty(t, repo.releases)
	require.Empty(t, repo.settlements)
}

func TestVideoUpstreamOriginPollingDisablesRedirectsForEveryProtocol(t *testing.T) {
	for _, tc := range videoOriginProtocolCases() {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, v, account := videoOriginFixture(t, tc)
			upstream := &videoOriginUpstream{respond: func(req *http.Request) (*http.Response, error) {
				require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))
				require.Equal(t, http.MethodGet, req.Method)
				require.Equal(t, "trusted.example", req.URL.Host)
				if tc.protocol == VideoProtocolGemini {
					require.Equal(t, account.GetCredential("api_key"), req.Header.Get("x-goog-api-key"))
				} else {
					require.Equal(t, "Bearer "+account.GetCredential("api_key"), req.Header.Get("Authorization"))
				}
				response := grokMediaContentStatusResponse(`{"error":{"message":"redirect"}}`)
				response.StatusCode = http.StatusTemporaryRedirect
				response.Header.Set("Location", "https://attacker.example/stolen")
				return response, nil
			}}
			svc.httpUpstream = upstream
			_, _ = svc.PollReservedVideoTask(context.Background(), v)
			require.Len(t, upstream.requests, 1)
			require.Empty(t, repo.releases)
			require.Empty(t, repo.settlements)
		})
	}
}

func TestVideoUpstreamOriginReconcilerRedactsURLError(t *testing.T) {
	for _, escaped := range []bool{false, true} {
		t.Run(fmt.Sprint(escaped), func(t *testing.T) {
			logs := captureVideoOriginLogs(t)
			svc, repo, input := videoTaskServiceFixture(t)
			v := acceptVideoFixture(t, svc, input)
			sensitiveURL := "https://userinfo-user:userinfo-password@relay.example/signed-path-secret?api_key=query-key-secret&sig=query-signature-secret"
			if escaped {
				sensitiveURL = strings.ReplaceAll(sensitiveURL, "/", `\/`)
			}
			svc.httpUpstream = &videoOriginUpstream{respond: func(*http.Request) (*http.Response, error) {
				return nil, &url.Error{Op: "Get", URL: sensitiveURL, Err: errors.New("transport interrupted; Bearer bearer-secret; api_key=extra-key-secret")}
			}}
			svc.accountRepo = &videoBoundAccountFake{account: input.Account}
			require.NoError(t, svc.ReconcileVideoBillingOnce(context.Background(), "redaction-test"))
			require.Contains(t, logs.String(), "video_billing.reconcile_retry")
			for _, secret := range []string{"userinfo-user", "userinfo-password", "signed-path-secret", "query-key-secret", "query-signature-secret", "bearer-secret", "extra-key-secret"} {
				require.NotContains(t, logs.String(), secret)
			}
			require.Equal(t, []string{"upstream observation or settlement failed"}, repo.rescheduleErrors)
			require.Empty(t, repo.releases)
			require.Empty(t, repo.settlements)
			stored, err := repo.Get(context.Background(), v.ID)
			require.NoError(t, err)
			require.Equal(t, VideoReservationAccepted, stored.State)
		})
	}
}

func TestVideoUpstreamOriginUserinfoRejectedBeforeAccountWrites(t *testing.T) {
	for _, protocol := range []string{VideoProtocolSeedance, VideoProtocolGemini, VideoProtocolMiniMax} {
		for _, writePath := range []string{"validate", "create-helper", "create", "update", "bulk"} {
			t.Run(protocol+"/"+writePath, func(t *testing.T) {
				account := &Account{ID: 42, Name: "video", Platform: PlatformVideo, Type: AccountTypeAPIKey,
					Credentials: map[string]any{"api_key": "account-secret", "video_protocol": protocol, "base_url": "https://trusted.example"}}
				badBase := "https://userinfo-user:userinfo-password@relay.example/private-path?key=query-secret"
				repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{account.ID: account}, getByIDsAccounts: []*Account{account}}
				svc := &adminServiceImpl{accountRepo: repo}
				var err error
				switch writePath {
				case "validate":
					account.Credentials["base_url"] = badBase
					err = ValidateVideoAccount(account)
				case "create-helper", "create":
					account.Credentials["base_url"] = badBase
					input := &CreateAccountInput{Name: account.Name, Platform: account.Platform, Type: account.Type, Credentials: account.Credentials, SkipDefaultGroupBind: true}
					if writePath == "create-helper" {
						_, err = buildAccountForCreate(input, map[string]any{})
					} else {
						_, err = svc.CreateAccount(context.Background(), input)
					}
				case "update":
					_, err = svc.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{Credentials: map[string]any{"base_url": badBase}})
				case "bulk":
					_, err = svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{account.ID}, Credentials: map[string]any{"base_url": badBase}})
				}
				require.ErrorContains(t, err, "userinfo")
				for _, secret := range []string{"userinfo-user", "userinfo-password", "private-path", "query-secret"} {
					require.NotContains(t, err.Error(), secret)
				}
				require.Nil(t, repo.createAccount)
				require.Empty(t, repo.updatedAccounts)
				require.Zero(t, repo.bulkUpdateCalls)
			})
		}
	}
}

type videoOriginActiveAccountRepo struct {
	*accountRepoStubForBulkUpdate
	active int64
	ids    []int64
}

func (r *videoOriginActiveAccountRepo) CountActiveVideoBillingReservations(_ context.Context, accountID int64) (int64, error) {
	r.ids = append(r.ids, accountID)
	return r.active, nil
}

func TestVideoUpstreamOriginAccountChangeWithHoldWarnsWithoutURL(t *testing.T) {
	for _, writePath := range []string{"update", "bulk"} {
		t.Run(writePath, func(t *testing.T) {
			logs := captureVideoOriginLogs(t)
			account := &Account{ID: 42, Name: "video", Platform: PlatformVideo, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "account-secret", "video_protocol": VideoProtocolSeedance, "base_url": "https://trusted.example/old-secret-path"}}
			repo := &videoOriginActiveAccountRepo{accountRepoStubForBulkUpdate: &accountRepoStubForBulkUpdate{
				getByIDAccounts: map[int64]*Account{account.ID: account}, getByIDsAccounts: []*Account{account}}, active: 2}
			svc := &adminServiceImpl{accountRepo: repo}
			credentials := map[string]any{"base_url": "https://changed.example/new-secret-path?key=query-secret"}
			if writePath == "update" {
				_, err := svc.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{Credentials: credentials})
				require.NoError(t, err)
			} else {
				_, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{account.ID}, Credentials: credentials})
				require.NoError(t, err)
			}
			require.Equal(t, []int64{account.ID}, repo.ids)
			require.Contains(t, logs.String(), "video_billing.account_upstream_changed_with_active_holds")
			require.Contains(t, logs.String(), `"active_reservations":2`)
			for _, secret := range []string{"account-secret", "old-secret-path", "new-secret-path", "query-secret", "trusted.example", "changed.example"} {
				require.NotContains(t, logs.String(), secret)
			}
		})
	}
}
