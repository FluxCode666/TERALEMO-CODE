package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/textproto"
	"sort"
	"strings"
	"sync"
	"time"
)

type scheduledQuestionNotificationTimeouts struct {
	Email  time.Duration
	Feishu time.Duration
	Stage  time.Duration
}

func (s *ScheduledQuestionTestService) notificationSiteName(ctx context.Context) string {
	if s.settings != nil {
		if name, err := s.settings.GetValue(ctx, SettingKeySiteName); err == nil && strings.TrimSpace(name) != "" {
			return strings.TrimSpace(name)
		}
	}
	return defaultSiteName
}

func (s *ScheduledQuestionTestService) sendScheduledQuestionNotification(ctx context.Context, plan *ScheduledQuestionTestPlan, run *ScheduledQuestionTestRun, kind string, indices []int) bool {
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	siteName := defaultSiteName
	if ctx.Err() == nil {
		siteName = s.notificationSiteName(ctx)
	}
	notification := buildScheduledQuestionNotification(siteName, plan, run, kind, indices, now)
	type delivery struct {
		result  ScheduledQuestionTestNotificationResult
		timeout time.Duration
		send    func(context.Context) (bool, string)
	}
	deliveries := make([]delivery, 0, 2)
	if plan.EmailEnabled {
		timeout := s.notificationTimeouts.Email
		if timeout <= 0 {
			timeout = time.Minute
		}
		deliveries = append(deliveries, delivery{
			result: ScheduledQuestionTestNotificationResult{Kind: kind, Channel: "email"}, timeout: timeout,
			send: func(channelCtx context.Context) (bool, string) {
				return s.sendScheduledQuestionEmail(channelCtx, plan, run, kind, indices, notification)
			},
		})
	}
	if plan.FeishuEnabled {
		timeout := s.notificationTimeouts.Feishu
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		deliveries = append(deliveries, delivery{
			result: ScheduledQuestionTestNotificationResult{Kind: kind, Channel: "feishu"}, timeout: timeout,
			send: func(channelCtx context.Context) (bool, string) {
				if strings.TrimSpace(plan.FeishuWebhookURL) == "" {
					return false, "invalid feishu webhook URL"
				}
				if s.feishu == nil {
					return false, "feishu service unavailable"
				}
				return s.feishu.Send(channelCtx, plan.FeishuWebhookURL, plan.FeishuSecret, notification)
			},
		})
	}
	var workers sync.WaitGroup
	for i := range deliveries {
		workers.Add(1)
		go func() {
			defer workers.Done()
			item := &deliveries[i]
			item.result.Detail = "notification failed"
			defer func() {
				if recover() != nil {
					item.result.Success, item.result.Detail = false, "notification failed"
				}
			}()
			channelCtx, cancel := context.WithTimeout(ctx, item.timeout)
			defer cancel()
			if channelCtx.Err() == nil {
				item.result.Success, item.result.Detail = item.send(channelCtx)
			}
			if err := channelCtx.Err(); err != nil && !item.result.Success && !strings.HasPrefix(item.result.Detail, "sent ") {
				item.result.Success, item.result.Detail = false, scheduledQuestionNotificationContextDetail(err)
			}
		}()
	}
	// Both clients honor cancellation; join every worker before touching persisted results.
	workers.Wait()
	sent := false
	for _, item := range deliveries {
		result := item.result
		result.Detail = sanitizeScheduledQuestionNotificationDetail(result.Detail, plan.FeishuWebhookURL, plan.FeishuSecret)
		run.NotificationResults = append(run.NotificationResults, result)
		if !result.Success {
			slog.Warn("scheduled_question_test_notification_failed", "plan_id", plan.ID, "kind", kind, "channel", result.Channel, "detail", result.Detail)
		}
		sent = sent || result.Success
	}
	return sent
}

func scheduledQuestionNotificationContextDetail(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "aborted"
}

