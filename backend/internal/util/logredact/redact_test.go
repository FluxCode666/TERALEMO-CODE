package logredact

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactDefaultCredentialKeys(t *testing.T) {
	for _, key := range []string{"api_key", "apikey", "token", "key", "secret", "sig", "signature"} {
		t.Run(key, func(t *testing.T) {
			// The structured and text paths must agree and match case-insensitively.
			key = strings.ToUpper(key)
			input := map[string]any{key: "credential-value", "model": "video-model"}
			redacted := RedactMap(input)
			if redacted[key] != "***" || redacted["model"] != "video-model" {
				t.Fatalf("unexpected map redaction: %#v", redacted)
			}
			if input[key] != "credential-value" {
				t.Fatal("redaction mutated the input map")
			}
			for _, text := range []string{
				key + "=credential-value",
				key + ": credential-value",
				key + `="credential-value with spaces"`,
				key + `='credential-value with spaces'`,
				`upstream rejected {"` + key + `":"credential-value"}`,
			} {
				if out := RedactText(text); strings.Contains(out, "credential-value") || !strings.Contains(out, "***") {
					t.Fatalf("credential remains in %q", out)
				}
			}
			raw, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			if out := RedactText(string(raw)); strings.Contains(out, "credential-value") || !strings.Contains(out, "video-model") {
				t.Fatalf("unexpected JSON redaction: %q", out)
			}
		})
	}
}

func TestRedactText_DefaultKeysMatchWholeFields(t *testing.T) {
	input := "api_key_id=17 token_count=42 cache-key=primary refresh-token=normal secret_name=name signature_valid=true monkey=animal"
	if out := RedactText(input); out != input {
		t.Fatalf("normal fields changed: %q", out)
	}
	input = `{"api_key_id":17,"cache-key":"primary","secret_name":"name","signature_valid":true,"token_count":42}`
	if out := RedactText(input); out != input {
		t.Fatalf("normal JSON fields changed: %q", out)
	}
}

func TestRedactText_DefaultKeysPreserveAdjacentFields(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{
			"https://example.test/?key=credential&api_key=other&token_count=123&sig=signature&model=video",
			"https://example.test/?key=***&api_key=***&token_count=123&sig=***&model=video",
		},
		{"key=credential,token_count=42;signature=credential,model=video", "key=***,token_count=42;signature=***,model=video"},
	} {
		if out := RedactText(tc.input); out != tc.want {
			t.Fatalf("unexpected redaction: got %q, want %q", out, tc.want)
		}
	}
}

func TestRedactText_CredentialFormats(t *testing.T) {
	key := "sk-proj-" + strings.Repeat("Ab1_-", 8)
	for _, tc := range []struct{ input, want string }{
		{"upstream rejected " + key + " for video", "upstream rejected sk-*** for video"},
		{"Authorization: Bearer arbitrary-credential, account_id=7", "Authorization: Bearer ***, account_id=7"},
		{"authorization=bEaReR\tcredential-value; status=401", "authorization=bEaReR\t***; status=401"},
		{"Authorization: Bearer " + key, "Authorization: Bearer ***"},
	} {
		if out := RedactText(tc.input); out != tc.want {
			t.Fatalf("unexpected format redaction: got %q, want %q", out, tc.want)
		}
	}
}

