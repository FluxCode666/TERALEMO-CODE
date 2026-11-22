package service

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

func (s *OpsAlertEvaluatorService) dispatchOpsAlert(runtimeCfg *OpsAlertRuntimeSettings, rule *OpsAlertRule, event *OpsAlertEvent, status string, now time.Time, value float64, platform string, groupID *int64) {
	if s.alertNotifier == nil || (runtimeCfg != nil && isOpsAlertSilenced(now, rule, event, runtimeCfg.Silencing)) {
		return
	}
	state := "告警触发"
	if status == OpsAlertStatusResolved {
		state = "已自动恢复"
	}
	description := strings.TrimSpace(rule.Description)
	if description == "" {
		description = event.Description
	}
	fields := []AlertNotifyField{
		{Label: "规则名", Value: rule.Name},
		{Label: "级别", Value: rule.Severity},
		{Label: "状态", Value: state},
		{Label: "指标", Value: fmt.Sprintf("%s %s %.2f（当前值 %.2f）", rule.MetricType, rule.Operator, rule.Threshold, value)},
		{Label: "触发时间", Value: formatAlertNotifyLocalTime(event.FiredAt)},
		{Label: "描述", Value: description},
	}
	if platform != "" {
		fields = append(fields, AlertNotifyField{Label: "平台", Value: platform})
	}
	if groupID != nil && *groupID > 0 {
		fields = append(fields, AlertNotifyField{Label: "分组", Value: strconv.FormatInt(*groupID, 10)})
	}
	s.alertNotifier.Dispatch(AlertNotification{
		Event: AlertNotifyEventOps, Status: status, Severity: opsEmailSeverityForOps(rule.Severity),
		Title: strings.TrimSpace(rule.Name), Summary: state + "：" + strings.TrimSpace(rule.Name),
		Fields: fields, OccurredAt: now.UTC(), Source: AlertNotifySource{Type: "ops_alert", ID: strconv.FormatInt(event.ID, 10)},
	})
}

func buildQuotaAlertNotification(account *Account, dim quotaDim, siteName string, now time.Time) AlertNotification {
	dimension := map[string]string{quotaDimDaily: "日限额", quotaDimWeekly: "周限额", quotaDimTotal: "总限额"}[dim.name]
	threshold := fmt.Sprintf("$%.2f", dim.threshold)
	if dim.thresholdType == thresholdTypePercentage {
		threshold = fmt.Sprintf("%g%%", dim.threshold)
	}
	accountID := strconv.FormatInt(account.ID, 10)
	return AlertNotification{
		Event: AlertNotifyEventQuota, Status: OpsAlertStatusFiring, Severity: "warning",
		Title: "账号配额告警", Summary: account.Name + "的" + dimension + "已达到告警阈值。",
		Fields: []AlertNotifyField{
			{Label: "账号 ID", Value: accountID}, {Label: "账号名称", Value: account.Name},
			{Label: "平台", Value: account.Platform}, {Label: "维度", Value: dimension},
			{Label: "已用", Value: fmt.Sprintf("$%.2f", dim.currentUsed)},
			{Label: "上限", Value: fmt.Sprintf("$%.2f", dim.limit)},
			{Label: "剩余", Value: fmt.Sprintf("$%.2f", math.Max(0, dim.limit-dim.currentUsed))},
			{Label: "剩余阈值", Value: threshold},
		},
		OccurredAt: now.UTC(), SiteName: siteName, Source: AlertNotifySource{Type: "account_quota", ID: accountID + "-" + dim.name},
	}
}
