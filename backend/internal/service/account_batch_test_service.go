package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

var ErrAccountBatchTestMixedPlatforms = infraerrors.BadRequest("BATCH_TEST_MIXED_PLATFORMS", "Batch test requires accounts from the same platform")

// AccountBatchTestService runs question tests through the single-account tester.
type AccountBatchTestService struct {
	accountRepo AccountRepository
	accountTest *AccountTestService
	questions   *AccountTestQuestionService
	rateLimit   *RateLimitService
}

func NewAccountBatchTestService(accountRepo AccountRepository, accountTest *AccountTestService, questions *AccountTestQuestionService, rateLimit *RateLimitService) *AccountBatchTestService {
	return &AccountBatchTestService{accountRepo: accountRepo, accountTest: accountTest, questions: questions, rateLimit: rateLimit}
}

type AccountBatchTestInput struct {
	AccountIDs  []int64
	QuestionIDs []int64
	ModelID     string
	Concurrency *int
}

// AccountBatchTestPlan is validated before the handler starts its SSE response.
type AccountBatchTestPlan struct {
	accountIDs  []int64
	questions   []*model.AccountTestQuestion
	modelID     string
	concurrency int
}

type AccountBatchTestEvent struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type AccountBatchTestStart struct {
	Total         int `json:"total"`
	AccountCount  int `json:"account_count"`
	QuestionCount int `json:"question_count"`
	Concurrency   int `json:"concurrency"`
}

type AccountBatchTestItemResult struct {
	Index           int      `json:"index"`
	AccountID       int64    `json:"account_id"`
	AccountName     string   `json:"account_name"`
	Platform        string   `json:"platform"`
	QuestionID      int64    `json:"question_id"`
	QuestionName    string   `json:"question_name"`
	Model           string   `json:"model"`
	Status          string   `json:"status"`
	Judged          bool     `json:"judged"`
	AnswerPatterns  []string `json:"answer_patterns"`
	MatchedPatterns []string `json:"matched_patterns"`
	Answer          string   `json:"answer"`
	Error           string   `json:"error"`
	LatencyMs       int64    `json:"latency_ms"`
}

type AccountBatchTestSummary struct {
	Total     int  `json:"total"`
	Passed    int  `json:"passed"`
	Failed    int  `json:"failed"`
	Unjudged  int  `json:"unjudged"`
	Error     int  `json:"error"`
	Cancelled bool `json:"cancelled"`
}

func (s *AccountBatchTestService) Prepare(ctx context.Context, input AccountBatchTestInput) (*AccountBatchTestPlan, error) {
	accountIDs, err := normalizeAccountBatchTestIDs(input.AccountIDs, "account_ids", 200)
	if err != nil {
		return nil, err
	}
	questionIDs, err := normalizeAccountBatchTestIDs(input.QuestionIDs, "question_ids", 20)
	if err != nil {
		return nil, err
	}
	concurrency := 3
	if input.Concurrency != nil {
		concurrency = max(1, min(5, *input.Concurrency))
	}
	plan := &AccountBatchTestPlan{accountIDs: accountIDs, modelID: input.ModelID, concurrency: concurrency}
	for _, id := range questionIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		question, err := s.questions.GetByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("failed to load test question %d: %w", id, err)
		}
		if question == nil {
			return nil, fmt.Errorf("test question %d not found", id)
		}
		if !question.Enabled {
			return nil, fmt.Errorf("test question %d is disabled", id)
		}
		// Freeze the question for the whole batch, including its regular expressions.
		snapshot := *question
		snapshot.AnswerPatterns = append([]string{}, question.AnswerPatterns...)
		plan.questions = append(plan.questions, &snapshot)
	}
	platform := ""
	for _, id := range accountIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		account, lookupErr := s.accountRepo.GetByID(ctx, id)
		// Missing accounts still produce per-question errors when the batch runs.
		if lookupErr != nil || account == nil {
			continue
		}
		if platform != "" && platform != account.Platform {
			return nil, ErrAccountBatchTestMixedPlatforms
		}
		platform = account.Platform
	}
	return plan, nil
}

func normalizeAccountBatchTestIDs(ids []int64, field string, limit int) ([]int64, error) {
	unique := make([]int64, 0, min(len(ids), limit))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("%s must contain positive integers (invalid id %d)", field, id)
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
		if len(unique) > limit {
			return nil, fmt.Errorf("%s must contain between 1 and %d unique ids", field, limit)
		}
	}
	if len(unique) == 0 {
		return nil, fmt.Errorf("%s must contain between 1 and %d unique ids", field, limit)
	}
	return unique, nil
}

