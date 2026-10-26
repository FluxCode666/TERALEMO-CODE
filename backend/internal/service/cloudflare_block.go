package service

import (
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
)

var (
	cloudflare1010ErrorPattern = regexp.MustCompile(`(?i)\berror(?:\s+code)?\s*[:=]?\s*([+-]?[0-9]+(?:\.[0-9]+)?(?:e[+-]?[0-9]+)?)\b`)
	cloudflare1010CodePattern  = regexp.MustCompile(`[+-]?\b[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?\b`)
)

// isCloudflare1010Block identifies Cloudflare's browser integrity / bad browser
// block. The request never reached the account API, so it is not evidence that
// the account's credentials, entitlement or quota are invalid.
func isCloudflare1010Block(body []byte) bool {
	if gjson.ValidBytes(body) {
		for _, path := range []string{"error.code", "code"} {
			code := gjson.GetBytes(body, path)
			if (code.Type == gjson.Number && code.Num == 1010) ||
				(code.Type == gjson.String && strings.TrimSpace(code.Str) == "1010") {
				return true
			}
		}
	}

	normalized := strings.ToLower(string(body))
	// Match complete numeric tokens, so codes such as 10100 or 1010.5 cannot
	// acquire the edge-block exemption through a textual prefix match.
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

const cloudflare1010TestBlockedMessage = "测试被边缘拦截（Cloudflare 1010），请检查网络出口或代理后重试"
