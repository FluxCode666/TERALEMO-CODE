//go:build unit

package service

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScheduledQuestionNotificationTableRows(t *testing.T) {
	now := time.Date(2026, time.November, 5, 10, 30, 0, 0, time.Local)
	for _, tc := range []struct {
		name, result, color, ratio, note string
		summary                          ScheduledQuestionTestAccountSummary
	}{
		{name: "passed", result: "通过", color: "green", ratio: "2/2 (100%)", summary: ScheduledQuestionTestAccountSummary{Passed: 2, Judged: 2, Ratio: scheduledQuestionPtr(100)}},
		{name: "failed", result: "未通过", color: "red", ratio: "1/2 (50%)", summary: ScheduledQuestionTestAccountSummary{Passed: 1, Judged: 2, Ratio: scheduledQuestionPtr(50), Alerting: true}},
		{name: "manual", result: "需人工", color: "orange", ratio: "—"},
		{name: "error", result: "出错", color: "red", ratio: "—", note: "出错", summary: ScheduledQuestionTestAccountSummary{Error: "private upstream error"}},
		{name: "incomplete", result: "未完成", color: "orange", ratio: "—", note: "出错", summary: ScheduledQuestionTestAccountSummary{Incomplete: true, Error: "private upstream error"}},
		{name: "quota permanent", result: "已跳过", color: "neutral", ratio: "—", note: "配额已用尽；需管理员调整配额", summary: ScheduledQuestionTestAccountSummary{Result: "skipped", SkipReason: "quota_exceeded"}},
		{name: "quota reset", result: "已跳过", color: "neutral", ratio: "—", note: "配额已用尽；预计 11-05 10:30 恢复", summary: ScheduledQuestionTestAccountSummary{Result: "skipped", SkipReason: "quota_exceeded", SkipResetAt: &now}},
		{name: "window", result: "已跳过", color: "neutral", ratio: "—", note: "5h 窗口费用达到阈值；预计 11-05 10:30 恢复", summary: ScheduledQuestionTestAccountSummary{Result: "skipped", SkipReason: "window_cost_exceeded", SkipResetAt: &now}},
		{name: "rate limit", result: "已跳过", color: "neutral", ratio: "—", note: "上游限流中", summary: ScheduledQuestionTestAccountSummary{Result: "skipped", SkipReason: "rate_limited"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.summary.AccountID, tc.summary.AccountName = 42, "account"
			tc.summary.SchedulableBefore, tc.summary.SchedulableAfter = scheduledQuestionPtr(true), scheduledQuestionPtr(false)
			row := scheduledQuestionNotificationAccountRow(tc.summary, &ScheduledQuestionTestPlan{})
			require.Equal(t, "account (42)", row.Account)
			require.Equal(t, tc.result, row.Result)
			require.Equal(t, tc.color, row.ResultColor)
			require.Equal(t, tc.ratio, row.Ratio)
			require.Equal(t, "开", row.Before)
			require.Equal(t, "关", row.After)
			require.Equal(t, "—", row.Action)
			require.Equal(t, tc.note, row.Note)
		})
	}
}

func TestScheduledQuestionNotificationTableActions(t *testing.T) {
	for _, tc := range []struct{ action, text, color string }{
		{action: "paused", text: "已暂停", color: "red"},
		{action: "resumed", text: "已恢复", color: "green"},
		{action: "pause_failed", text: "暂停失败", color: "red"},
		{action: "resume_failed", text: "恢复失败", color: "red"},
		{action: "resume_blocked", text: "其它计划未通过，未恢复", color: "orange"},
		{text: "—", color: "neutral"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			row := scheduledQuestionNotificationAccountRow(ScheduledQuestionTestAccountSummary{ScheduleAction: tc.action}, nil)
			require.Equal(t, tc.text, row.Action)
			require.Equal(t, tc.color, row.ActionColor)
			require.Equal(t, "—", row.Before)
			require.Equal(t, "—", row.After)
			if tc.action == "resume_blocked" {
				require.Equal(t, "其它计划判定未通过", row.Note)
			}
		})
	}
	row := scheduledQuestionNotificationAccountRow(ScheduledQuestionTestAccountSummary{
		Result: "passed", ScheduleAction: "resume_blocked", Error: "private partial upstream failure",
	}, nil)
	require.Equal(t, "其它计划判定未通过；出错", row.Note, "partial errors must not hide the blocked-resume explanation")
}

