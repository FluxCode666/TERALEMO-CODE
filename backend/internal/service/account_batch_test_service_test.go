//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type batchTestQuestionRepo struct {
	AccountTestQuestionRepository
	questions map[int64]*model.AccountTestQuestion
	lookups   []int64
	err       error
}

func (r *batchTestQuestionRepo) GetByID(_ context.Context, id int64) (*model.AccountTestQuestion, error) {
	r.lookups = append(r.lookups, id)
	return r.questions[id], r.err
}

type batchTestAccountRepo struct {
	AccountRepository
	mu            sync.Mutex
	accounts      map[int64]*Account
	lookups       []int64
	clearedErrors []int64
}

func (r *batchTestAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lookups = append(r.lookups, id)
	return r.accounts[id], nil
}

func (r *batchTestAccountRepo) ClearError(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clearedErrors = append(r.clearedErrors, id)
	return nil
}

func batchSyntheticAccount(id int64) *Account {
	return &Account{ID: id, Name: fmt.Sprintf("account-%d", id), Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		Extra: map[string]any{"synthetic_ui_test": true}}
}

func batchEnabledQuestion(id int64, patterns ...string) *model.AccountTestQuestion {
	return &model.AccountTestQuestion{ID: id, Name: fmt.Sprintf("question-%d", id), Content: fmt.Sprintf("question %d", id), Enabled: true, AnswerPatterns: patterns}
}

func newBatchTestService(accountRepo *batchTestAccountRepo, questionRepo *batchTestQuestionRepo, upstream HTTPUpstream, recoverAccounts bool) *AccountBatchTestService {
	gin.SetMode(gin.TestMode)
	testService := NewAccountTestService(accountRepo, nil, nil, nil, nil, upstream, &config.Config{}, nil)
	var rateLimit *RateLimitService
	if recoverAccounts {
		rateLimit = NewRateLimitService(accountRepo, nil, nil, nil, nil)
	}
	return NewAccountBatchTestService(accountRepo, testService, NewAccountTestQuestionService(questionRepo), rateLimit)
}

func TestAccountBatchTestPrepare(t *testing.T) {
	repo := &batchTestQuestionRepo{questions: map[int64]*model.AccountTestQuestion{1: batchEnabledQuestion(1), 2: batchEnabledQuestion(2)}}
	svc := newBatchTestService(&batchTestAccountRepo{}, repo, nil, false)
	for _, tt := range []struct {
		name        string
		concurrency *int
		want        int
	}{
		{name: "default", want: 3},
		{name: "lower clamp", concurrency: new(int), want: 1},
		{name: "negative clamp", concurrency: batchInt(-9), want: 1},
		{name: "upper clamp", concurrency: batchInt(99), want: 5},
		{name: "in range", concurrency: batchInt(4), want: 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := svc.Prepare(context.Background(), AccountBatchTestInput{
				AccountIDs: []int64{3, 1, 3}, QuestionIDs: []int64{2, 1, 2}, ModelID: "custom", Concurrency: tt.concurrency,
			})
			require.NoError(t, err)
			require.Equal(t, []int64{3, 1}, plan.accountIDs)
			require.Equal(t, int64(2), plan.questions[0].ID)
			require.Equal(t, int64(1), plan.questions[1].ID)
			require.Equal(t, "custom", plan.modelID)
			require.Equal(t, tt.want, plan.concurrency)
		})
	}
	for _, tt := range []struct {
		name      string
		accounts  []int64
		questions []int64
		want      string
	}{
		{name: "no accounts", questions: []int64{1}, want: "account_ids"},
		{name: "no questions", accounts: []int64{1}, want: "question_ids"},
		{name: "zero account", accounts: []int64{0}, questions: []int64{1}, want: "positive integers"},
		{name: "negative question", accounts: []int64{1}, questions: []int64{-1}, want: "positive integers"},
		{name: "too many accounts", accounts: batchIDRange(201), questions: []int64{1}, want: "200"},
		{name: "too many questions", accounts: []int64{1}, questions: batchIDRange(21), want: "20"},
		{name: "missing question", accounts: []int64{1}, questions: []int64{99}, want: "99 not found"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Prepare(context.Background(), AccountBatchTestInput{AccountIDs: tt.accounts, QuestionIDs: tt.questions})
			require.ErrorContains(t, err, tt.want)
		})
	}
	repo.questions[2].Enabled = false
	_, err := svc.Prepare(context.Background(), AccountBatchTestInput{AccountIDs: []int64{1}, QuestionIDs: []int64{1, 2}})
	require.ErrorContains(t, err, "2 is disabled")
	repo.err = errors.New("database unavailable")
	_, err = svc.Prepare(context.Background(), AccountBatchTestInput{AccountIDs: []int64{1}, QuestionIDs: []int64{1}})
	require.ErrorContains(t, err, "test question 1")
	require.ErrorContains(t, err, "database unavailable")
}

