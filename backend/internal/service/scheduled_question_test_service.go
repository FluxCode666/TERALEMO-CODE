package service

import (
	"context"
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
	repo      ScheduledQuestionTestRepository
	accounts  AccountRepository
	questions *AccountTestQuestionService
	batch     scheduledQuestionBatchTester
	settings  SettingRepository
	email     scheduledQuestionEmailSender
	now       func() time.Time
	mu        sync.Mutex
	running   map[int64]bool
	baseCtx   context.Context
	cancel    context.CancelFunc
	stopped   bool
	active    sync.WaitGroup
}

func NewScheduledQuestionTestService(repo ScheduledQuestionTestRepository, accounts AccountRepository, questions *AccountTestQuestionService, batch *AccountBatchTestService, settings SettingRepository, email *EmailService) *ScheduledQuestionTestService {
	svc := &ScheduledQuestionTestService{
		repo: repo, accounts: accounts, questions: questions, batch: batch, settings: settings,
		now: time.Now, running: make(map[int64]bool),
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
	plan := &ScheduledQuestionTestPlan{Enabled: true, MinPassRatio: 80, CooldownMinutes: 360, NotifyEnabled: true}
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
	return s.repo.Create(ctx, plan)
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
	return s.repo.Update(ctx, &plan, mask)
}

func (s *ScheduledQuestionTestService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

func (s *ScheduledQuestionTestService) ListRuns(ctx context.Context, id int64, limit int) ([]*ScheduledQuestionTestRun, error) {
	if _, err := s.GetByID(ctx, id); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	return s.repo.ListRuns(ctx, id, min(limit, 200))
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
	}
	items := make([]AccountBatchTestItemResult, 0, len(plan.AccountIDs)*len(plan.QuestionIDs))
	batchPlan, runErr := s.batch.Prepare(ctx, AccountBatchTestInput{AccountIDs: plan.AccountIDs, QuestionIDs: plan.QuestionIDs, ModelID: plan.ModelID})
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
	if runErr != nil {
		run.Status, run.ErrorMessage = "failed", runErr.Error()
	}
	run.Summary = summarizeScheduledQuestionTests(plan, items, run.ErrorMessage)
	for _, summary := range run.Summary {
		if summary.Alerting {
			run.AlertedAccounts++
		} else if summary.Ratio != nil {
			run.PassedAccounts++
		}
	}
	// A cancelled/expired round still records its outcome without losing cancellation to upstreams.
	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(parent), 30*time.Second)
	defer persistCancel()
	notificationPlan := *plan
	if ctx.Err() != nil {
		notificationPlan.NotifyEnabled = false
	}
	states, err := s.updateAlertStates(persistCtx, &notificationPlan, run)
	if err != nil {
		run.Status = "failed"
		if run.ErrorMessage != "" {
			run.ErrorMessage += "; "
		}
		run.ErrorMessage += "failed to update alert states: " + err.Error()
		// A failed state read must not erase the round or overwrite old states.
		states = nil
	}
	persistCancel()
	run.FinishedAt = s.now()
	next, err := computeNextRun(plan.CronExpression, run.FinishedAt)
	if err != nil {
		return nil, err
	}
	// Slow SMTP must not consume the deadline reserved for storing the round.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(parent), 30*time.Second)
	defer finishCancel()
	return s.repo.FinishRun(finishCtx, run, states, next)
}
