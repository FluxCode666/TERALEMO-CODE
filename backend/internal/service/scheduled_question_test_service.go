package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/model"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrScheduledQuestionTestNotFound = infraerrors.NotFound("SCHEDULED_QUESTION_TEST_NOT_FOUND", "scheduled question test plan not found")
	ErrScheduledQuestionTestRunning  = infraerrors.Conflict("SCHEDULED_QUESTION_TEST_RUNNING", "scheduled question test plan is already running")
	ErrScheduledQuestionTestStopped  = infraerrors.ServiceUnavailable("SCHEDULED_QUESTION_TEST_STOPPED", "scheduled question test service is stopping")
)

type scheduledQuestionBatchTester interface {
	Prepare(context.Context, AccountBatchTestInput) (*AccountBatchTestPlan, error)
	Run(context.Context, *AccountBatchTestPlan, func(AccountBatchTestEvent) error) error
}

type scheduledQuestionEmailSender interface {
	SendEmail(ctx context.Context, to, subject, body string) error
}

// ScheduledQuestionTestService reuses batch tests and persists account alert transitions.
type ScheduledQuestionTestService struct {
	repo                    ScheduledQuestionTestRepository
	accounts                AccountRepository
	questions               *AccountTestQuestionService
	batch                   scheduledQuestionBatchTester
	settings                SettingRepository
	email                   scheduledQuestionEmailSender
	feishu                  scheduledQuestionFeishuNotifier
	notificationTimeouts    scheduledQuestionNotificationTimeouts
	ownershipRefreshTimeout time.Duration
	usageLogRepo            UsageLogRepository
	sessionLimitCache       SessionLimitCache
	now                     func() time.Time
	mu                      sync.Mutex
	running                 map[int64]bool
	baseCtx                 context.Context
	cancel                  context.CancelFunc
	stopped                 bool
	active                  sync.WaitGroup
}

func NewScheduledQuestionTestService(repo ScheduledQuestionTestRepository, accounts AccountRepository, questions *AccountTestQuestionService, batch *AccountBatchTestService, settings SettingRepository, email *EmailService, usageLogRepo UsageLogRepository, sessionLimitCache SessionLimitCache) *ScheduledQuestionTestService {
	svc := &ScheduledQuestionTestService{
		repo: repo, accounts: accounts, questions: questions, batch: batch, settings: settings,
		now: time.Now, running: make(map[int64]bool), feishu: newFeishuWebhookNotifier(), usageLogRepo: usageLogRepo, sessionLimitCache: sessionLimitCache,
	}
	if email != nil {
		svc.email = email
	}
	svc.Start()
	return svc
}

func (s *ScheduledQuestionTestService) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.baseCtx == nil && !s.stopped {
		s.baseCtx, s.cancel = context.WithCancel(context.Background())
	}
}

// Stop rejects new work and joins both manual and scheduled executions before storage is closed.
func (s *ScheduledQuestionTestService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stopped = true
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	s.active.Wait()
}

func (s *ScheduledQuestionTestService) withRunning(plan *ScheduledQuestionTestPlan) *ScheduledQuestionTestPlan {
	copy := *plan
	PopulateScheduledQuestionTestPlanNotificationConfig(&copy)
	s.mu.Lock()
	copy.Running = s.running[plan.ID]
	s.mu.Unlock()
	return &copy
}

func (s *ScheduledQuestionTestService) List(ctx context.Context) ([]*ScheduledQuestionTestPlan, error) {
	plans, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*ScheduledQuestionTestPlan, 0, len(plans))
	for _, plan := range plans {
		result = append(result, s.withRunning(plan))
	}
	return result, nil
}

func (s *ScheduledQuestionTestService) GetByID(ctx context.Context, id int64) (*ScheduledQuestionTestPlan, error) {
	plan, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, ErrScheduledQuestionTestNotFound
	}
	return s.withRunning(plan), nil
}