func batchInt(value int) *int { return &value }

func TestAccountBatchTestPrepareRequiresSamePlatform(t *testing.T) {
	for _, tt := range []struct {
		name      string
		platforms []string
		wantError bool
	}{
		{name: "same", platforms: []string{PlatformAnthropic, PlatformAnthropic}},
		{name: "mixed", platforms: []string{PlatformAnthropic, PlatformOpenAI}, wantError: true},
		{name: "same video remains item errors", platforms: []string{PlatformVideo, PlatformVideo}},
		{name: "same typesafe remains item errors", platforms: []string{PlatformTypeSafe, PlatformTypeSafe}},
		{name: "missing account remains item error", platforms: []string{PlatformAnthropic, ""}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			accounts := &batchTestAccountRepo{accounts: map[int64]*Account{}}
			for i, platform := range tt.platforms {
				if platform != "" {
					accounts.accounts[int64(i+1)] = &Account{ID: int64(i + 1), Platform: platform}
				}
			}
			questions := &batchTestQuestionRepo{questions: map[int64]*model.AccountTestQuestion{1: batchEnabledQuestion(1)}}
			svc := newBatchTestService(accounts, questions, nil, false)
			_, err := svc.Prepare(context.Background(), AccountBatchTestInput{AccountIDs: []int64{1, 2}, QuestionIDs: []int64{1}, ModelID: "manual-model"})
			if tt.wantError {
				require.ErrorIs(t, err, ErrAccountBatchTestMixedPlatforms)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func batchIDRange(count int) []int64 {
	ids := make([]int64, count)
	for index := range ids {
		ids[index] = int64(index + 1)
	}
	return ids
}

func TestAccountBatchTestStatusesIndicesAndRecovery(t *testing.T) {
	account := batchSyntheticAccount(7)
	account.Status = StatusError
	accounts := &batchTestAccountRepo{accounts: map[int64]*Account{7: account}}
	questions := &batchTestQuestionRepo{questions: map[int64]*model.AccountTestQuestion{
		1: batchEnabledQuestion(1, "(?i)anthropic"), 2: batchEnabledQuestion(2, "^not-the-answer$"), 3: batchEnabledQuestion(3),
	}}
	svc := newBatchTestService(accounts, questions, nil, true)
	plan, err := svc.Prepare(context.Background(), AccountBatchTestInput{AccountIDs: []int64{7, 99}, QuestionIDs: []int64{1, 2, 3}, Concurrency: batchInt(1)})
	require.NoError(t, err)
	var events []AccountBatchTestEvent
	err = svc.Run(context.Background(), plan, func(event AccountBatchTestEvent) error {
		events = append(events, event)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, events, 8)
	require.Equal(t, "batch_start", events[0].Type)
	start, ok := events[0].Data.(AccountBatchTestStart)
	require.True(t, ok)
	require.Equal(t, AccountBatchTestStart{Total: 6, AccountCount: 2, QuestionCount: 3, Concurrency: 1}, start)
	for index, status := range []string{"passed", "failed", "unjudged", "error", "error", "error"} {
		require.Equal(t, "item_result", events[index+1].Type)
		item, ok := events[index+1].Data.(AccountBatchTestItemResult)
		require.True(t, ok)
		require.Equal(t, index, item.Index)
		require.Equal(t, status, item.Status)
		require.Equal(t, int64(index%3+1), item.QuestionID)
		if index < 3 {
			require.Equal(t, "account-7", item.AccountName)
			require.Equal(t, PlatformAnthropic, item.Platform)
			require.NotEmpty(t, item.Model)
			require.Contains(t, item.Answer, "Synthetic")
		} else {
			require.Equal(t, "Account not found", item.Error)
		}
	}
	require.Equal(t, "batch_complete", events[7].Type)
	summary, ok := events[7].Data.(AccountBatchTestSummary)
	require.True(t, ok)
	require.Equal(t, AccountBatchTestSummary{Total: 6, Passed: 1, Failed: 1, Unjudged: 1, Error: 3}, summary)
	require.Equal(t, []int64{7}, accounts.clearedErrors, "all three successful connections recover this account only once")
}

func TestAccountBatchTestUnsupportedAccountsAndModels(t *testing.T) {
	for _, tt := range []struct{ name, platform, model, want string }{
		{name: "video", platform: PlatformVideo, want: "Test questions are not supported for this account"},
		{name: "typesafe", platform: PlatformTypeSafe, want: "Test questions are not supported for this account"},
		{name: "openai image", platform: PlatformOpenAI, model: "gpt-image-2", want: "Test questions require a text model"},
		{name: "grok video", platform: PlatformGrok, model: "grok-imagine-video", want: "Test questions require a text model"},
		{name: "gemini image", platform: PlatformGemini, model: "gemini-2.5-flash-image", want: "Test questions require a text model"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			accounts := &batchTestAccountRepo{accounts: map[int64]*Account{1: {ID: 1, Platform: tt.platform, Type: AccountTypeAPIKey}}}
			questions := &batchTestQuestionRepo{questions: map[int64]*model.AccountTestQuestion{1: batchEnabledQuestion(1)}}
			svc := newBatchTestService(accounts, questions, nil, false)
			plan, err := svc.Prepare(context.Background(), AccountBatchTestInput{AccountIDs: []int64{1}, QuestionIDs: []int64{1}, ModelID: tt.model})
			require.NoError(t, err)
			err = svc.Run(context.Background(), plan, func(event AccountBatchTestEvent) error {
				if event.Type == "item_result" {
					item, ok := event.Data.(AccountBatchTestItemResult)
					require.True(t, ok)
					require.Equal(t, "error", item.Status)
					require.Equal(t, tt.want, item.Error)
				}
				return nil
			})
			require.NoError(t, err)
		})
	}
}

type batchTestHTTPUpstream struct {
	call func(*http.Request, int64) (*http.Response, error)
}

func (u *batchTestHTTPUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	return u.call(req, accountID)
}

func (u *batchTestHTTPUpstream) DoWithTLS(req *http.Request, _ string, accountID int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.call(req, accountID)
}

func batchClaudeAccount(id int64) *Account {
	return &Account{ID: id, Name: "claude", Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-key", "base_url": "https://batch-test.example"}}
}

func batchClaudeResponse(text string) *http.Response {
	payload, _ := json.Marshal(map[string]any{"type": "content_block_delta", "delta": map[string]string{"text": text}})
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader("data: " + string(payload) + "\n\ndata: {\"type\":\"message_stop\"}\n\n"))}
}

func TestAccountBatchTestJudgesFullAnswerBeforeRuneTruncation(t *testing.T) {
	answer := strings.Repeat("\u6c49", 2001) + "TAIL_MATCH"
	accounts := &batchTestAccountRepo{accounts: map[int64]*Account{1: batchClaudeAccount(1)}}
	questions := &batchTestQuestionRepo{questions: map[int64]*model.AccountTestQuestion{1: batchEnabledQuestion(1, "TAIL_MATCH$")}}
	var body []byte
	upstream := &batchTestHTTPUpstream{call: func(req *http.Request, _ int64) (*http.Response, error) {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		return batchClaudeResponse(answer), nil
	}}
	svc := newBatchTestService(accounts, questions, upstream, false)
	plan, err := svc.Prepare(context.Background(), AccountBatchTestInput{AccountIDs: []int64{1}, QuestionIDs: []int64{1}, ModelID: "claude-custom"})
	require.NoError(t, err)
	err = svc.Run(context.Background(), plan, func(event AccountBatchTestEvent) error {
		if event.Type == "item_result" {
			item, ok := event.Data.(AccountBatchTestItemResult)
			require.True(t, ok)
			require.Equal(t, "passed", item.Status)
			require.Equal(t, []string{"TAIL_MATCH$"}, item.MatchedPatterns)
			require.Equal(t, 2001, utf8.RuneCountInString(item.Answer))
			require.True(t, strings.HasSuffix(item.Answer, "\u2026"))
			require.NotContains(t, item.Answer, "TAIL_MATCH")
			require.Equal(t, "claude-custom", item.Model)
		}
		return nil
	})
	require.NoError(t, err)
	var request struct {
		Messages []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(body, &request))
	require.Len(t, request.Messages, 1)
	require.Len(t, request.Messages[0].Content, 1)
	require.Equal(t, "question 1", request.Messages[0].Content[0].Text)
}

func TestAccountBatchTestWorkersSerializeEachAccountAndRespectConcurrency(t *testing.T) {
	accounts := &batchTestAccountRepo{accounts: map[int64]*Account{1: batchClaudeAccount(1), 2: batchClaudeAccount(2), 3: batchClaudeAccount(3)}}
	questions := &batchTestQuestionRepo{questions: map[int64]*model.AccountTestQuestion{1: batchEnabledQuestion(1), 2: batchEnabledQuestion(2)}}
	var mu sync.Mutex
	active, maxActive := 0, 0
	perAccount := map[int64]int{}
	order := map[int64][]string{}
	overlapped := false
	upstream := &batchTestHTTPUpstream{call: func(req *http.Request, accountID int64) (*http.Response, error) {
		payload, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		active++
		maxActive = max(maxActive, active)
		perAccount[accountID]++
		overlapped = overlapped || perAccount[accountID] > 1
		order[accountID] = append(order[accountID], string(payload))
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		active--
		perAccount[accountID]--
		mu.Unlock()
		return batchClaudeResponse("ok"), nil
	}}
	svc := newBatchTestService(accounts, questions, upstream, false)
	plan, err := svc.Prepare(context.Background(), AccountBatchTestInput{AccountIDs: []int64{1, 2, 3}, QuestionIDs: []int64{2, 1}, Concurrency: batchInt(2)})
	require.NoError(t, err)
	err = svc.Run(context.Background(), plan, func(AccountBatchTestEvent) error { return nil })
	require.NoError(t, err)
	require.False(t, overlapped)
	require.LessOrEqual(t, maxActive, 2)
	for _, accountOrder := range order {
		require.Len(t, accountOrder, 2)
		require.Contains(t, accountOrder[0], "question 2")
		require.Contains(t, accountOrder[1], "question 1")
	}
	require.Len(t, order, 3)
}

func TestAccountBatchTestCancellationStopsDispatchAndEvents(t *testing.T) {
	accounts := &batchTestAccountRepo{accounts: map[int64]*Account{1: batchClaudeAccount(1), 2: batchClaudeAccount(2)}}
	questions := &batchTestQuestionRepo{questions: map[int64]*model.AccountTestQuestion{1: batchEnabledQuestion(1), 2: batchEnabledQuestion(2)}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls, stopped atomic.Int32
	upstream := &batchTestHTTPUpstream{call: func(req *http.Request, _ int64) (*http.Response, error) {
		calls.Add(1)
		cancel()
		<-req.Context().Done()
		stopped.Add(1)
		return nil, req.Context().Err()
	}}
	svc := newBatchTestService(accounts, questions, upstream, false)
	plan, err := svc.Prepare(ctx, AccountBatchTestInput{AccountIDs: []int64{1, 2}, QuestionIDs: []int64{1, 2}, Concurrency: batchInt(1)})
	require.NoError(t, err)
	var events []string
	err = svc.Run(ctx, plan, func(event AccountBatchTestEvent) error {
		require.NoError(t, ctx.Err(), "never emit after cancellation")
		events = append(events, event.Type)
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, int32(1), stopped.Load(), "Run waits for the running upstream call to observe cancellation")
	require.Equal(t, []string{"batch_start"}, events)

	err = svc.Run(ctx, plan, func(AccountBatchTestEvent) error {
		t.Error("cancelled run must not write even batch_start")
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
}

func TestAccountBatchTestEmitFailureStopsWorkers(t *testing.T) {
	accounts := &batchTestAccountRepo{accounts: map[int64]*Account{1: batchSyntheticAccount(1)}}
	questions := &batchTestQuestionRepo{questions: map[int64]*model.AccountTestQuestion{1: batchEnabledQuestion(1)}}
	svc := newBatchTestService(accounts, questions, nil, false)
	plan, err := svc.Prepare(context.Background(), AccountBatchTestInput{AccountIDs: []int64{1}, QuestionIDs: []int64{1}})
	require.NoError(t, err)
	writeErr := errors.New("client write failed")
	err = svc.Run(context.Background(), plan, func(event AccountBatchTestEvent) error {
		if event.Type == "item_result" {
			return writeErr
		}
		require.Equal(t, "batch_start", event.Type)
		return nil
	})
	require.ErrorIs(t, err, writeErr)
}
