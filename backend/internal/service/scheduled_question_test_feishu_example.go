package service

import "time"

func scheduledQuestionFeishuTestNotification(siteName string, now time.Time) scheduledQuestionNotification {
	on, off, fullRatio, failedRatio := true, false, 100, 33
	notification := scheduledQuestionNotification{
		Title: "定时测试通知测试 / Test notification", Color: "blue",
		Lines: []string{
			sanitizeScheduledQuestionErrorText(siteName, nil, 120),
			now.In(time.Local).Format("2006-01-02 15:04:05 -07:00"),
			"以下为示例数据，用于验证表格与彩色标签显示；不会执行实际测试或修改账号调度。",
		},
	}
	for _, summary := range []ScheduledQuestionTestAccountSummary{
		{AccountID: 1, AccountName: "示例账号 A", Result: "passed", Passed: 3, Judged: 3, Total: 3, Ratio: &fullRatio, SchedulableBefore: &off, SchedulableAfter: &on, ScheduleAction: "resumed"},
		{AccountID: 2, AccountName: "示例账号 B", Result: "failed", Passed: 1, Judged: 3, Total: 3, Ratio: &failedRatio, SchedulableBefore: &on, SchedulableAfter: &off, ScheduleAction: "paused"},
	} {
		row := scheduledQuestionNotificationAccountRow(summary, nil)
		row.Note = "示例数据"
		notification.Rows = append(notification.Rows, row)
	}
	notification.DetailStart = len(notification.Lines)
	return notification
}
