package service

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func (s *ScheduledQuestionTestService) applyScheduledQuestionScheduling(ctx, roundCtx context.Context, plan *ScheduledQuestionTestPlan, run *ScheduledQuestionTestRun, states []*ScheduledQuestionTestAccountState, now time.Time) {
	allErrors := len(run.Summary) >= 2
	for _, summary := range run.Summary {
		if summary.Incomplete || summary.Judged != 0 || summary.Ratio == nil || !summary.Alerting || summary.Total == 0 {
			allErrors = false
			break
		}
	}
	run.AutoPauseSkipped = plan.AutoPauseEnabled && allErrors
	for i := range run.Summary {
		summary, state := &run.Summary[i], states[i]
		if roundCtx.Err() != nil || ctx.Err() != nil {
			run.NotificationsSuppressed = true
			return
		}
		pause := plan.AutoPauseEnabled && !allErrors && summary.Alerting && !summary.Incomplete
		resume := plan.AutoResumeEnabled && state.AutoPaused && !summary.Alerting && summary.Ratio != nil && !summary.Incomplete
		if !pause && !resume {
			continue
		}
		account, err := s.accounts.GetByID(ctx, summary.AccountID)
		if err != nil || account == nil {
			continue
		}
		if roundCtx.Err() != nil || ctx.Err() != nil {
			run.NotificationsSuppressed = true
			return
		}
		wasSchedulable := account.Schedulable
		if pause && !wasSchedulable {
			claimed, err := s.repo.ClaimAutoPauseIfOtherHeld(ctx, plan.ID, account.ID, &now)
			if err != nil {
				summary.ScheduleAction = "pause_failed"
				slog.Warn("scheduled_question_test_pause_join_failed", "plan_id", plan.ID, "account_id", account.ID)
			} else if claimed {
				state.AutoPaused, state.AutoPausedAt = true, &now
			} else {
				latest, err := s.accounts.GetByID(ctx, account.ID)
				if err == nil && latest != nil {
					account, wasSchedulable = latest, latest.Schedulable
				}
			}
		}
		if pause && wasSchedulable {
			if roundCtx.Err() != nil || ctx.Err() != nil {
				run.NotificationsSuppressed = true
				return
			}
			if err := s.repo.SetAutoPaused(ctx, plan.ID, account.ID, true, &now); err != nil {
				summary.ScheduleAction = "pause_failed"
				slog.Warn("scheduled_question_test_pause_ownership_failed", "plan_id", plan.ID, "account_id", account.ID)
				continue
			}
			if roundCtx.Err() != nil {
				run.NotificationsSuppressed = true
				if rollbackErr := s.repo.SetAutoPaused(ctx, plan.ID, account.ID, false, nil); rollbackErr != nil {
					slog.Warn("scheduled_question_test_pause_ownership_rollback_failed", "plan_id", plan.ID, "account_id", account.ID)
				}
				return
			}
			if err := s.accounts.SetSchedulable(ctx, account.ID, false); err != nil {
				summary.ScheduleAction = "pause_failed"
				slog.Warn("scheduled_question_test_pause_failed", "plan_id", plan.ID, "account_id", account.ID)
				state.AutoPaused, state.AutoPausedAt = false, nil
				if rollbackErr := s.repo.SetAutoPaused(ctx, plan.ID, account.ID, false, nil); rollbackErr != nil {
					slog.Warn("scheduled_question_test_pause_ownership_rollback_failed", "plan_id", plan.ID, "account_id", account.ID)
				}
			} else {
				state.AutoPaused, state.AutoPausedAt = true, &now
				summary.ScheduleAction = "paused"
				run.PausedAccounts++
			}
		}
		if resume {
			shouldResume, err := s.repo.ReleaseAutoPause(ctx, plan.ID, account.ID)
			if err != nil {
				summary.ScheduleAction = "resume_failed"
				slog.Warn("scheduled_question_test_resume_ownership_failed", "plan_id", plan.ID, "account_id", account.ID)
				continue
			}
			state.AutoPaused, state.AutoPausedAt = false, nil
			summary.AutoPaused = false
			if !shouldResume {
				continue
			}
			if !account.Schedulable {
				if err := s.accounts.SetSchedulable(ctx, account.ID, true); err != nil {
					summary.ScheduleAction = "resume_failed"
					slog.Warn("scheduled_question_test_resume_failed", "plan_id", plan.ID, "account_id", account.ID)
					if claimErr := s.repo.SetAutoPaused(ctx, plan.ID, account.ID, true, &now); claimErr != nil {
						slog.Warn("scheduled_question_test_resume_reclaim_failed", "plan_id", plan.ID, "account_id", account.ID)
					} else {
						state.AutoPaused, state.AutoPausedAt = true, &now
					}
					summary.AutoPaused = state.AutoPaused
					continue
				}
				summary.ScheduleAction = "resumed"
				run.ResumedAccounts++
			}
			state.AutoPaused, state.AutoPausedAt = false, nil
		}
		summary.AutoPaused = state.AutoPaused
	}
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
	success, detail := s.feishu.Send(ctx, webhook, secret, scheduledQuestionNotification{
		Title: "定时测试通知测试 / Test notification", Color: "blue",
		Lines: []string{s.notificationSiteName(ctx), s.now().In(time.Local).Format("2006-01-02 15:04:05 -07:00")}, DetailStart: 2,
	})
	if !success {
		return infraerrors.New(502, "FEISHU_TEST_FAILED", sanitizeScheduledQuestionNotificationDetail(detail, webhook, secret))
	}
	return nil
}
