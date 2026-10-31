package service

import (
	"context"
	"regexp"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

type accountTestQuestionContextKey struct{}

type accountTestQuestionState struct {
	question *model.AccountTestQuestion
	answer   strings.Builder
	failed   bool
	complete bool
}

// AccountTestQuestionResult describes regular-expression matching, not upstream connectivity.
type AccountTestQuestionResult struct {
	QuestionID      int64    `json:"question_id"`
	QuestionName    string   `json:"question_name"`
	Judged          bool     `json:"judged"`
	AnswerPatterns  []string `json:"answer_patterns"`
	MatchedPatterns []string `json:"matched_patterns"`
}

func accountTestQuestionFromContext(ctx context.Context) *accountTestQuestionState {
	state, _ := ctx.Value(accountTestQuestionContextKey{}).(*accountTestQuestionState)
	return state
}

func applyAntigravityTestQuestion(ctx context.Context, body []byte) ([]byte, error) {
	state := accountTestQuestionFromContext(ctx)
	if state == nil {
		return body, nil
	}
	body, err := sjson.SetBytes(body, "request.contents", []map[string]any{{
		"role": "user", "parts": []map[string]string{{"text": state.question.Content}},
	}})
	if err != nil {
		return nil, err
	}
	// Connectivity probes use one token; questions need room for reasoning and answers.
	return sjson.SetBytes(body, "request.generationConfig.maxOutputTokens", 4096)
}

// Only replace the user text; leave client fingerprints and all other options intact.
func applyAccountTestQuestion(c *gin.Context, payload map[string]any, field string) {
	state := accountTestQuestionFromContext(c.Request.Context())
	if state == nil {
		return
	}
	messages, ok := payload[field].([]map[string]any)
	if !ok || len(messages) != 1 {
		return
	}
	blocks, ok := messages[0]["content"].([]map[string]any)
	if ok && len(blocks) == 1 {
		blocks[0]["text"] = state.question.Content
	}
}

func questionRequiresTextModel(modelID string) bool {
	modelID = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(modelID)), "models/")
	return isOpenAIImageModel(modelID) || isGrokImageGenerationModel(modelID) || isGrokVideoGenerationModel(modelID) ||
		(strings.HasPrefix(modelID, "gemini-") && strings.Contains(modelID, "image"))
}

func (state *accountTestQuestionState) result() (AccountTestQuestionResult, bool) {
	patterns := append([]string{}, state.question.AnswerPatterns...)
	judged, matched := EvaluateAccountTestAnswer(state.answer.String(), patterns)
	result := AccountTestQuestionResult{
		QuestionID:      state.question.ID,
		QuestionName:    state.question.Name,
		Judged:          judged,
		AnswerPatterns:  patterns,
		MatchedPatterns: matched,
	}
	return result, !result.Judged || len(result.MatchedPatterns) > 0
}

// EvaluateAccountTestAnswer uses Go's RE2 syntax; case folding is opt-in via (?i).
// Patterns are validated when saved. Invalid legacy data cannot match an answer.
func EvaluateAccountTestAnswer(answer string, patterns []string) (judged bool, matched []string) {
	matched = []string{}
	for _, pattern := range patterns {
		compiled, err := regexp.Compile(pattern)
		if err == nil && compiled.MatchString(answer) {
			matched = append(matched, pattern)
		}
	}
	return len(patterns) > 0, matched
}