func applyScheduledQuestionTestInput(plan *ScheduledQuestionTestPlan, input ScheduledQuestionTestPlanInput) ScheduledQuestionTestPlanUpdateMask {
	mask := ScheduledQuestionTestPlanUpdateMask{
		Name: input.Name != nil, AccountIDs: input.AccountIDs != nil, QuestionIDs: input.QuestionIDs != nil,
		ModelID: input.ModelID != nil, CronExpression: input.CronExpression != nil, Enabled: input.Enabled != nil,
		MinPassRatio: input.MinPassRatio != nil, CooldownMinutes: input.CooldownMinutes != nil, NotifyEnabled: input.NotifyEnabled != nil,
		ReportEnabled: input.ReportEnabled != nil, EmailEnabled: input.EmailEnabled != nil, FeishuEnabled: input.FeishuEnabled != nil,
		FeishuWebhookURL: input.FeishuWebhookURL != nil, FeishuSecret: input.FeishuSecret != nil,
		AutoPauseEnabled: input.AutoPauseEnabled != nil, AutoResumeEnabled: input.AutoResumeEnabled != nil,
	}
	if input.Name != nil {
		plan.Name = *input.Name
	}
	if input.AccountIDs != nil {
		plan.AccountIDs = append([]int64{}, (*input.AccountIDs)...)
	}
	if input.QuestionIDs != nil {
		plan.QuestionIDs = append([]int64{}, (*input.QuestionIDs)...)
	}
	if input.ModelID != nil {
		plan.ModelID = *input.ModelID
	}
	if input.CronExpression != nil {
		plan.CronExpression = *input.CronExpression
	}
	if input.Enabled != nil {
		plan.Enabled = *input.Enabled
	}
	if input.MinPassRatio != nil {
		plan.MinPassRatio = *input.MinPassRatio
	}
	if input.CooldownMinutes != nil {
		plan.CooldownMinutes = *input.CooldownMinutes
	}
	if input.NotifyEnabled != nil {
		plan.NotifyEnabled = *input.NotifyEnabled
	}
	if input.ReportEnabled != nil {
		plan.ReportEnabled = *input.ReportEnabled
	}
	if input.EmailEnabled != nil {
		plan.EmailEnabled = *input.EmailEnabled
	}
	if input.FeishuEnabled != nil {
		plan.FeishuEnabled = *input.FeishuEnabled
	}
	if input.FeishuWebhookURL != nil {
		plan.FeishuWebhookURL = strings.TrimSpace(*input.FeishuWebhookURL)
	}
	if input.FeishuSecret != nil {
		plan.FeishuSecret = strings.TrimSpace(*input.FeishuSecret)
	}
	if input.AutoPauseEnabled != nil {
		plan.AutoPauseEnabled = *input.AutoPauseEnabled
	}
	if input.AutoResumeEnabled != nil {
		plan.AutoResumeEnabled = *input.AutoResumeEnabled
	}
	return mask
}

func scheduledQuestionValidation(field, message string) error {
	return &model.ValidationError{Field: field, Message: message}
}

func (s *ScheduledQuestionTestService) validatePlanFields(plan *ScheduledQuestionTestPlan) error {
	plan.Name, plan.ModelID, plan.CronExpression = strings.TrimSpace(plan.Name), strings.TrimSpace(plan.ModelID), strings.TrimSpace(plan.CronExpression)
	if plan.Name == "" || utf8.RuneCountInString(plan.Name) > 100 {
		return scheduledQuestionValidation("name", "name must contain 1 to 100 characters")
	}
	if utf8.RuneCountInString(plan.ModelID) > 100 {
		return scheduledQuestionValidation("model_id", "model_id must contain at most 100 characters")
	}
	if plan.MinPassRatio < 0 || plan.MinPassRatio > 100 {
		return scheduledQuestionValidation("min_pass_ratio", "min_pass_ratio must be between 0 and 100")
	}
	if plan.CooldownMinutes < 0 || plan.CooldownMinutes > 10080 {
		return scheduledQuestionValidation("cooldown_minutes", "cooldown_minutes must be between 0 and 10080")
	}
	if err := validateScheduledQuestionFeishuWebhook(plan.FeishuWebhookURL); err != nil {
		return err
	}
	if utf8.RuneCountInString(plan.FeishuSecret) > 200 {
		return scheduledQuestionValidation("feishu_secret", "feishu_secret must contain at most 200 characters")
	}
	if plan.FeishuEnabled && plan.FeishuWebhookURL == "" {
		return scheduledQuestionValidation("feishu_webhook_url", "feishu webhook is required when the channel is enabled")
	}
	if utf8.RuneCountInString(plan.CronExpression) > 100 {
		return scheduledQuestionValidation("cron_expression", "cron_expression must contain at most 100 characters")
	}
	if _, err := computeNextRun(plan.CronExpression, s.now()); err != nil {
		return scheduledQuestionValidation("cron_expression", "invalid cron expression: "+err.Error())
	}
	var err error
	plan.AccountIDs, err = normalizeAccountBatchTestIDs(plan.AccountIDs, "account_ids", 200)
	if err != nil {
		return scheduledQuestionValidation("account_ids", err.Error())
	}
	plan.QuestionIDs, err = normalizeAccountBatchTestIDs(plan.QuestionIDs, "question_ids", 20)
	if err != nil {
		return scheduledQuestionValidation("question_ids", err.Error())
	}
	return nil
}

