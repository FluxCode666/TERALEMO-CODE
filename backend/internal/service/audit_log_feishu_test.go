//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactAuditBodyFeishuCredentials(t *testing.T) {
	for _, key := range []string{"feishu_webhook_url", "webhook_url", "feishu_secret", "secret", "feishuWebhookURL", "Webhook-URL"} {
		t.Run(key, func(t *testing.T) {
			_, exact := auditBodySensitiveExactKeys[auditNormalizeBodyKey(key)]
			require.True(t, exact, "credential fields must be explicitly covered")
			raw, err := json.Marshal(map[string]any{
				"name": "visible plan", "base_url": "https://visible.example",
				"nested": []any{map[string]any{key: "https://open.feishu.cn/open-apis/bot/v2/hook/CANARY-private-token"}},
			})
			require.NoError(t, err)
			redacted := RedactAuditBody(raw, "application/json")
			require.NotContains(t, redacted, "CANARY-private-token")
			require.NotContains(t, redacted, "open.feishu.cn")
			require.Contains(t, redacted, `"`+key+`":"***"`)
			require.Contains(t, redacted, "visible plan")
			require.Contains(t, redacted, "https://visible.example")
		})
	}
}

func TestStoredAuditLogRedactsTruncatedBodyWithoutMutatingRecord(t *testing.T) {
	entry := &AuditLog{RequestBody: `{"feishu_webhook_url":"https://open.feishu.cn/open-apis/bot/v2/hook/CANARY-private-token"...<truncated>`}
	redacted := redactStoredAuditLog(entry)
	require.NotContains(t, redacted.RequestBody, "CANARY-private-token")
	require.Contains(t, redacted.RequestBody, "omitted")
	require.Contains(t, entry.RequestBody, "CANARY-private-token")
	placeholder := &AuditLog{RequestBody: "<credential-bearing body omitted>"}
	require.Equal(t, placeholder.RequestBody, redactStoredAuditLog(placeholder).RequestBody)
}
