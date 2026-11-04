package service

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var scheduledQuestionErrorURLStart = regexp.MustCompile(`(?i)https?://`)
var scheduledQuestionHookPath = regexp.MustCompile(`(?i)/hooks?/`)

func sanitizeScheduledQuestionErrorText(text string, plan *ScheduledQuestionTestPlan, limit int) string {
	text = strings.ToValidUTF8(text, "�")
	for attempt := 0; attempt < 3; attempt++ {
		next := sanitizeScheduledQuestionErrorTextOnce(text, plan)
		if next == text {
			break
		}
		text = next
	}
	if limit < 0 {
		limit = 0
	}
	return strings.ToValidUTF8(truncateScheduledQuestionText(text, limit), "�")
}

func sanitizeScheduledQuestionErrorTextOnce(text string, plan *ScheduledQuestionTestPlan) string {
	var sanitized strings.Builder
	starts := scheduledQuestionErrorURLStart.FindAllStringIndex(text, -1)
	cursor := 0
	for i, start := range starts {
		end := len(text)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		for offset, character := range text[start[0]:end] {
			if unicode.IsSpace(character) || strings.ContainsRune(",，、;；\"'<>()[]{}", character) {
				end = start[0] + offset
				break
			}
		}
		_, _ = sanitized.WriteString(text[cursor:start[0]])
		_, _ = sanitized.WriteString(sanitizeScheduledQuestionErrorURL(text[start[0]:end]))
		cursor = end
	}
	_, _ = sanitized.WriteString(text[cursor:])
	text = sanitized.String()
	if plan != nil {
		known := make([]string, 0, 5)
		if plan.FeishuSecret != "" {
			known = append(known, plan.FeishuSecret, url.QueryEscape(plan.FeishuSecret), url.PathEscape(plan.FeishuSecret))
		}
		if parsed, err := url.Parse(plan.FeishuWebhookURL); err == nil {
			const prefix = "/open-apis/bot/v2/hook/"
			if strings.HasPrefix(parsed.Path, prefix) {
				token := strings.TrimPrefix(parsed.Path, prefix)
				if token != "" {
					known = append(known, token, url.PathEscape(token), url.QueryEscape(token))
				}
			}
		}
		// Replace longer overlapping values first, before truncation can expose a prefix.
		sort.Slice(known, func(i, j int) bool { return len(known[i]) > len(known[j]) })
		for _, sensitive := range known {
			text = strings.ReplaceAll(text, sensitive, "[redacted]")
		}
	}
	return strings.ToValidUTF8(text, "�")
}

func sanitizeScheduledQuestionErrorURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "[redacted URL]"
	}
	segments := strings.Split(parsed.EscapedPath(), "/")
	for index, segment := range segments {
		for attempt := 0; attempt < 3; attempt++ {
			decoded, decodeErr := url.PathUnescape(segment)
			if decodeErr != nil {
				segment = escapeInvalidPathPercent(segment)
				break
			}
			if decoded == segment {
				break
			}
			segment = decoded
		}
		segments[index] = segment
	}
	path := strings.Join(segments, "/")
	if hook := scheduledQuestionHookPath.FindStringIndex(path); hook != nil {
		path = path[:hook[1]] + "****"
	}
	return parsed.Scheme + "://" + parsed.Host + path
}

func escapeInvalidPathPercent(segment string) string {
	var escaped strings.Builder
	for index := 0; index < len(segment); index++ {
		if segment[index] != '%' {
			_ = escaped.WriteByte(segment[index])
			continue
		}
		if index+2 < len(segment) && isHexByte(segment[index+1]) && isHexByte(segment[index+2]) {
			_, _ = escaped.WriteString(segment[index : index+3])
			index += 2
			continue
		}
		_, _ = escaped.WriteString("%25")
	}
	return escaped.String()
}

func isHexByte(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}