// Run writes events only from its caller's goroutine; workers own whole accounts
// so one account's questions never compete for upstream capacity with each other.
func (s *AccountBatchTestService) Run(ctx context.Context, plan *AccountBatchTestPlan, emit func(AccountBatchTestEvent) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	total := len(plan.accountIDs) * len(plan.questions)
	if err := emit(AccountBatchTestEvent{Type: "batch_start", Data: AccountBatchTestStart{
		Total: total, AccountCount: len(plan.accountIDs), QuestionCount: len(plan.questions), Concurrency: plan.concurrency,
	}}); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan int)
	results := make(chan AccountBatchTestItemResult, plan.concurrency)
	var workers sync.WaitGroup
	for range min(plan.concurrency, len(plan.accountIDs)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for accountIndex := range jobs {
				if ctx.Err() != nil {
					return
				}
				s.runAccount(ctx, plan, accountIndex, results)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index := range plan.accountIDs {
			if ctx.Err() != nil {
				return
			}
			select {
			case jobs <- index:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	summary := AccountBatchTestSummary{Total: total}
	var emitErr error
	for item := range results {
		if ctx.Err() != nil {
			continue
		}
		if err := emit(AccountBatchTestEvent{Type: "item_result", Data: item}); err != nil {
			emitErr = err
			cancel()
			continue
		}
		switch item.Status {
		case "passed":
			summary.Passed++
		case "failed":
			summary.Failed++
		case "unjudged":
			summary.Unjudged++
		case "error":
			summary.Error++
		}
	}
	if emitErr != nil {
		return emitErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return emit(AccountBatchTestEvent{Type: "batch_complete", Data: summary})
}

func (s *AccountBatchTestService) runAccount(ctx context.Context, plan *AccountBatchTestPlan, accountIndex int, results chan<- AccountBatchTestItemResult) {
	accountID := plan.accountIDs[accountIndex]
	account, lookupErr := s.accountRepo.GetByID(ctx, accountID)
	recovered := false
	for questionIndex, question := range plan.questions {
		if ctx.Err() != nil {
			return
		}
		item := AccountBatchTestItemResult{
			Index: accountIndex*len(plan.questions) + questionIndex, AccountID: accountID,
			QuestionID: question.ID, QuestionName: question.Name, Model: plan.modelID, Status: "error",
			AnswerPatterns: append([]string{}, question.AnswerPatterns...), MatchedPatterns: []string{},
		}
		if account != nil {
			item.AccountName, item.Platform = account.Name, account.Platform
		}
		if lookupErr != nil || account == nil {
			item.Error = "Account not found"
		} else {
			connected := s.runQuestion(ctx, accountID, question, &item)
			if connected && !recovered && s.rateLimit != nil && ctx.Err() == nil {
				recovered = true
				if _, err := s.rateLimit.RecoverAccountAfterSuccessfulTest(ctx, accountID); err != nil {
					slog.Warn("batch_account_test_recovery_failed", "account_id", accountID, "error", err)
				}
			}
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case results <- item:
		case <-ctx.Done():
			return
		}
	}
}

func (s *AccountBatchTestService) runQuestion(ctx context.Context, accountID int64, question *model.AccountTestQuestion, item *AccountBatchTestItemResult) bool {
	startedAt := time.Now()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = (&http.Request{}).WithContext(ctx)
	c.Set(accountTestBackgroundKey, true)
	testErr := s.accountTest.TestAccountConnection(c, accountID, item.Model, "", AccountTestModeDefault, AccountTestOptions{Question: question})
	item.LatencyMs = time.Since(startedAt).Milliseconds()
	answer, streamErr, complete, success, questionResult := parseAccountBatchTestOutput(recorder.Body.String(), item)
	item.Answer = truncateAccountBatchTestAnswer(answer)
	if testErr != nil || streamErr != "" || !complete || (!success && !questionResult) {
		switch {
		case streamErr != "":
			item.Error = streamErr
		case testErr != nil:
			item.Error = testErr.Error()
		case item.Error == "":
			item.Error = "Account test did not complete successfully"
		}
		return false
	}
	item.Error = ""
	item.Judged, item.MatchedPatterns = EvaluateAccountTestAnswer(answer, question.AnswerPatterns)
	switch {
	case !item.Judged:
		item.Status = "unjudged"
	case len(item.MatchedPatterns) > 0:
		item.Status = "passed"
	default:
		item.Status = "failed"
		item.Error = "Answer did not match any expected pattern"
	}
	return true
}

func parseAccountBatchTestOutput(body string, item *AccountBatchTestItemResult) (answer, streamErr string, complete, success, questionResult bool) {
	var text strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event TestEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			continue
		}
		switch event.Type {
		case "test_start":
			if event.Model != "" {
				item.Model = event.Model
			}
		case "content":
			_, _ = text.WriteString(event.Text)
		case "error":
			streamErr = event.Error
			if streamErr == "" {
				streamErr = "Account test failed"
			}
		case "question_result":
			questionResult = true
		case "test_complete":
			complete, success = true, event.Success
			item.Error = event.Error
		}
	}
	return text.String(), streamErr, complete, success, questionResult
}

func truncateAccountBatchTestAnswer(answer string) string {
	runes := []rune(answer)
	if len(runes) <= 2000 {
		return answer
	}
	return string(runes[:2000]) + "\u2026"
}
