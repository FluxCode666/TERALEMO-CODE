package imagecompat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tidwall/gjson"
)

// Error types returned to clients (OpenAI style).
const (
	ErrorTypeInvalidRequest   = "invalid_request_error"
	ErrorTypeRateLimit        = "rate_limit_error"
	ErrorTypeUpstream         = "upstream_error"
	ErrorTypeContentPolicy    = "content_policy_violation"
	errorCodeContentPolicy    = "content_policy_violation"
	errorCodeUnsupportedParam = "unsupported_parameter"
	errorCodeInvalidValue     = "invalid_value"
	errorCodeMissingParam     = "missing_required_parameter"
)

// maxErrorMessageRunes bounds every client-facing message.
const maxErrorMessageRunes = 300

// Error is an OpenAI-style client error. Message is already sanitized: it never
// contains URLs, credentials or the raw upstream body.
type Error struct {
	HTTPStatus     int
	Type           string // invalid_request_error / rate_limit_error / upstream_error / content_policy_violation ...
	Code           string
	Param          string
	Message        string
	Retryable      bool // upstream 429/5xx/overload/account failures → the caller may fail over to another account
	UpstreamStatus int  // for logs and operations only; never returned to clients
	// CallIndex is the index into Plan.Calls / the BuildResponse results of
	// the call this error came from, so callers read response headers (e.g.
	// Retry-After) from the same call. -1 when the error is not tied to a
	// specific call (BuildPlan validation, standalone ConvertUpstreamError).
	// Every constructor sets it explicitly; never rely on the zero value.
	CallIndex int
}

// noCallIndex marks errors that do not belong to a specific upstream call.
const noCallIndex = -1

func (e *Error) Error() string {
	if e == nil {
		return "imagecompat: <nil>"
	}
	return fmt.Sprintf("imagecompat: %d %s: %s", e.HTTPStatus, e.Type, e.Message)
}

// OpenAIErrorBody renders {"error":{"message","type","code","param"}}; empty
// code / param are rendered as null like the OpenAI API does.
func (e *Error) OpenAIErrorBody() []byte {
	if e == nil {
		return []byte(`{"error":{"message":"unknown error","type":"upstream_error","code":null,"param":null}}`)
	}
	payload := struct {
		Error struct {
			Message string  `json:"message"`
			Type    string  `json:"type"`
			Code    *string `json:"code"`
			Param   *string `json:"param"`
		} `json:"error"`
	}{}
	payload.Error.Message = e.Message
	payload.Error.Type = e.Type
	if e.Code != "" {
		code := e.Code
		payload.Error.Code = &code
	}
	if e.Param != "" {
		param := e.Param
		payload.Error.Param = &param
	}
	body, err := marshalNoEscape(payload)
	if err != nil {
		return []byte(`{"error":{"message":"internal error","type":"upstream_error","code":null,"param":null}}`)
	}
	return body
}

func invalidParam(param, code, format string, args ...any) *Error {
	return &Error{
		HTTPStatus: http.StatusBadRequest,
		Type:       ErrorTypeInvalidRequest,
		Code:       code,
		Param:      param,
		Message:    truncateRunes(fmt.Sprintf(format, args...), maxErrorMessageRunes),
		CallIndex:  noCallIndex,
	}
}

func unsupportedParam(label, param, value string) *Error {
	return invalidParam(param, errorCodeUnsupportedParam,
		"%s does not support %s %q; omit the parameter", label, param, value)
}

func contentPolicyError(upstreamStatus int, detail string) *Error {
	msg := "The upstream safety system blocked this request; adjust the prompt or reference images and try again"
	if detail = sanitizeMessage(detail); detail != "" {
		msg += " (" + detail + ")"
	}
	return &Error{
		HTTPStatus:     http.StatusBadRequest,
		Type:           ErrorTypeContentPolicy,
		Code:           errorCodeContentPolicy,
		Message:        truncateRunes(msg, maxErrorMessageRunes),
		UpstreamStatus: upstreamStatus,
		CallIndex:      noCallIndex,
	}
}

func upstreamError(httpStatus int, code, message string, retryable bool, upstreamStatus int) *Error {
	return &Error{
		HTTPStatus:     httpStatus,
		Type:           ErrorTypeUpstream,
		Code:           code,
		Message:        truncateRunes(message, maxErrorMessageRunes),
		Retryable:      retryable,
		UpstreamStatus: upstreamStatus,
		CallIndex:      noCallIndex,
	}
}

// upstreamErrorInfo is what we can extract from an upstream error body.
type upstreamErrorInfo struct {
	message string // raw (unsanitized) message
	status  string // Gemini error.status, e.g. INVALID_ARGUMENT
	code    string // xAI / OpenAI style code
	reasons []string
}