func TestScheduledQuestionNotificationAllKindsUseFilteredSortedTables(t *testing.T) {
	now := time.Now()
	run := &ScheduledQuestionTestRun{StartedAt: now, TotalAccounts: 8, AutoPauseSkipped: true, Summary: []ScheduledQuestionTestAccountSummary{
		{AccountID: 9, Result: "passed"},
		{AccountID: 7, Result: "skipped", SkipReason: "rate_limited"},
		{AccountID: 6, Result: "failed"},
		{AccountID: 5, Result: "error"},
		{AccountID: 4, Result: "skipped", SkipReason: "quota_exceeded"},
		{AccountID: 3, Result: "passed", ScheduleAction: "resumed"},
		{AccountID: 2, Result: "failed", ScheduleAction: "paused"},
		{AccountID: 1, Result: "unjudged"},
	}}
	for _, kind := range []string{"report", "alert", "recovered"} {
		notification := buildScheduledQuestionNotification("Site", &ScheduledQuestionTestPlan{Name: "plan", MinPassRatio: 80}, run, kind, []int{2, 6, -1, 100}, now)
		body := strings.Join(notification.Lines, "\n")
		for _, text := range []string{"Plan: plan", "Round:", "Duration:", "Threshold: 80%", "Passed:", "Failed:", "Manual:", "Errors:", "Skipped:", "Total: 8", "Paused:", "Resumed:", "Auto-pause skipped"} {
			require.Contains(t, body, text)
		}
		if kind == "report" {
			require.Len(t, notification.Rows, 8)
			require.Equal(t, []string{" (2)", " (3)", " (5)", " (6)", " (4)", " (7)", " (1)", " (9)"}, notificationAccountNames(notification.Rows))
		} else {
			require.Equal(t, []string{" (2)", " (6)"}, notificationAccountNames(notification.Rows))
		}
	}
	require.Equal(t, int64(9), run.Summary[0].AccountID, "notification sorting must not mutate persisted summaries")
}

func notificationAccountNames(rows []scheduledQuestionNotificationRow) []string {
	accounts := make([]string, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, row.Account)
	}
	return accounts
}

func TestScheduledQuestionNotificationEmailTableEscapesEveryCell(t *testing.T) {
	unsafe := `<img src=x onerror="steal()"> & **<at id=all>**`
	var body strings.Builder
	writeScheduledQuestionNotificationTable(&body, []scheduledQuestionNotificationRow{{
		Account: unsafe, Result: unsafe, Ratio: unsafe, Before: unsafe, After: unsafe, Action: unsafe, Note: unsafe, ResultColor: "red",
	}})
	require.Contains(t, body.String(), "<table")
	require.Contains(t, body.String(), "<th")
	require.Contains(t, body.String(), "border:1px solid")
	require.Contains(t, body.String(), "background-color:#f1f5f9")
	require.Contains(t, body.String(), "color:#b91c1c")
	require.Equal(t, 7, strings.Count(body.String(), "&lt;img"))
	require.NotContains(t, body.String(), "<img")
}

func TestScheduledQuestionNotificationRedactsSecretsInUserControlledNames(t *testing.T) {
	plan := &ScheduledQuestionTestPlan{Name: scheduledQuestionTestWebhook + " secret-value", FeishuWebhookURL: scheduledQuestionTestWebhook, FeishuSecret: "secret-value"}
	run := &ScheduledQuestionTestRun{Summary: []ScheduledQuestionTestAccountSummary{{AccountName: plan.Name, Error: "unsafe upstream body"}}}
	notification := buildScheduledQuestionNotification(plan.Name, plan, run, "report", nil, time.Now())
	text := notification.Title + strings.Join(notification.Lines, "") + notification.Rows[0].Account + notification.Rows[0].Note
	for _, secret := range []string{scheduledQuestionTestWebhook, "token-sensitive-1234", "secret-value", "unsafe upstream body"} {
		require.NotContains(t, text, secret)
	}
}

func TestScheduledQuestionAllSkippedNoticeOnlyAppearsBeforeReportStatistics(t *testing.T) {
	const notice = "本轮所有账号额度已用尽，未执行测试"
	for _, tc := range []struct {
		name, kind     string
		total, skipped int
		wantNotice     bool
	}{
		{name: "all skipped report", kind: "report", total: 2, skipped: 2, wantNotice: true},
		{name: "partial skipped report", kind: "report", total: 2, skipped: 1},
		{name: "empty report", kind: "report"},
		{name: "all skipped alert", kind: "alert", total: 2, skipped: 2},
		{name: "all skipped recovered", kind: "recovered", total: 2, skipped: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notification := buildScheduledQuestionNotification("Site", &ScheduledQuestionTestPlan{}, &ScheduledQuestionTestRun{
				TotalAccounts: tc.total, SkippedAccounts: tc.skipped,
			}, tc.kind, nil, time.Now())
			body := strings.Join(notification.Lines, "\n")
			if tc.wantNotice {
				require.Equal(t, 1, strings.Count(body, notice))
				require.Less(t, strings.Index(body, notice), strings.Index(body, "通过 / Passed:"))
			} else {
				require.NotContains(t, body, notice)
			}
		})
	}
}
