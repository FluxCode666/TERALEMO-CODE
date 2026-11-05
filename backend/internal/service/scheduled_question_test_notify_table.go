package service

import (
	"fmt"
	"html"
	"strings"
	"time"
)

type scheduledQuestionNotificationRow struct {
	Account, Result, Ratio, Before, After, Action, Note string
	ResultColor, ActionColor                            string
}

func scheduledQuestionNotificationAccountRow(summary ScheduledQuestionTestAccountSummary, plan *ScheduledQuestionTestPlan) scheduledQuestionNotificationRow {
	row := scheduledQuestionNotificationRow{
		Account: fmt.Sprintf("%s (%d)", scheduledQuestionNotificationText(summary.AccountName, plan), summary.AccountID),
		Ratio:   "—", Before: scheduledQuestionSchedulableText(summary.SchedulableBefore), After: scheduledQuestionSchedulableText(summary.SchedulableAfter),
		Action: "—", ActionColor: "neutral",
	}
	result := scheduledQuestionAccountResult(summary)
	switch result {
	case "passed":
		row.Result, row.ResultColor = "通过", "green"
	case "failed":
		row.Result, row.ResultColor = "未通过", "red"
	case "error":
		row.Result, row.ResultColor = "出错", "red"
	case "incomplete":
		row.Result, row.ResultColor = "未完成", "orange"
	case "skipped":
		row.Result, row.ResultColor = "已跳过", "neutral"
	default:
		row.Result, row.ResultColor = "需人工", "orange"
	}
	if result != "skipped" && summary.Judged > 0 {
		row.Ratio = fmt.Sprintf("%d/%d", summary.Passed, summary.Judged)
		if summary.Ratio != nil {
			row.Ratio += fmt.Sprintf(" (%d%%)", *summary.Ratio)
		}
	}
	switch summary.ScheduleAction {
	case "paused":
		row.Action, row.ActionColor = "已暂停", "red"
	case "resumed":
		row.Action, row.ActionColor = "已恢复", "green"
	case "pause_failed":
		row.Action, row.ActionColor = "暂停失败", "red"
	case "resume_failed":
		row.Action, row.ActionColor = "恢复失败", "red"
	case "resume_blocked":
		row.Action, row.ActionColor, row.Note = "其它计划未通过，未恢复", "orange", "其它计划判定未通过"
	}
	if result == "skipped" {
		switch summary.SkipReason {
		case "quota_exceeded":
			row.Note = "配额已用尽"
		case "window_cost_exceeded":
			row.Note = "5h 窗口费用达到阈值"
		case "rate_limited":
			row.Note = "上游限流中"
		default:
			row.Note = "已跳过"
		}
		if summary.SkipResetAt != nil {
			row.Note += "；预计 " + summary.SkipResetAt.In(time.Local).Format("01-02 15:04") + " 恢复"
		} else if summary.SkipReason == "quota_exceeded" {
			row.Note += "；需管理员调整配额"
		}
	} else if summary.Error != "" || result == "error" {
		if row.Note != "" {
			row.Note += "；"
		}
		row.Note += "出错"
	}
	return row
}

func scheduledQuestionNotificationText(text string, plan *ScheduledQuestionTestPlan) string {
	return sanitizeScheduledQuestionErrorText(text, plan, len(text))
}

func scheduledQuestionSchedulableText(value *bool) string {
	if value == nil {
		return "—"
	}
	if *value {
		return "开"
	}
	return "关"
}

func scheduledQuestionNotificationRank(summary ScheduledQuestionTestAccountSummary) int {
	if summary.ScheduleAction != "" {
		return 0
	}
	switch scheduledQuestionAccountResult(summary) {
	case "failed", "error":
		return 1
	case "skipped":
		return 2
	default:
		return 3
	}
}

func writeScheduledQuestionNotificationTable(body *strings.Builder, rows []scheduledQuestionNotificationRow) {
	if len(rows) == 0 {
		return
	}
	_, _ = body.WriteString(`<table style="border-collapse:collapse;width:100%;font-size:14px"><thead><tr>`)
	for _, title := range []string{"账号", "结果", "通过/判定", "测试前调度", "测试后调度", "本轮动作", "说明"} {
		_, _ = fmt.Fprintf(body, `<th style="border:1px solid #cbd5e1;padding:8px;text-align:left;background-color:#f1f5f9">%s</th>`, html.EscapeString(title))
	}
	_, _ = body.WriteString("</tr></thead><tbody>")
	for _, row := range rows {
		_, _ = body.WriteString("<tr>")
		for index, cell := range []string{row.Account, row.Result, row.Ratio, row.Before, row.After, row.Action, row.Note} {
			style := "border:1px solid #cbd5e1;padding:8px;vertical-align:top"
			if index == 1 || index == 5 {
				color := row.ResultColor
				if index == 5 {
					color = row.ActionColor
				}
				switch color {
				case "green":
					style += ";color:#166534;background-color:#f0fdf4"
				case "red":
					style += ";color:#b91c1c;background-color:#fef2f2"
				case "orange":
					style += ";color:#9a3412;background-color:#fff7ed"
				default:
					style += ";color:#475569;background-color:#f8fafc"
				}
			}
			_, _ = fmt.Fprintf(body, `<td style="%s">%s</td>`, style, html.EscapeString(cell))
		}
		_, _ = body.WriteString("</tr>")
	}
	_, _ = body.WriteString("</tbody></table>")
}
