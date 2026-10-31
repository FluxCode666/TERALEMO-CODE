package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/robfig/cron/v3"
)

// ScheduledQuestionTestRunnerService runs alongside the original connectivity runner.
type ScheduledQuestionTestRunnerService struct {
	repo      ScheduledQuestionTestRepository
	service   *ScheduledQuestionTestService
	cfg       *config.Config
	cron      *cron.Cron
	cancel    context.CancelFunc
	startOnce sync.Once
	stopOnce  sync.Once
}

func NewScheduledQuestionTestRunnerService(repo ScheduledQuestionTestRepository, service *ScheduledQuestionTestService, cfg *config.Config) *ScheduledQuestionTestRunnerService {
	return &ScheduledQuestionTestRunnerService{repo: repo, service: service, cfg: cfg}
}

func (s *ScheduledQuestionTestRunnerService) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		s.service.Start()
		loc := time.Local
		if s.cfg != nil {
			if configured, err := time.LoadLocation(s.cfg.Timezone); err == nil {
				loc = configured
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		scheduler := cron.New(cron.WithParser(scheduledTestCronParser), cron.WithLocation(loc), cron.WithChain(cron.SkipIfStillRunning(cron.DefaultLogger)))
		if _, err := scheduler.AddFunc("* * * * *", func() { s.runScheduled(ctx) }); err != nil {
			cancel()
			slog.Error("scheduled_question_test_runner_start_failed", "error", err)
			return
		}
		s.cron = scheduler
		scheduler.Start()
		slog.Info("scheduled_question_test_runner_started", "interval", "every minute")
	})
}

func (s *ScheduledQuestionTestRunnerService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		s.service.Stop()
		if s.cron != nil {
			ctx := s.cron.Stop()
			select {
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
				slog.Warn("scheduled_question_test_runner_stop_timed_out")
			}
		}
	})
}

func (s *ScheduledQuestionTestRunnerService) runScheduled(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	plans, err := s.repo.ListDue(queryCtx, time.Now())
	cancel()
	if err != nil {
		slog.Warn("scheduled_question_test_due_query_failed", "error", err)
		return
	}
	workers := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for _, plan := range plans {
		if !plan.Enabled || ctx.Err() != nil {
			continue
		}
		select {
		case workers <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			defer func() { <-workers }()
			if _, err := s.service.runScheduledPlan(ctx, id); err != nil && !errors.Is(err, ErrScheduledQuestionTestRunning) {
				slog.Warn("scheduled_question_test_execution_failed", "plan_id", id, "error", err)
			}
		}(plan.ID)
	}
	wg.Wait()
}