func (i upstreamErrorInfo) searchText() string {
	return strings.ToLower(strings.Join(append([]string{i.message, i.status, i.code}, i.reasons...), " "))
}

func parseUpstreamErrorBody(body []byte) upstreamErrorInfo {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return upstreamErrorInfo{}
	}
	if !gjson.Valid(trimmed) {
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "<") || strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype") {
			// HTML error pages (proxies, CDNs) carry nothing useful for clients.
			return upstreamErrorInfo{}
		}
		return upstreamErrorInfo{message: trimmed}
	}
	root := gjson.Parse(trimmed)
	if root.IsArray() {
		root = root.Get("0")
	}
	info := upstreamErrorInfo{}
	errValue := root.Get("error")
	switch {
	case errValue.IsObject():
		info.message = errValue.Get("message").String()
		info.status = errValue.Get("status").String()
		if code := errValue.Get("code"); code.Exists() && code.Type == gjson.String {
			info.code = code.String()
		}
		if info.code == "" {
			info.code = errValue.Get("type").String()
		}
		errValue.Get("details").ForEach(func(_, detail gjson.Result) bool {
			if reason := detail.Get("reason").String(); reason != "" {
				info.reasons = append(info.reasons, reason)
			}
			return true
		})
	case errValue.Type == gjson.String:
		// xAI: {"code":"Client specified an invalid argument","error":"Incorrect API key provided: ..."}
		info.message = errValue.String()
		if code := root.Get("code"); code.Type == gjson.String {
			info.code = code.String()
		}
	}
	if info.message == "" {
		for _, key := range []string{"message", "detail", "msg", "error_description"} {
			if v := root.Get(key); v.Type == gjson.String && v.String() != "" {
				info.message = v.String()
				break
			}
		}
	}
	return info
}

