package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func (s *ScheduledQuestionTestService) applyScheduledQuestionScheduling(ctx, roundCtx context.Context, plan *ScheduledQuestionTestPlan, run *ScheduledQuestionTestRun, states []*ScheduledQuestionTestAccountState, now time.Time) error {
	active, allErrors := 0, true
	for _, summary := range run.Summary {
		if summary.Result == "skipped" {
			continue
		}
		active++
		if summary.Incomplete || summary.Judged != 0 || summary.Ratio == nil || !summary.Alerting || summary.Total == 0 {
			allErrors = false
		}
	}
	run.AutoPauseSkipped = plan.AutoPauseEnabled && active >= 2 && allErrors
	byID := make(map[int64]*ScheduledQuestionTestAccountState, len(states))
	for _, state := range states {
		byID[state.AccountID] = state
	}
	var failures []error
	for i := range run.Summary {
		summary := &run.Summary[i]
		state := byID[summary.AccountID]
		if summary.Result == "skipped" || state == nil {
			continue
		}
		if roundCtx.Err() != nil || ctx.Err() != nil {
			run.NotificationsSuppressed = true
			return errors.Join(append(failures, roundCtx.Err(), ctx.Err())...)
		}
		pause := plan.AutoPauseEnabled && !run.AutoPauseSkipped && summary.Alerting && !summary.Incomplete
		resume := plan.AutoResumeEnabled && scheduledQuestionAccountResult(*summary) == "passed"
		action, err := s.repo.ApplyAccountVerdict(ctx, plan.ID, summary.AccountID, state.State, pause, resume, now, func(actionCtx context.Context, value bool) error {
			if err := roundCtx.Err(); err != nil {
				return err
			}
			return s.accounts.SetSchedulable(actionCtx, summary.AccountID, value)
		})
		if err != nil {
			failures = append(failures, err)
			switch {
			case pause:
				summary.ScheduleAction = "pause_failed"
			case resume:
				summary.ScheduleAction = "resume_failed"
			}
			slog.Warn("scheduled_question_test_verdict_failed", "plan_id", plan.ID, "account_id", summary.AccountID)
			continue
		}
		summary.ScheduleAction = action
		switch action {
		case "paused":
			state.AutoPaused, state.AutoPausedAt = true, &now
			run.PausedAccounts++
		case "resumed":
			state.AutoPaused, state.AutoPausedAt = false, nil
			run.ResumedAccounts++
		}
		summary.AutoPaused = state.AutoPaused
	}
	return errors.Join(failures...)
}

func (s *ScheduledQuestionTestService) TestFeishu(ctx context.Context, input ScheduledQuestionTestFeishuTestInput) error {
	webhook, secret := strings.TrimSpace(input.WebhookURL), ""
	if input.PlanID > 0 {
		plan, err := s.GetByID(ctx, input.PlanID)
		if err != nil {
			return err
		}
		if webhook == "" {
			webhook = plan.FeishuWebhookURL
		}
		if input.Secret == nil {
			secret = plan.FeishuSecret
		}
	}
	if input.Secret != nil {
		secret = strings.TrimSpace(*input.Secret)
	}
	if err := validateScheduledQuestionFeishuWebhook(webhook); err != nil {
		return err
	}
	if webhook == "" {
		return scheduledQuestionValidation("feishu_webhook_url", "feishu webhook is required")
	}
	if utf8.RuneCountInString(secret) > 200 {
		return scheduledQuestionValidation("feishu_secret", "feishu_secret must contain at most 200 characters")
	}
	if s.feishu == nil {
		return infraerrors.New(502, "FEISHU_TEST_FAILED", "feishu service unavailable")
	}
	siteName := sanitizeScheduledQuestionErrorText(s.notificationSiteName(ctx), &ScheduledQuestionTestPlan{FeishuWebhookURL: webhook, FeishuSecret: secret}, 120)
	success, detail := s.feishu.Send(ctx, webhook, secret, scheduledQuestionFeishuTestNotification(siteName, s.now()))
	if !success {
		return infraerrors.New(502, "FEISHU_TEST_FAILED", sanitizeScheduledQuestionNotificationDetail(detail, webhook, secret))
	}
	return nil
}