func (s *ScheduledQuestionTestService) validatePlanDependencies(ctx context.Context, plan *ScheduledQuestionTestPlan) error {
	platform := ""
	for _, id := range plan.AccountIDs {
		account, err := s.accounts.GetByID(ctx, id)
		if err != nil || account == nil {
			return scheduledQuestionValidation("account_ids", fmt.Sprintf("account %d not found or unavailable", id))
		}
		if platform != "" && account.Platform != platform {
			return scheduledQuestionValidation("account_ids", ErrAccountBatchTestMixedPlatforms.Message)
		}
		platform = account.Platform
	}
	for _, id := range plan.QuestionIDs {
		question, err := s.questions.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if question == nil || !question.Enabled {
			return scheduledQuestionValidation("question_ids", fmt.Sprintf("test question %d not found or disabled", id))
		}
	}
	return nil
}

func (s *ScheduledQuestionTestService) Create(ctx context.Context, input ScheduledQuestionTestPlanInput) (*ScheduledQuestionTestPlan, error) {
	plan := &ScheduledQuestionTestPlan{Enabled: true, MinPassRatio: 80, CooldownMinutes: 360, NotifyEnabled: true, ReportEnabled: true, EmailEnabled: true}
	applyScheduledQuestionTestInput(plan, input)
	if err := s.validatePlanFields(plan); err != nil {
		return nil, err
	}
	if err := s.validatePlanDependencies(ctx, plan); err != nil {
		return nil, err
	}
	next, err := computeNextRun(plan.CronExpression, s.now())
	if err != nil {
		return nil, err
	}
	plan.NextRunAt = &next
	created, err := s.repo.Create(ctx, plan)
	if err != nil {
		return nil, err
	}
	return s.withRunning(created), nil
}

func (s *ScheduledQuestionTestService) Update(ctx context.Context, id int64, input ScheduledQuestionTestPlanInput) (*ScheduledQuestionTestPlan, error) {
	existing, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	plan := *existing
	mask := applyScheduledQuestionTestInput(&plan, input)
	if err := s.validatePlanFields(&plan); err != nil {
		return nil, err
	}
	if mask.AccountIDs || mask.QuestionIDs || (!existing.Enabled && plan.Enabled) {
		if err := s.validatePlanDependencies(ctx, &plan); err != nil {
			return nil, err
		}
	}
	if mask.CronExpression || (mask.Enabled && plan.Enabled) {
		next, err := computeNextRun(plan.CronExpression, s.now())
		if err != nil {
			return nil, err
		}
		plan.NextRunAt, mask.NextRunAt = &next, true
	}
	updated, err := s.repo.Update(ctx, &plan, mask)
	if err != nil {
		return nil, err
	}
	return s.withRunning(updated), nil
}

func (s *ScheduledQuestionTestService) Delete(ctx context.Context, id int64) error {
	if _, err := s.acquireExecution(id); err != nil {
		return err
	}
	defer s.releaseExecution(id)
	return s.repo.Delete(ctx, id)
}

func (s *ScheduledQuestionTestService) ListRuns(ctx context.Context, id int64, limit int) ([]*ScheduledQuestionTestRun, error) {
	plan, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	runs, err := s.repo.ListRuns(ctx, id, min(limit, 200))
	for _, run := range runs {
		if run.NotificationResults == nil {
			run.NotificationResults = []ScheduledQuestionTestNotificationResult{}
		}
		for i := range run.Summary {
			run.Summary[i].Error = sanitizeScheduledQuestionErrorText(run.Summary[i].Error, plan, 500)
		}
	}
	return runs, err
}

func (s *ScheduledQuestionTestService) RunNow(ctx context.Context, id int64) (*ScheduledQuestionTestAccepted, error) {
	baseCtx, err := s.acquireExecution(id)
	if err != nil {
		return nil, err
	}
	plan, err := s.GetByID(ctx, id)
	if err != nil {
		s.releaseExecution(id)
		return nil, err
	}
	go func() {
		defer s.releaseExecution(id)
		if _, err := s.runPlan(baseCtx, plan); err != nil {
			slog.Error("scheduled_question_test_background_failed", "plan_id", id, "error", err)
		}
	}()
	return &ScheduledQuestionTestAccepted{Accepted: true, PlanID: id}, nil
}

