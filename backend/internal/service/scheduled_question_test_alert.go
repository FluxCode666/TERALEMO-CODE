package service

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"math"
	"strings"
	"time"
)

func summarizeScheduledQuestionTests(plan *ScheduledQuestionTestPlan, items []AccountBatchTestItemResult, roundError string) []ScheduledQuestionTestAccountSummary {
	byAccount := make(map[int64][]AccountBatchTestItemResult, len(plan.AccountIDs))
	for _, item := range items {
		byAccount[item.AccountID] = append(byAccount[item.AccountID], item)
	}
	result := make([]ScheduledQuestionTestAccountSummary, 0, len(plan.AccountIDs))
	for _, id := range plan.AccountIDs {
		summary := ScheduledQuestionTestAccountSummary{AccountID: id, Total: len(plan.QuestionIDs), StateAfter: "ok"}
		accountItems := byAccount[id]
		errors := 0
		for _, item := range accountItems {
			summary.AccountName, summary.Platform = item.AccountName, item.Platform
			if item.Status == "error" {
				errors++
				if summary.Error == "" {
					summary.Error = item.Error
				}
				continue
			}
			if item.Judged {
				summary.Judged++
				if item.Status == "passed" {
					summary.Passed++
				}
			}
		}
		if missing := summary.Total - len(accountItems); missing > 0 {
			errors += missing
			summary.Error = roundError
			if summary.Error == "" {
				summary.Error = "Account test did not complete"
			}
		}
		switch {
		case summary.Judged > 0:
			ratio := int(math.Round(float64(summary.Passed) * 100 / float64(summary.Judged)))
			summary.Ratio, summary.Alerting = &ratio, ratio < plan.MinPassRatio
		case errors == summary.Total && summary.Total > 0:
			ratio := 0
			summary.Ratio, summary.Alerting = &ratio, true
			if summary.Error == "" {
				summary.Error = "All account tests failed"
			}
		}
		if summary.Alerting {
			summary.StateAfter = "alert"
		}
		result = append(result, summary)
	}
	return result
}

// scheduledQuestionAlertTransition decides notification intent without advancing the send timestamp.
func scheduledQuestionAlertTransition(previous *ScheduledQuestionTestAccountState, alerting bool, now time.Time, cooldown time.Duration) (next ScheduledQuestionTestAccountState, notification string) {
	if previous != nil {
		next = *previous
	}
	if alerting {
		next.State = "alert"
		if previous == nil || previous.State != "alert" || previous.LastAlertAt == nil || now.Sub(*previous.LastAlertAt) >= cooldown {
			notification = "alert"
		}
	} else {
		next.State, next.LastAlertAt = "ok", nil
		if previous != nil && previous.State == "alert" {
			notification = "recovered"
		}
	}
	next.UpdatedAt = now
	return next, notification
}

func (s *ScheduledQuestionTestService) updateAlertStates(ctx context.Context, plan *ScheduledQuestionTestPlan, run *ScheduledQuestionTestRun) ([]*ScheduledQuestionTestAccountState, error) {
	previous, err := s.repo.ListStates(ctx, plan.ID)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*ScheduledQuestionTestAccountState, len(previous))
	for _, state := range previous {
		byID[state.AccountID] = state
	}
	now := s.now()
	states := make([]*ScheduledQuestionTestAccountState, 0, len(run.Summary))
	notifications := map[string][]int{"alert": {}, "recovered": {}}
	for i, summary := range run.Summary {
		next, notification := scheduledQuestionAlertTransition(byID[summary.AccountID], summary.Alerting, now, time.Duration(plan.CooldownMinutes)*time.Minute)
		next.PlanID, next.AccountID = plan.ID, summary.AccountID
		states = append(states, &next)
		if notification != "" {
			notifications[notification] = append(notifications[notification], i)
		}
	}
	if !plan.Enabled || !plan.NotifyEnabled {
		run.NotificationsSuppressed = true
		return states, nil
	}
	current, err := s.GetByID(ctx, plan.ID)
	if err != nil {
		return nil, err
	}
	if !current.Enabled || !current.NotifyEnabled {
		run.NotificationsSuppressed = true
		return states, nil
	}
	for _, kind := range []string{"alert", "recovered"} {
		indices := notifications[kind]
		if len(indices) == 0 {
			continue
		}
		if s.sendScheduledQuestionNotification(ctx, plan, run, kind, indices) && kind == "alert" {
			for _, i := range indices {
				sentAt := now
				states[i].LastAlertAt = &sentAt
			}
		}
	}
	return states, nil
}

func (s *ScheduledQuestionTestService) sendScheduledQuestionNotification(ctx context.Context, plan *ScheduledQuestionTestPlan, run *ScheduledQuestionTestRun, kind string, indices []int) bool {
	if s.settings == nil || s.email == nil {
		slog.Warn("scheduled_question_test_notification_unavailable", "plan_id", plan.ID)
		return false
	}
	raw, err := s.settings.GetValue(ctx, SettingKeyAccountQuotaNotifyEmails)
	if err != nil {
		slog.Warn("scheduled_question_test_recipients_failed", "plan_id", plan.ID, "error", err)
		return false
	}
	recipients := filterVerifiedEmails(ParseNotifyEmails(raw))
	if len(recipients) == 0 {
		slog.Warn("scheduled_question_test_no_recipients", "plan_id", plan.ID)
		return false
	}
	siteName, err := s.settings.GetValue(ctx, SettingKeySiteName)
	if err != nil || strings.TrimSpace(siteName) == "" {
		siteName = defaultSiteName
	}
	label := "定时测试告警 / Scheduled test alert"
	if kind == "recovered" {
		label = "定时测试已恢复 / Scheduled test recovered"
	}
	subject := fmt.Sprintf("[%s] %s: %s", siteName, label, plan.Name)
	var body strings.Builder
	_, _ = fmt.Fprintf(&body, "<h2>%s</h2><p>Plan: %s<br>Round: %s<br>Threshold: %d%%</p><ul>", html.EscapeString(label), html.EscapeString(plan.Name), run.StartedAt.Format(time.RFC3339), plan.MinPassRatio)
	for _, i := range indices {
		summary := run.Summary[i]
		ratio := "unjudged"
		if summary.Ratio != nil {
			ratio = fmt.Sprintf("%d%%", *summary.Ratio)
		}
		// Upstream errors may contain provider response bodies; do not include them in mail.
		_, _ = fmt.Fprintf(&body, "<li>%s (ID %d): %d/%d judged passed, %s; threshold %d%%</li>", html.EscapeString(summary.AccountName), summary.AccountID, summary.Passed, summary.Judged, ratio, plan.MinPassRatio)
	}
	_, _ = body.WriteString("</ul>")
	sent := false
	for _, recipient := range recipients {
		for _, i := range indices {
			slog.Info("scheduled_question_test_notification_attempt", "plan_id", plan.ID, "account_id", run.Summary[i].AccountID, "kind", kind)
		}
		mailCtx, cancel := context.WithTimeout(ctx, emailSendTimeout)
		err := s.email.SendEmail(mailCtx, recipient, subject, body.String())
		cancel()
		for _, i := range indices {
			slog.Info("scheduled_question_test_notification_sent", "plan_id", plan.ID, "account_id", run.Summary[i].AccountID, "kind", kind, "success", err == nil)
		}
		if err != nil {
			slog.Warn("scheduled_question_test_email_failed", "plan_id", plan.ID, "error", err)
			continue
		}
		sent = true
	}
	return sent
}
