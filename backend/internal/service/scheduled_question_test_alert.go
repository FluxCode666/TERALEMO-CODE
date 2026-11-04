package service

import (
	"context"
	"math"
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
			summary.Incomplete = true
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
		summary.Error = sanitizeScheduledQuestionErrorText(summary.Error, plan, 500)
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

func (s *ScheduledQuestionTestService) updateAlertStates(persistCtx context.Context, plan *ScheduledQuestionTestPlan, run *ScheduledQuestionTestRun, roundContexts ...context.Context) ([]*ScheduledQuestionTestAccountState, error) {
	roundCtx := persistCtx
	if len(roundContexts) > 0 {
		roundCtx = roundContexts[0]
	}
	previous, err := s.repo.ListStates(persistCtx, plan.ID)
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
		run.Summary[i].AutoPaused = next.AutoPaused
		if notification != "" {
			notifications[notification] = append(notifications[notification], i)
		}
	}
	current, err := s.GetByID(persistCtx, plan.ID)
	if err != nil {
		return nil, err
	}
	if !current.Enabled || roundCtx.Err() != nil {
		run.NotificationsSuppressed = true
		return states, nil
	}
	run.NotificationsSuppressed = !current.NotifyEnabled
	// Cooldown and channels also honor edits made while the upstream round was running.
	if current.CooldownMinutes != plan.CooldownMinutes {
		notifications = map[string][]int{"alert": {}, "recovered": {}}
		for i, summary := range run.Summary {
			_, kind := scheduledQuestionAlertTransition(byID[summary.AccountID], summary.Alerting, now, time.Duration(current.CooldownMinutes)*time.Minute)
			if kind != "" {
				notifications[kind] = append(notifications[kind], i)
			}
		}
	}
	s.applyScheduledQuestionScheduling(persistCtx, roundCtx, current, run, states, now)
	// Notification delivery owns a separate budget and still stops with the round/service.
	stageTimeout := s.notificationTimeouts.Stage
	if stageTimeout <= 0 {
		stageTimeout = 3 * time.Minute
	}
	notifyCtx, cancelNotify := context.WithTimeout(roundCtx, stageTimeout)
	defer cancelNotify()
	if s.baseCtx != nil {
		stopCancel := context.AfterFunc(s.baseCtx, cancelNotify)
		defer stopCancel()
		if s.baseCtx.Err() != nil {
			cancelNotify()
		}
	}
	if current.NotifyEnabled {
		for _, kind := range []string{"alert", "recovered"} {
			indices := notifications[kind]
			if len(indices) == 0 {
				continue
			}
			if s.sendScheduledQuestionNotification(notifyCtx, current, run, kind, indices) && kind == "alert" {
				for _, i := range indices {
					sentAt := now
					states[i].LastAlertAt = &sentAt
				}
			}
		}
	}
	if current.ReportEnabled {
		s.sendScheduledQuestionNotification(notifyCtx, current, run, "report", nil)
	}
	return states, nil
}
