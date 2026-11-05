package service

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// scheduledQuestionSkipReason is only used by scheduled rounds, never manual tests.
func (s *ScheduledQuestionTestService) scheduledQuestionSkipReason(ctx context.Context, account *Account, modelID string) (string, *time.Time) {
	if account == nil {
		return "", nil
	}
	if account.IsAPIKeyOrBedrock() && account.IsQuotaExceeded() {
		return "quota_exceeded", scheduledQuestionQuotaResetAt(account)
	}
	if account.IsAnthropicOAuthOrSetupToken() && account.GetWindowCostLimit() > 0 {
		start := account.GetCurrentWindowStartTime()
		if cost, ok := s.scheduledQuestionWindowCost(ctx, account, start); ok && account.CheckWindowCostSchedulability(cost) != WindowCostSchedulable {
			resetAt := start.Add(5 * time.Hour)
			return "window_cost_exceeded", &resetAt
		}
	}
	var resetAt *time.Time
	if account.IsRateLimited() {
		value := *account.RateLimitResetAt
		resetAt = &value
	}
	// No default-model inference: only an explicit plan model selects model limits.
	if modelID = strings.TrimSpace(modelID); modelID != "" {
		now := time.Now()
		for _, key := range account.modelRateLimitKeysForRequest(ctx, modelID) {
			if modelReset := account.modelRateLimitResetAt(key); modelReset != nil && now.Before(*modelReset) && (resetAt == nil || modelReset.After(*resetAt)) {
				resetAt = modelReset
			}
		}
	}
	if resetAt != nil {
		return "rate_limited", resetAt
	}
	return "", nil
}

func scheduledQuestionQuotaResetAt(account *Account) *time.Time {
	if limit := account.GetQuotaLimit(); limit > 0 && account.GetQuotaUsed() >= limit {
		return nil
	}
	now := time.Now()
	location, err := time.LoadLocation(account.GetQuotaResetTimezone())
	if err != nil {
		location = time.UTC
	}
	var resetAt *time.Time
	if limit := account.GetQuotaDailyLimit(); limit > 0 && account.GetQuotaDailyUsed() >= limit && !account.IsDailyQuotaPeriodExpired() {
		value := account.getExtraTime("quota_daily_start").Add(24 * time.Hour)
		if account.GetQuotaDailyResetMode() == "fixed" {
			value = nextFixedDailyReset(account.GetQuotaDailyResetHour(), location, now)
		}
		resetAt = &value
	}
	if limit := account.GetQuotaWeeklyLimit(); limit > 0 && account.GetQuotaWeeklyUsed() >= limit && !account.IsWeeklyQuotaPeriodExpired() {
		value := account.getExtraTime("quota_weekly_start").Add(7 * 24 * time.Hour)
		if account.GetQuotaWeeklyResetMode() == "fixed" {
			value = nextFixedWeeklyReset(account.GetQuotaWeeklyResetDay(), account.GetQuotaWeeklyResetHour(), location, now)
		}
		if resetAt == nil || value.After(*resetAt) {
			resetAt = &value
		}
	}
	return resetAt
}

func (s *ScheduledQuestionTestService) scheduledQuestionWindowCost(ctx context.Context, account *Account, start time.Time) (float64, bool) {
	if s.sessionLimitCache != nil {
		cost, hit, err := s.sessionLimitCache.GetWindowCost(ctx, account.ID)
		if err == nil && hit {
			return cost, true
		}
		if err != nil {
			slog.Warn("scheduled_question_window_cost_unavailable", "account_id", account.ID, "source", "cache")
		}
	}
	if s.usageLogRepo == nil {
		slog.Warn("scheduled_question_window_cost_unavailable", "account_id", account.ID, "source", "repository_unavailable")
		return 0, false
	}
	stats, err := s.usageLogRepo.GetAccountWindowStats(ctx, account.ID, start)
	if err != nil || stats == nil {
		// Upstream/storage errors may contain credentials; log only a safe category.
		slog.Warn("scheduled_question_window_cost_unavailable", "account_id", account.ID, "source", "repository")
		return 0, false
	}
	cost := stats.StandardCost
	if s.sessionLimitCache != nil {
		_ = s.sessionLimitCache.SetWindowCost(ctx, account.ID, cost)
	}
	return cost, true
}

func scheduledQuestionAllErrorsRateLimited(accountID int64, questionCount int, items []AccountBatchTestItemResult) bool {
	if questionCount <= 0 {
		return false
	}
	count := 0
	for _, item := range items {
		if item.AccountID != accountID {
			continue
		}
		count++
		if item.Status != "error" || item.Judged {
			return false
		}
		if item.StatusCode != 0 {
			if item.StatusCode != http.StatusTooManyRequests {
				return false
			}
			continue
		}
		message := strings.TrimSpace(item.Error)
		if message != "API returned 429" && !strings.HasPrefix(message, "API returned 429:") {
			return false
		}
	}
	return count == questionCount
}