func (s *ScheduledQuestionTestService) runScheduledPlan(ctx context.Context, id int64) (*ScheduledQuestionTestRun, error) {
	return s.run(ctx, id, true)
}

func (s *ScheduledQuestionTestService) run(ctx context.Context, id int64, requireEnabled bool) (*ScheduledQuestionTestRun, error) {
	baseCtx, err := s.acquireExecution(id)
	if err != nil {
		return nil, err
	}
	defer s.releaseExecution(id)
	// Scheduled scans retain their caller cancellation and share the service shutdown signal.
	executionCtx, cancel := context.WithCancel(baseCtx)
	defer cancel()
	stopCancel := context.AfterFunc(ctx, cancel)
	defer stopCancel()
	if ctx.Err() != nil {
		cancel()
	}
	plan, err := s.GetByID(executionCtx, id)
	if err != nil {
		return nil, err
	}
	if requireEnabled && !plan.Enabled {
		return nil, scheduledQuestionValidation("enabled", "scheduled question test plan is disabled")
	}
	if requireEnabled && plan.NextRunAt != nil && plan.NextRunAt.After(s.now()) {
		return nil, nil
	}
	return s.runPlan(executionCtx, plan)
}

func (s *ScheduledQuestionTestService) acquireExecution(id int64) (context.Context, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, ErrScheduledQuestionTestStopped
	}
	if s.running[id] {
		return nil, ErrScheduledQuestionTestRunning
	}
	s.running[id] = true
	s.active.Add(1)
	return s.baseCtx, nil
}

func (s *ScheduledQuestionTestService) releaseExecution(id int64) {
	s.mu.Lock()
	delete(s.running, id)
	s.mu.Unlock()
	s.active.Done()
}

func (s *ScheduledQuestionTestService) runPlan(parent context.Context, plan *ScheduledQuestionTestPlan) (*ScheduledQuestionTestRun, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	run := &ScheduledQuestionTestRun{
		PlanID: plan.ID, CronExpression: plan.CronExpression, PlanUpdatedAt: plan.UpdatedAt,
		Status: "success", StartedAt: s.now(), TotalAccounts: len(plan.AccountIDs),
		NotificationsSuppressed: !plan.Enabled || !plan.NotifyEnabled,
		NotificationResults:     []ScheduledQuestionTestNotificationResult{},
	}
	items := make([]AccountBatchTestItemResult, 0, len(plan.AccountIDs)*len(plan.QuestionIDs))
	activePlan := *plan
	activePlan.AccountIDs = nil
	before := make(map[int64]*bool, len(plan.AccountIDs))
	skipped := make(map[int64]ScheduledQuestionTestAccountSummary)
	for _, id := range plan.AccountIDs {
		account, err := s.accounts.GetByID(ctx, id)
		if err == nil && account != nil {
			schedulable := account.Schedulable
			before[id] = &schedulable
			reason, resetAt := s.scheduledQuestionSkipReason(ctx, account, plan.ModelID)
			if reason != "" {
				skipped[id] = ScheduledQuestionTestAccountSummary{AccountID: id, AccountName: account.Name, Platform: account.Platform, Total: len(plan.QuestionIDs), Result: "skipped", SkipReason: reason, SkipResetAt: resetAt, SchedulableBefore: &schedulable, SchedulableAfter: &schedulable}
				continue
			}
		}
		activePlan.AccountIDs = append(activePlan.AccountIDs, id)
	}
	var runErr error
	if len(activePlan.AccountIDs) > 0 {
		var batchPlan *AccountBatchTestPlan
		batchPlan, runErr = s.batch.Prepare(ctx, AccountBatchTestInput{AccountIDs: activePlan.AccountIDs, QuestionIDs: plan.QuestionIDs, ModelID: plan.ModelID, captureStatus: true})
		if runErr == nil {
			runErr = s.batch.Run(ctx, batchPlan, func(event AccountBatchTestEvent) error {
				if event.Type == "item_result" {
					item, ok := event.Data.(AccountBatchTestItemResult)
					if !ok {
						return fmt.Errorf("unexpected batch item result type")
					}
					items = append(items, item)
				}
				return nil
			})
		}
	}
	if runErr != nil {
		run.Status, run.ErrorMessage = "failed", runErr.Error()
	}
	activeSummaries := summarizeScheduledQuestionTests(&activePlan, items, run.ErrorMessage)
	byID := make(map[int64]ScheduledQuestionTestAccountSummary, len(plan.AccountIDs))
	for _, summary := range activeSummaries {
		if !summary.Incomplete && scheduledQuestionAllErrors(summary.AccountID, len(plan.QuestionIDs), items) {
			account, err := s.accounts.GetByID(ctx, summary.AccountID)
			reason, resetAt := "", (*time.Time)(nil)
			if err == nil && account != nil {
				reason, resetAt = s.scheduledQuestionSkipReason(ctx, account, plan.ModelID)
			}
			if reason == "" && scheduledQuestionAllErrorsRateLimited(summary.AccountID, len(plan.QuestionIDs), items) {
				reason = "rate_limited"
			}
			if reason != "" {
				summary.Result, summary.SkipReason, summary.SkipResetAt = "skipped", reason, resetAt
				summary.Alerting, summary.Error, summary.Ratio, summary.StateAfter = false, "", nil, ""
			}
		}
		summary.SchedulableBefore = before[summary.AccountID]
		byID[summary.AccountID] = summary
	}
	for id, summary := range skipped {
		byID[id] = summary
	}
	for _, id := range plan.AccountIDs {
		summary := byID[id]
		run.Summary = append(run.Summary, summary)
		switch summary.Result {
		case "skipped":
			run.SkippedAccounts++
		case "passed":
			run.PassedAccounts++
		default:
			if summary.Alerting {
				run.AlertedAccounts++
			}
		}
	}
	// A cancelled/expired round still records its outcome without losing cancellation to upstreams.
	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(parent), 30*time.Second)
	defer persistCancel()
	run.FinishedAt = s.now()
	states, err := s.updateAlertStates(persistCtx, plan, run, ctx)
	if err != nil {
		run.Status = "failed"
		run.NotificationsSuppressed = true
		if run.ErrorMessage != "" {
			run.ErrorMessage += "; "
		}
		run.ErrorMessage += "failed to update alert states: " + err.Error()
		// A failed state read must not erase the round or overwrite old states.
		states = nil
	}
	s.refreshScheduledQuestionSchedulability(persistCtx, run)
	run.FinishedAt = s.now()
	next, err := computeNextRun(plan.CronExpression, run.FinishedAt)
	if err != nil {
		return nil, err
	}
	if states != nil {
		refreshTimeout := s.ownershipRefreshTimeout
		if refreshTimeout <= 0 {
			refreshTimeout = 10 * time.Second
		}
		refreshCtx, refreshCancel := context.WithTimeout(context.WithoutCancel(parent), refreshTimeout)
		s.refreshScheduledQuestionSummaryOwnership(refreshCtx, plan.ID, run)
		refreshCancel()
	}
	// Summary refresh is best-effort and must not consume the persistence budget.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(parent), 30*time.Second)
	defer finishCancel()
	return s.repo.FinishRun(finishCtx, run, states, next)
}

