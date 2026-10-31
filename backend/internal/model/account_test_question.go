package model

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// AccountTestQuestion 定义账号文本测试使用的题干和期望正则。
type AccountTestQuestion struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Content        string    `json:"content"`
	AnswerPatterns []string  `json:"answer_patterns"`
	Enabled        bool      `json:"enabled"`
	SortOrder      int       `json:"sort_order"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// NormalizeAndValidate 规范化输入并验证字符数限制，正则去重后保持原顺序。
func (q *AccountTestQuestion) NormalizeAndValidate() error {
	q.Name = strings.TrimSpace(q.Name)
	q.Content = strings.TrimSpace(q.Content)
	if q.Name == "" || utf8.RuneCountInString(q.Name) > 100 {
		return &ValidationError{Field: "name", Message: "name must contain 1 to 100 characters"}
	}
	if q.Content == "" || utf8.RuneCountInString(q.Content) > 20000 {
		return &ValidationError{Field: "content", Message: "content must contain 1 to 20000 characters"}
	}
	if q.SortOrder < math.MinInt32 || q.SortOrder > math.MaxInt32 {
		return &ValidationError{Field: "sort_order", Message: "sort_order must be between -2147483648 and 2147483647"}
	}

	patterns := make([]string, 0, len(q.AnswerPatterns))
	seen := make(map[string]struct{}, len(q.AnswerPatterns))
	for i, rawPattern := range q.AnswerPatterns {
		pattern := strings.TrimSpace(rawPattern)
		if pattern == "" {
			continue
		}
		if utf8.RuneCountInString(pattern) > 500 {
			return &ValidationError{Field: "answer_patterns", Message: fmt.Sprintf("pattern %d %q must contain at most 500 characters", i+1, rawPattern)}
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return &ValidationError{Field: "answer_patterns", Message: fmt.Sprintf("pattern %d %q is invalid: %s", i+1, rawPattern, err.Error())}
		}
		if _, exists := seen[pattern]; exists {
			continue
		}
		seen[pattern] = struct{}{}
		patterns = append(patterns, pattern)
	}
	if len(patterns) > 20 {
		return &ValidationError{Field: "answer_patterns", Message: "at most 20 patterns are allowed"}
	}
	q.AnswerPatterns = patterns
	return nil
}
