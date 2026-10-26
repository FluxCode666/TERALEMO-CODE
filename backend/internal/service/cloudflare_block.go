package service

import (
	"encoding/json"
	"regexp"
	"strings"
)

var (
	// Matches an explicit "error code: 1010" style statement with a complete
	// numeric token, so codes such as 10100, 11010, 1010.5 or 1010e1 cannot
	// acquire the edge-block exemption through a textual prefix or suffix match.
	cloudflare1010ErrorPattern = regexp.MustCompile(`(?i)\berror(?:\s+code)?\s*[:=]?\s*([+-]?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?)\b`)
	cloudflare1010CodePattern  = regexp.MustCompile(`[+-]?\b[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?\b`)
)

const (
	// Cloudflare's browser-integrity block carries this error name; it is the
	// authoritative marker when the upstream relays the edge body verbatim.
	cloudflareBrowserSignatureError = "browser_signature_banned"
)

// isCloudflare1010Block reports Cloudflare's browser integrity / bad browser
// block (error code 1010). The request never reached the account API, so it is
// not evidence that the account's credentials, entitlement or quota are invalid.
//
// A bare 1010 numeric code inside a JSON error object is deliberately not
// treated as proof: relays and providers reuse that number for their own error
// codes, and exempting a genuine ban would let a dead account stay scheduled
// forever. JSON bodies therefore need Cloudflare corroboration (`cloudflare_error`,
// `error_name: browser_signature_banned`, or an `error_code` sibling), while
// plain-text/HTML edge bodies are matched on an explicit "error code: 1010".
//
// This is the single source of truth for 1010 detection: the shared 403 handler,
// the Grok path and the account probes all route through it.
func isCloudflare1010Block(body []byte) bool {
	var payload any
	if json.Unmarshal(body, &payload) == nil {
		return containsCloudflare1010Block(payload, false)
	}

	normalized := strings.ToLower(string(body))
	for _, match := range cloudflare1010ErrorPattern.FindAllStringSubmatch(normalized, -1) {
		if match[1] == "1010" {
			return true
		}
	}
	if strings.Contains(normalized, "cloudflare") {
		for _, code := range cloudflare1010CodePattern.FindAllString(normalized, -1) {
			if code == "1010" {
				return true
			}
		}
	}
	return false
}

// containsCloudflare1010Block walks a decoded JSON document. corroborated reports
// whether an ancestor already supplied Cloudflare-specific evidence.
func containsCloudflare1010Block(value any, corroborated bool) bool {
	switch node := value.(type) {
	case map[string]any:
		cloudflareError := false
		for key, child := range node {
			if strings.EqualFold(key, "cloudflare_error") {
				if is, ok := child.(bool); ok && is {
					cloudflareError = true
					corroborated = true
				}
			}
			if strings.EqualFold(key, "error_name") {
				if name, ok := child.(string); ok && strings.EqualFold(strings.TrimSpace(name), cloudflareBrowserSignatureError) {
					return true
				}
			}
		}
		for key, child := range node {
			switch strings.ToLower(key) {
			case "code", "error_code":
				// A code only counts when this object (or an ancestor) proves the
				// response really came from Cloudflare.
				if (corroborated || cloudflareError) && isCloudflare1010Code(child) {
					return true
				}
			case "message", "detail", "error":
				// Relay wrappers sometimes carry only the edge text, e.g.
				// {"error":{"message":"Cloudflare blocked the browser with code 1010"}}.
				if text, ok := child.(string); ok && mentionsCloudflare1010(text) {
					return true
				}
			}
			if containsCloudflare1010Block(child, corroborated || cloudflareError) {
				return true
			}
		}
	case []any:
		for _, child := range node {
			if containsCloudflare1010Block(child, corroborated) {
				return true
			}
		}
	}
	return false
}

func mentionsCloudflare1010(text string) bool {
	normalized := strings.ToLower(text)
	if !strings.Contains(normalized, "cloudflare") {
		return false
	}
	for _, match := range cloudflare1010ErrorPattern.FindAllStringSubmatch(normalized, -1) {
		if match[1] == "1010" {
			return true
		}
	}
	for _, code := range cloudflare1010CodePattern.FindAllString(normalized, -1) {
		if code == "1010" {
			return true
		}
	}
	return false
}

func isCloudflare1010Code(value any) bool {
	switch code := value.(type) {
	case float64:
		return code == 1010
	case json.Number:
		return code.String() == "1010"
	case string:
		return strings.TrimSpace(code) == "1010"
	default:
		return false
	}
}

const cloudflare1010TestBlockedMessage = "测试被边缘拦截（Cloudflare 1010），请检查网络出口或代理后重试"