func (s *ScheduledQuestionTestService) refreshScheduledQuestionSummaryOwnership(ctx context.Context, planID int64, run *ScheduledQuestionTestRun) {
	states, err := s.repo.ListStates(ctx, planID)
	if err != nil {
		slog.Warn("scheduled_question_test_summary_ownership_refresh_failed", "plan_id", planID)
		return
	}
	byID := make(map[int64]bool, len(states))
	for _, state := range states {
		byID[state.AccountID] = state.AutoPaused
	}
	for i := range run.Summary {
		run.Summary[i].AutoPaused = byID[run.Summary[i].AccountID]
	}
}

func (s *ScheduledQuestionTestService) refreshScheduledQuestionSchedulability(ctx context.Context, run *ScheduledQuestionTestRun) {
	for i := range run.Summary {
		summary := &run.Summary[i]
		account, err := s.accounts.GetByID(ctx, summary.AccountID)
		if (err == nil && account == nil) || errors.Is(err, ErrAccountNotFound) {
			summary.SchedulableAfter = nil
			continue
		}
		if err == nil && account != nil {
			value := account.Schedulable
			summary.SchedulableAfter = &value
			continue
		}
		summary.SchedulableAfter = summary.SchedulableBefore
		if summary.ScheduleAction == "paused" || summary.ScheduleAction == "resumed" {
			value := summary.ScheduleAction == "resumed"
			summary.SchedulableAfter = &value
		}
	}
}

func scheduledQuestionAllErrors(accountID int64, questionCount int, items []AccountBatchTestItemResult) bool {
	if questionCount <= 0 {
		return false
	}
	count := 0
	for _, item := range items {
		if item.AccountID != accountID {
			continue
		}
		if item.Status != "error" || item.Judged {
			return false
		}
		count++
	}
	return count == questionCount
}