func (s *ScheduledQuestionTestService) sendScheduledQuestionEmail(ctx context.Context, plan *ScheduledQuestionTestPlan, run *ScheduledQuestionTestRun, kind string, indices []int, notification scheduledQuestionNotification) (bool, string) {
	if s.settings == nil || s.email == nil {
		return false, "email service unavailable"
	}
	raw, err := s.settings.GetValue(ctx, SettingKeyAccountQuotaNotifyEmails)
	if err != nil {
		return false, "email recipients unavailable"
	}
	recipients := filterVerifiedEmails(ParseNotifyEmails(raw))
	if len(recipients) == 0 {
		return false, "no verified recipients"
	}
	var body strings.Builder
	_, _ = fmt.Fprintf(&body, "<h2>%s</h2>", html.EscapeString(notification.Title))
	for _, line := range notification.Lines {
		_, _ = fmt.Fprintf(&body, "<p>%s</p>", html.EscapeString(line))
	}
	sent := 0
	failure := ""
	for _, recipient := range recipients {
		if ctx.Err() != nil {
			break
		}
		scheduledQuestionNotificationAccountLog(plan.ID, run, kind, indices, "scheduled_question_test_notification_attempt", false)
		mailCtx, cancel := context.WithTimeout(ctx, emailSendTimeout)
		err := s.email.SendEmail(mailCtx, recipient, notification.Title, body.String())
		if err != nil {
			cause := err
			if mailCtx.Err() != nil {
				cause = mailCtx.Err()
			}
			category := scheduledQuestionEmailFailureCategory(cause)
			if failure != "timeout" || category == "timeout" {
				failure = category
			}
		}
		cancel()
		scheduledQuestionNotificationAccountLog(plan.ID, run, kind, indices, "scheduled_question_test_notification_sent", err == nil)
		if err == nil {
			sent++
		}
	}
	if ctx.Err() != nil {
		failure = scheduledQuestionNotificationContextDetail(ctx.Err())
	}
	detail := fmt.Sprintf("sent %d/%d recipients", sent, len(recipients))
	if failure != "" {
		detail += " (" + failure + ")"
	}
	return sent > 0, detail
}

func scheduledQuestionEmailFailureCategory(err error) string {
	var networkError net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "aborted"
	}
	var smtpError *textproto.Error
	if errors.As(err, &smtpError) && smtpError.Code == 535 {
		return "auth failed"
	}
	// Only choose fixed categories from internal errors; never return SMTP reply text.
	if strings.Contains(strings.ToLower(err.Error()), "smtp auth:") {
		return "auth failed"
	}
	if errors.As(err, &networkError) {
		return "connection failed"
	}
	for _, prefix := range []string{"smtp dial:", "tls dial:", "new smtp client:", "set smtp deadline:"} {
		if strings.Contains(strings.ToLower(err.Error()), prefix) {
			return "connection failed"
		}
	}
	return "rejected"
}

func scheduledQuestionNotificationAccountLog(planID int64, run *ScheduledQuestionTestRun, kind string, indices []int, message string, success bool) {
	for _, i := range indices {
		if i >= 0 && i < len(run.Summary) {
			slog.Info(message, "plan_id", planID, "account_id", run.Summary[i].AccountID, "kind", kind, "channel", "email", "success", success)
		}
	}
}