func isAuthFailure(statusCode int, text string) bool {
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return true
	}
	if statusCode != http.StatusBadRequest {
		return false
	}
	// Gemini answers invalid keys with 400 INVALID_ARGUMENT / API_KEY_INVALID,
	// xAI with 400 "Incorrect API key provided".
	for _, marker := range []string{"api_key_invalid", "api key", "api_key", "apikey", "unauthenticated", "permission_denied"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func isAccountUnavailable(text string) bool {
	for _, marker := range []string{
		"location is not supported", "user location", "billed users", "billing",
		"credit", "spending limit", "quota", "suspended",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func isContentPolicyText(text string) bool {
	for _, marker := range []string{
		"content policy", "content_policy", "moderation", "safety system", "safety filter",
		"responsible ai", "violat", "nsfw", "prohibited content", "blocked by safety",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func isOverloadedText(text string) bool {
	return strings.Contains(text, "overloaded") || strings.Contains(text, "unavailable") ||
		strings.Contains(text, "try again later")
}

// ConvertUpstreamError converts a non-2xx upstream response into a client
// error. Upstream credentials problems are never surfaced verbatim.
func ConvertUpstreamError(p Protocol, statusCode int, body []byte) *Error {
	info := parseUpstreamErrorBody(body)
	text := info.searchText()
	detail := sanitizeMessage(info.message)
	label := protocolLabel(p)

	withDetail := func(base string) string {
		if detail == "" {
			return base
		}
		return base + ": " + detail
	}

	switch {
	case statusCode <= 0:
		return upstreamError(http.StatusBadGateway, "upstream_error",
			label+" upstream request failed", true, statusCode)
	case statusCode < 400:
		return upstreamError(http.StatusBadGateway, "upstream_error",
			fmt.Sprintf("%s upstream returned an unexpected status %d", label, statusCode), false, statusCode)
	case isAuthFailure(statusCode, text):
		return upstreamError(http.StatusBadGateway, "upstream_auth_failed",
			label+" upstream account is unavailable (authentication or permission failure); please retry later or contact the administrator",
			true, statusCode)
	case statusCode == http.StatusPaymentRequired:
		return upstreamError(http.StatusBadGateway, "upstream_quota_exhausted",
			label+" upstream account has insufficient quota; please retry later or contact the administrator", true, statusCode)
	case statusCode == http.StatusTooManyRequests:
		return &Error{
			HTTPStatus:     http.StatusTooManyRequests,
			Type:           ErrorTypeRateLimit,
			Code:           "rate_limit_exceeded",
			Message:        label + " upstream rate limit exceeded; please retry later",
			Retryable:      true,
			UpstreamStatus: statusCode,
			CallIndex:      noCallIndex,
		}
	case statusCode == http.StatusNotFound:
		return &Error{
			HTTPStatus:     http.StatusNotFound,
			Type:           ErrorTypeInvalidRequest,
			Code:           "model_not_found",
			Message:        "The requested model was not found on the " + label + " upstream or does not support image generation",
			Retryable:      true, // another account may map / serve the model
			UpstreamStatus: statusCode,
			CallIndex:      noCallIndex,
		}
	case statusCode == http.StatusRequestTimeout || statusCode == http.StatusGatewayTimeout:
		return upstreamError(http.StatusGatewayTimeout, "upstream_timeout",
			label+" upstream timed out; please retry later", true, statusCode)
	case statusCode == http.StatusServiceUnavailable || statusCode == 529 ||
		(statusCode >= 500 && isOverloadedText(text)):
		return upstreamError(http.StatusServiceUnavailable, "upstream_overloaded",
			label+" upstream is overloaded or temporarily unavailable; please retry later", true, statusCode)
	case statusCode >= 500:
		return upstreamError(http.StatusBadGateway, "upstream_error",
			fmt.Sprintf("%s upstream error (status %d); please retry later", label, statusCode), true, statusCode)
	case statusCode == http.StatusMethodNotAllowed:
		return upstreamError(http.StatusBadGateway, "upstream_error",
			label+" upstream rejected the request method", true, statusCode)
	case isContentPolicyText(text):
		return contentPolicyError(statusCode, info.message)
	case isAccountUnavailable(text):
		return upstreamError(http.StatusBadGateway, "upstream_account_unavailable",
			label+" upstream account is unavailable (region, billing or quota restriction); please retry later or contact the administrator",
			true, statusCode)
	case statusCode == http.StatusRequestEntityTooLarge:
		return invalidUpstreamRequest(statusCode, "request_too_large",
			withDetail(label+" upstream rejected the request because it is too large (reduce the number or size of reference images)"))
	default:
		return invalidUpstreamRequest(statusCode, "upstream_invalid_request",
			withDetail(label+" upstream rejected the request"))
	}
}

func invalidUpstreamRequest(upstreamStatus int, code, message string) *Error {
	return &Error{
		HTTPStatus:     http.StatusBadRequest,
		Type:           ErrorTypeInvalidRequest,
		Code:           code,
		Message:        truncateRunes(message, maxErrorMessageRunes),
		UpstreamStatus: upstreamStatus,
		CallIndex:      noCallIndex,
	}
}

func protocolLabel(p Protocol) string {
	switch p {
	case ProtocolGemini:
		return "Gemini"
	case ProtocolGrok:
		return "xAI Grok"
	case ProtocolOpenAI:
		return "OpenAI"
	default:
		return "image"
	}
}

var (
	reURL         = regexp.MustCompile(`(?i)\b(?:https?|wss?|ftp|file)://[^\s"'<>()\[\]{}]*`)
	reDataURL     = regexp.MustCompile(`(?i)\bdata:[a-z0-9.+\-]+/[a-z0-9.+\-]+;base64,[A-Za-z0-9+/=_\-]+`)
	reBearer      = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=\-]+`)
	reKeyParam    = regexp.MustCompile(`(?i)\b(key|api[_-]?key|access[_-]?token|token|secret|password|signature|sig)\s*[=:]\s*["']?[^\s&"',;]+`)
	reGoogleKey   = regexp.MustCompile(`AIza[0-9A-Za-z_\-]{10,}`)
	reGoogleOAuth = regexp.MustCompile(`ya29\.[0-9A-Za-z_\-.]+`)
	reXAIKey      = regexp.MustCompile(`(?i)\bxai-[0-9A-Za-z_\-*]{4,}`)
	reMaskedKey   = regexp.MustCompile(`(?i)\b(?:xa|sk|ai)[A-Za-z0-9_\-]*\*{2,}[A-Za-z0-9_\-*]*`)
	reOpenAIKey   = regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{8,}`)
	reIPv4        = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}(?::\d{1,5})?\b`)
	reHost        = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9\-]{0,61}[a-z0-9])?\.)+(?:com|net|org|io|ai|dev|app|cn|co|cloud|xyz|me|info|top|cc|tech|site|online|link|pro|vip|us|uk|jp|hk|tw|sg|de)\b(?::\d{1,5})?(?:/[^\s"'<>]*)?`)
	reLongToken   = regexp.MustCompile(`\b[A-Za-z0-9_\-]{48,}\b`)
	reWhitespace  = regexp.MustCompile(`\s+`)

	// Stack-trace fragments. Only the first meaningful line of an upstream
	// message survives (see firstMeaningfulLine); these strip frames that are
	// inlined into that line.
	reTraceback = regexp.MustCompile(`(?i)\btraceback\b.*$`)                                // Python: "Traceback (most recent call last):"
	reGoroutine = regexp.MustCompile(`(?i)\bgoroutine\s+\d+\s*\[.*$`)                       // Go: "goroutine 1 [running]:"
	rePyFrame   = regexp.MustCompile(`(?i)\bfile\s+"[^"]*",\s*line\s+\d+(?:,\s*in\s+\S+)?`) // Python frame
	// JS / Java style frames: "at render (/srv/x.js:87)", "at com.a.B.m(B.java:12)".
	reStackCall = regexp.MustCompile(`(?i)\bat\s+(?:[^\s()]+\s*){0,4}\([^)]*(?:[/\\]|:\d+)[^)]*\)`)
	// Anonymous frames: "at /srv/x.js:87:3".
	reStackBare = regexp.MustCompile(`(?i)\bat\s+[^\s()]*[/\\][^\s()]*:\d+(?::\d+)?`)
	// Source locations: "renderer.js:87", "/srv/app/main.go:123:4".
	reSourceLoc = regexp.MustCompile(`(?i)[\w.\-/\\@~+]*\.(?:js|mjs|cjs|jsx|ts|tsx|go|py|java|kt|rb|php|rs|cs|cpp|cc|c|h|scala|swift|lua|ex|exs)(?::\d+){1,2}\b`)
	// Absolute filesystem paths with at least two segments: "/srv/image/x".
	reUnixPath    = regexp.MustCompile(`(^|[\s(\[{"'=,;:])/(?:[^\s/"'<>()\[\]{},;]+/)+[^\s"'<>()\[\]{},;]*`)
	reWindowsPath = regexp.MustCompile(`\b[A-Za-z]:\\[^\s"'<>]*`)
)

// firstMeaningfulLine keeps only the first non-blank line of an upstream
// message: multi-line upstream errors put stack traces, dumps and request
// echoes after the first line.
func firstMeaningfulLine(message string) string {
	lines := strings.FieldsFunc(message, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029'
	})
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

// stripStackFragments removes stack-frame-like fragments and absolute paths.
// A line that is nothing but a stack frame becomes empty.
func stripStackFragments(message string) string {
	message = reTraceback.ReplaceAllString(message, "")
	message = reGoroutine.ReplaceAllString(message, "")
	message = rePyFrame.ReplaceAllString(message, "")
	message = reStackCall.ReplaceAllString(message, "")
	message = reStackBare.ReplaceAllString(message, "")
	message = reSourceLoc.ReplaceAllString(message, "")
	message = reUnixPath.ReplaceAllString(message, "${1}[path]")
	message = reWindowsPath.ReplaceAllString(message, "[path]")
	return message
}

// sanitizeMessage keeps the first meaningful line of an upstream message,
// strips stack fragments, filesystem paths, URLs, hosts, credentials and long
// opaque tokens, collapses whitespace and bounds its length. It cannot know
// arbitrary account secrets; the caller additionally redacts the account's
// real credentials and base URL.
func sanitizeMessage(message string) string {
	if message == "" {
		return ""
	}
	if !utf8.ValidString(message) {
		message = strings.ToValidUTF8(message, "")
	}
	message = firstMeaningfulLine(message)
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, message)
	message = reDataURL.ReplaceAllString(message, "[data]")
	message = reURL.ReplaceAllString(message, "[url]")
	message = stripStackFragments(message)
	message = reBearer.ReplaceAllString(message, "Bearer [redacted]")
	message = reKeyParam.ReplaceAllString(message, "$1=[redacted]")
	message = reGoogleKey.ReplaceAllString(message, "[redacted]")
	message = reGoogleOAuth.ReplaceAllString(message, "[redacted]")
	message = reXAIKey.ReplaceAllString(message, "[redacted]")
	message = reOpenAIKey.ReplaceAllString(message, "[redacted]")
	message = reMaskedKey.ReplaceAllString(message, "[redacted]")
	message = reIPv4.ReplaceAllString(message, "[host]")
	message = reHost.ReplaceAllString(message, "[host]")
	message = reLongToken.ReplaceAllString(message, "[redacted]")
	message = strings.TrimSpace(reWhitespace.ReplaceAllString(message, " "))
	return truncateRunes(message, maxErrorMessageRunes)
}

func truncateRunes(s string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit-1]) + "…"
}

func marshalNoEscape(v any) ([]byte, error) {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(strings.TrimRight(sb.String(), "\n")), nil
}
