//go:build unit

package model

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountTestQuestionNormalizeAndValidate(t *testing.T) {
	question := &AccountTestQuestion{
		Name: "  name\n", Content: " \ncontent\t ",
		AnswerPatterns: []string{" Claude ", "", "\t", "claude", "Claude", " anthropic "},
	}
	require.NoError(t, question.NormalizeAndValidate())
	require.Equal(t, "name", question.Name)
	require.Equal(t, "content", question.Content)
	require.Equal(t, []string{"Claude", "claude", "anthropic"}, question.AnswerPatterns)
	require.NoError(t, question.NormalizeAndValidate())
	require.Equal(t, []string{"Claude", "claude", "anthropic"}, question.AnswerPatterns)
}

func TestAccountTestQuestionValidationBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		question AccountTestQuestion
		field    string
	}{
		{name: "empty name", question: AccountTestQuestion{Name: " \t", Content: "x"}, field: "name"},
		{name: "long name", question: AccountTestQuestion{Name: strings.Repeat("x", 101), Content: "x"}, field: "name"},
		{name: "empty content", question: AccountTestQuestion{Name: "x", Content: " \n"}, field: "content"},
		{name: "long content", question: AccountTestQuestion{Name: "x", Content: strings.Repeat("x", 20001)}, field: "content"},
		{name: "long pattern", question: AccountTestQuestion{Name: "x", Content: "x", AnswerPatterns: []string{strings.Repeat("x", 501)}}, field: "answer_patterns"},
		{name: "long unicode pattern", question: AccountTestQuestion{Name: "x", Content: "x", AnswerPatterns: []string{strings.Repeat("字", 501)}}, field: "answer_patterns"},
		{name: "max unicode characters", question: AccountTestQuestion{
			Name: strings.Repeat("题", 100), Content: strings.Repeat("文", 20000), AnswerPatterns: []string{strings.Repeat("字", 500)},
		}},
		{name: "no patterns", question: AccountTestQuestion{Name: "x", Content: "x"}},
		{name: "minimum sort order", question: AccountTestQuestion{Name: "x", Content: "x", SortOrder: math.MinInt32}},
		{name: "maximum sort order", question: AccountTestQuestion{Name: "x", Content: "x", SortOrder: math.MaxInt32}},
		{name: "sort order below minimum", question: AccountTestQuestion{Name: "x", Content: "x", SortOrder: math.MinInt32 - 1}, field: "sort_order"},
		{name: "sort order above maximum", question: AccountTestQuestion{Name: "x", Content: "x", SortOrder: math.MaxInt32 + 1}, field: "sort_order"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.question.NormalizeAndValidate()
			if tt.field == "" {
				require.NoError(t, err)
				require.NotNil(t, tt.question.AnswerPatterns)
				return
			}
			var validationError *ValidationError
			require.ErrorAs(t, err, &validationError)
			require.Equal(t, tt.field, validationError.Field)
		})
	}
}

func TestAccountTestQuestionPatternsRE2ValidationReportsOriginalPosition(t *testing.T) {
	for _, pattern := range []string{" [ ", " (?=claude) ", ` (claude)\1 `, "(?P<name>"} {
		t.Run(pattern, func(t *testing.T) {
			question := &AccountTestQuestion{
				Name: "name", Content: "content", AnswerPatterns: []string{"", " valid ", "valid", pattern},
			}
			err := question.NormalizeAndValidate()
			var validationError *ValidationError
			require.ErrorAs(t, err, &validationError)
			require.Equal(t, "answer_patterns", validationError.Field)
			require.Contains(t, validationError.Message, fmt.Sprintf("pattern 4 %q", pattern))
			_, compileErr := regexp.Compile(strings.TrimSpace(pattern))
			require.Error(t, compileErr)
			require.Contains(t, validationError.Message, compileErr.Error())
		})
	}
}

func TestAccountTestQuestionPatternsPreserveFlagsAndCommas(t *testing.T) {
	question := &AccountTestQuestion{
		Name: "name", Content: "content",
		AnswerPatterns: []string{" (?i)claude ", "(?s)^first.*last$", "^[a-z]{1,3},[0-9]+$", "", " (?i)claude "},
	}
	require.NoError(t, question.NormalizeAndValidate())
	require.Equal(t, []string{"(?i)claude", "(?s)^first.*last$", "^[a-z]{1,3},[0-9]+$"}, question.AnswerPatterns)
}

func TestAccountTestQuestionPatternLimitAppliesAfterNormalization(t *testing.T) {
	patterns := make([]string, 20)
	for i := range patterns {
		patterns[i] = strings.Repeat("x", i+1)
	}
	question := &AccountTestQuestion{Name: "x", Content: "x", AnswerPatterns: append(patterns, " x ", " ")}
	require.NoError(t, question.NormalizeAndValidate())
	require.Len(t, question.AnswerPatterns, 20)
	question.AnswerPatterns = append(question.AnswerPatterns, "another")
	require.Error(t, question.NormalizeAndValidate())
}
