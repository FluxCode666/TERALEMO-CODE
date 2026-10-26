package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsCloudflare1010Block(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		// JSON bodies need Cloudflare corroboration (cloudflare_error, error_name
		// or an error_code sibling): a bare 1010 numeric code is reused by relays
		// and providers for their own errors, and exempting a genuine ban would
		// keep a dead account scheduled forever.
		{"nested bare numeric code", `{"error":{"code":1010,"message":"Access denied"}}`, false},
		{"nested bare string code", `{"error":{"code":"1010"}}`, false},
		{"top bare numeric code", `{"code":1010}`, false},
		{"top bare string code", `{"code":"1010"}`, false},
		{"cloudflare flagged numeric code", `{"cloudflare_error":true,"code":1010}`, true},
		{"cloudflare flagged nested code", `{"errors":[{"cloudflare_error":true,"code":"1010"}]}`, true},
		{"cloudflare error code field", `{"cloudflare_error":true,"error_code":1010}`, true},
		{"browser signature name", `{"error_name":"browser_signature_banned"}`, true},
		{"text error code", "error code: 1010", true},
		{"text mixed case", "ErRoR CoDe: 1010\n", true},
		{"text whitespace", "ERROR\tCODE :\t1010", true},
		{"error heading", "<h1>Error 1010</h1>", true},
		{"cloudflare browser check", "CLOUDFLARE browser integrity check failed (1010)", true},
		{"json text message", `{"error":{"message":"Cloudflare blocked the browser with code 1010"}}`, true},
		{"empty", "", false},
		{"normal forbidden", `{"error":{"code":"access_denied","message":"subscription required"}}`, false},
		{"different numeric code", `{"error":{"code":1020}}`, false},
		{"different string code", `{"code":"10100"}`, false},
		{"code prefix", "Cloudflare error code: 10100", false},
		{"code suffix", "Cloudflare error code: 11010", false},
		{"decimal code", "Cloudflare Error 1010.5", false},
		{"negative code", "Cloudflare Error -1010", false},
		{"exponent code", "Cloudflare Error 1010e1", false},
		{"unrelated number", "Forbidden request 1010", false},
		{"unrelated json field", `{"request_id":1010,"error":{"code":403}}`, false},
		{"cloudflare other block", "Cloudflare Error 1020: access denied", false},
		{"malformed json", `{"code":1010`, false},
		// A provider using 1010 as its own error code must still be penalized.
		{"provider ban with bare 1010", `{"error":{"code":1010,"message":"account banned"}}`, false},
		{"message mentions 1010", `{"message":"error code: 1010"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isCloudflare1010Block([]byte(tt.body)))
		})
	}
}

func TestTypeSafeAccountTestCloudflare1010KeepsAccountState(t *testing.T) {
	for _, body := range []string{`{"cloudflare_error":true,"error_code":1010}`, "Error 1010: bad browser"} {
		t.Run(body, func(t *testing.T) {
			svc, repo, _, c, recorder := newTypeSafeAccountTestFixture(t, http.StatusForbidden, body)
			require.Error(t, svc.TestAccountConnection(c, 31, "", "", AccountTestModeDefault))
			require.Zero(t, repo.errorCalls)
			require.Contains(t, recorder.Body.String(), cloudflare1010TestBlockedMessage)
		})
	}
}

func TestCNAnthropicAccountTestCloudflare1010KeepsAccountState(t *testing.T) {
	for _, tt := range []struct {
		name      string
		status    int
		body      string
		wantError int
	}{
		{"numeric code", http.StatusForbidden, `{"cloudflare_error":true,"error_code":1010}`, 0},
		{"text code", http.StatusForbidden, "Error code: 1010", 0},
		{"ordinary forbidden", http.StatusForbidden, `{"error":{"code":"access_denied"}}`, 1},
		{"unauthorized", http.StatusUnauthorized, `{"error":{"message":"invalid key"}}`, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _, c, recorder := newTypeSafeAccountTestFixture(t, tt.status, tt.body)
			account := &Account{
				ID: 32, Platform: PlatformZhipu, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test-key", "api_protocol": APIProtocolAnthropic},
			}
			require.Error(t, svc.testCNProviderAnthropicConnection(c, account, "glm-4.7"))
			require.Equal(t, tt.wantError, repo.errorCalls)
			if tt.wantError == 0 {
				require.Contains(t, recorder.Body.String(), cloudflare1010TestBlockedMessage)
			}
		})
	}
}

type cloudflare1010GrokRepo struct {
	AccountRepository
	tempCalls  int
	extraCalls int
}

func (r *cloudflare1010GrokRepo) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	r.tempCalls++
	return nil
}

func (r *cloudflare1010GrokRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	r.extraCalls++
	return nil
}

func TestGrokCloudflare1010SkipsAccountSideEffects(t *testing.T) {
	for _, tt := range []struct {
		name     string
		body     string
		wantCool int
	}{
		{"JSON block", `{"cloudflare_error":true,"error_code":1010,"error_name":"browser_signature_banned"}`, 0},
		{"text block", "Cloudflare Error 1010: bad browser", 0},
		{"ordinary forbidden", `{"error":{"code":"access_denied"}}`, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &cloudflare1010GrokRepo{}
			svc := &OpenAIGatewayService{accountRepo: repo}
			account := &Account{
				ID: 33, Platform: PlatformGrok, Type: AccountTypeOAuth,
				Extra: map[string]any{"keep": "original"},
				Credentials: map[string]any{
					"temp_unschedulable_enabled": true,
					"temp_unschedulable_rules": []any{map[string]any{
						"error_code": 403, "keywords": []any{"1010"}, "duration_minutes": 60,
					}},
				},
			}
			svc.handleGrokAccountUpstreamError(context.Background(), account, http.StatusForbidden, nil, []byte(tt.body))
			require.Equal(t, tt.wantCool, repo.tempCalls)
			if tt.wantCool == 0 {
				require.Zero(t, repo.extraCalls)
				require.Equal(t, map[string]any{"keep": "original"}, account.Extra)
			}
		})
	}
}