func buildScheduledQuestionNotification(siteName string, plan *ScheduledQuestionTestPlan, run *ScheduledQuestionTestRun, kind string, indices []int, now time.Time) scheduledQuestionNotification {
	label, color := "定时测试告警 / Scheduled test alert", "red"
	if kind == "recovered" {
		label, color = "定时测试已恢复 / Scheduled test recovered", "green"
	}
	if kind == "report" {
		label, color = "定时测试报告 / Scheduled test report", "green"
		if run.AlertedAccounts > 0 || run.PausedAccounts > 0 {
			color = "orange"
		}
		if run.Status == "failed" {
			color = "red"
		}
	}
	notification := scheduledQuestionNotification{
		Title: fmt.Sprintf("[%s] %s: %s", siteName, label, plan.Name), Color: color,
		Lines: []string{fmt.Sprintf("Plan: %s", plan.Name)},
	}
	if kind != "report" {
		notification.Lines = append(notification.Lines,
			"Round: "+run.StartedAt.Format(time.RFC3339),
			fmt.Sprintf("Threshold: %d%%", plan.MinPassRatio),
		)
		notification.DetailStart = len(notification.Lines)
		for _, i := range indices {
			if i < 0 || i >= len(run.Summary) {
				continue
			}
			summary := run.Summary[i]
			notification.Lines = append(notification.Lines, fmt.Sprintf("%s (ID %d): %d/%d judged passed, %s; threshold %d%%",
				summary.AccountName, summary.AccountID, summary.Passed, summary.Judged, scheduledQuestionSummaryRatio(summary), plan.MinPassRatio))
		}
		return notification
	}
	finishedAt := run.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = now
	}
	duration := finishedAt.Sub(run.StartedAt)
	if duration < 0 {
		duration = 0
	}
	notification.Lines = append(notification.Lines,
		"开始时间 / Started: "+run.StartedAt.In(time.Local).Format("2006-01-02 15:04:05 -07:00"),
		"耗时 / Duration: "+duration.Round(time.Millisecond).String(),
		"轮次状态 / Status: "+run.Status,
		fmt.Sprintf("阈值 / Threshold: %d%%", plan.MinPassRatio),
	)
	if run.Status == "failed" && run.ErrorMessage != "" {
		detail := sanitizeScheduledQuestionErrorText(run.ErrorMessage, plan, 300)
		notification.Lines = append(notification.Lines, "轮次错误 / Round error: "+detail)
	}
	manual, actionFailures := 0, 0
	for _, summary := range run.Summary {
		if summary.Ratio == nil && summary.Error == "" {
			manual++
		}
		if summary.ScheduleAction == "pause_failed" || summary.ScheduleAction == "resume_failed" {
			actionFailures++
		}
	}
	notification.Lines = append(notification.Lines,
		fmt.Sprintf("通过账号 / Passed: %d · 告警 / Alerting: %d · 需人工 / Manual: %d · 总账号 / Total: %d", run.PassedAccounts, run.AlertedAccounts, manual, run.TotalAccounts),
		fmt.Sprintf("自动暂停 / Paused: %d · 自动恢复 / Resumed: %d · 暂停/恢复失败 / Action failures: %d", run.PausedAccounts, run.ResumedAccounts, actionFailures),
	)
	if run.AutoPauseSkipped {
		notification.Lines = append(notification.Lines, "所有账号全部题目出错，疑似系统/网络故障，已跳过自动暂停 / Auto-pause skipped: all accounts errored; suspected system/network failure")
	}
	ordered := append([]ScheduledQuestionTestAccountSummary{}, run.Summary...)
	rank := func(summary ScheduledQuestionTestAccountSummary) int {
		if summary.ScheduleAction != "" {
			return 0
		}
		if summary.Alerting {
			return 1
		}
		return 2
	}
	sort.SliceStable(ordered, func(i, j int) bool { return rank(ordered[i]) < rank(ordered[j]) })
	notification.DetailStart = len(notification.Lines)
	for _, summary := range ordered {
		notification.Lines = append(notification.Lines, fmt.Sprintf("%s (%d) · 通过 %d/已判定 %d · %s · %s · %s",
			summary.AccountName, summary.AccountID, summary.Passed, summary.Judged,
			scheduledQuestionSummaryRatio(summary), scheduledQuestionSummaryStatus(summary), scheduledQuestionSummaryAction(summary)))
	}
	return notification
}

func scheduledQuestionSummaryRatio(summary ScheduledQuestionTestAccountSummary) string {
	if summary.Ratio != nil {
		return fmt.Sprintf("%d%%", *summary.Ratio)
	}
	return "unjudged"
}

func scheduledQuestionSummaryStatus(summary ScheduledQuestionTestAccountSummary) string {
	if summary.Incomplete {
		return "未完成"
	}
	if summary.Error != "" {
		return "出错"
	}
	if summary.Alerting {
		return "告警"
	}
	if summary.Ratio == nil {
		return "需人工"
	}
	return "通过"
}

func scheduledQuestionSummaryAction(summary ScheduledQuestionTestAccountSummary) string {
	switch summary.ScheduleAction {
	case "paused":
		return "已暂停"
	case "resumed":
		return "已恢复"
	case "pause_failed":
		return "暂停失败"
	case "resume_failed":
		return "恢复失败"
	default:
		if summary.AutoPaused {
			return "由本计划暂停中"
		}
		return "无调度动作"
	}
}