func TestRedactCredentialFormatsInStructuredStrings(t *testing.T) {
	key := "sk-" + strings.Repeat("Ab1_", 8)
	input := map[string]any{
		"message": "request rejected " + key,
		"details": []any{map[string]any{"header": "Bearer credential-value", "token_count": 42}},
	}
	redacted := RedactMap(input)
	if redacted["message"] != "request rejected sk-***" {
		t.Fatalf("credential remains in structured message: %#v", redacted)
	}
	details := redacted["details"].([]any)[0].(map[string]any)
	if details["header"] != "Bearer ***" || details["token_count"] != 42 {
		t.Fatalf("unexpected nested redaction: %#v", details)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{RedactJSON(raw), RedactText(string(raw))} {
		if strings.Contains(out, key) || strings.Contains(out, "credential-value") || !json.Valid([]byte(out)) {
			t.Fatalf("unexpected JSON format redaction: %q", out)
		}
	}
}

func TestRedactText_CredentialFormatOrdinaryValuesUnchanged(t *testing.T) {
	for _, input := range []string{
		"model=sk-video-model",
		"sk-prefix format is documented",
		"sk-abcdefghijklmno", // too short to be a credential
		"task-sk-" + strings.Repeat("Ab1_", 8),
		"Bearer", "bearer_count=7", "myBearer normal-word",
	} {
		if out := RedactText(input); out != input {
			t.Fatalf("ordinary value changed: got %q, want %q", out, input)
		}
	}
}

func TestRedactText_JSONLike(t *testing.T) {
	in := `{"access_token":"ya29.a0AfH6SMDUMMY","refresh_token":"1//0gDUMMY","other":"ok"}`
	out := RedactText(in)
	if out == in {
		t.Fatalf("expected redaction, got unchanged")
	}
	if want := `"access_token":"***"`; !strings.Contains(out, want) {
		t.Fatalf("expected %q in %q", want, out)
	}
	if want := `"refresh_token":"***"`; !strings.Contains(out, want) {
		t.Fatalf("expected %q in %q", want, out)
	}
}

func TestRedactText_QueryLike(t *testing.T) {
	in := "access_token=ya29.a0AfH6SMDUMMY refresh_token=1//0gDUMMY"
	out := RedactText(in)
	if strings.Contains(out, "ya29") || strings.Contains(out, "1//0") {
		t.Fatalf("expected tokens redacted, got %q", out)
	}
}

func TestRedactText_GOCSPX(t *testing.T) {
	in := "client_secret=GOCSPX-your-client-secret"
	out := RedactText(in)
	if strings.Contains(out, "your-client-secret") {
		t.Fatalf("expected secret redacted, got %q", out)
	}
	if !strings.Contains(out, "client_secret=***") {
		t.Fatalf("expected key redacted, got %q", out)
	}
}

func TestRedactText_GeminiAPIKeys(t *testing.T) {
	legacyKey := "AI" + "za" + strings.Repeat("Ab1_-", 7)
	authKey := "AQ." + "Ab" + strings.Repeat("Xy9_-z", 8)
	in := "upstream rejected " + legacyKey + " and " + authKey + " for model gemini-3.1-pro"
	out := RedactText(in)
	if strings.Contains(out, legacyKey) || strings.Contains(out, authKey) {
		t.Fatalf("expected Gemini keys redacted, got %q", out)
	}
	for _, want := range []string{"AIza***", "AQ.***", "for model gemini-3.1-pro"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in %q", want, out)
		}
	}
}

func TestRedactText_GeminiAuthKeyPrefixAloneUnchanged(t *testing.T) {
	in := "key format AQ.Ab... is documented"
	if out := RedactText(in); out != in {
		t.Fatalf("expected short AQ. prefix unchanged, got %q", out)
	}
}

func TestRedactText_ExtraKeyCacheUsesNormalizedSortedKey(t *testing.T) {
	clearExtraTextPatternCache()

	out1 := RedactText("custom_secret=abc", "Custom_Secret", " custom_secret ")
	out2 := RedactText("custom_secret=xyz", "custom_secret")
	if !strings.Contains(out1, "custom_secret=***") {
		t.Fatalf("expected custom key redacted in first call, got %q", out1)
	}
	if !strings.Contains(out2, "custom_secret=***") {
		t.Fatalf("expected custom key redacted in second call, got %q", out2)
	}

	if got := countExtraTextPatternCacheEntries(); got != 1 {
		t.Fatalf("expected 1 cached pattern set, got %d", got)
	}
}

func TestRedactText_DefaultPathDoesNotUseExtraCache(t *testing.T) {
	clearExtraTextPatternCache()

	out := RedactText("access_token=abc")
	if !strings.Contains(out, "access_token=***") {
		t.Fatalf("expected default key redacted, got %q", out)
	}
	if got := countExtraTextPatternCacheEntries(); got != 0 {
		t.Fatalf("expected extra cache to remain empty, got %d", got)
	}
}

func clearExtraTextPatternCache() {
	extraTextPatternCache.Range(func(key, value any) bool {
		extraTextPatternCache.Delete(key)
		return true
	})
}

func countExtraTextPatternCacheEntries() int {
	count := 0
	extraTextPatternCache.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}
