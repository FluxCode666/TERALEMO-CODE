package service

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type enterpriseBalanceNotifyReader interface {
	GetByID(context.Context, int64) (*Enterprise, error)
}

type enterpriseOwnerNotifyReader interface {
	List(context.Context, int64, pagination.PaginationParams, EnterpriseMemberListFilters) ([]EnterpriseMember, *pagination.PaginationResult, error)
}

type enterpriseBalanceEmailSender interface {
	SendEmail(context.Context, string, string, string) error
}

func (s *BalanceNotifyService) ConfigureEnterpriseBillingNotifications(enterprises EnterpriseRepository, members EnterpriseMemberRepository) {
	s.enterpriseRepo = enterprises
	s.enterpriseMemberRepo = members
	if s.emailService != nil {
		s.enterpriseEmailSender = s.emailService
	}
}

// The committed RETURNING balance serializes crossings between concurrent
// members. Only the deduction moving from > threshold to <= threshold sends.
func (s *BalanceNotifyService) CheckEnterpriseBalanceAfterDeduction(ctx context.Context, enterpriseID int64, newBalance, cost float64) {
	if s == nil || s.enterpriseRepo == nil || s.enterpriseMemberRepo == nil || s.enterpriseEmailSender == nil || cost <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, emailSendTimeout)
	defer cancel()
	enterprise, err := s.enterpriseRepo.GetByID(ctx, enterpriseID)
	if err != nil || enterprise == nil {
		slog.Warn("load enterprise for balance notification failed", "enterprise_id", enterpriseID, "error", err)
		return
	}
	if !enterpriseBalanceCrossedThreshold(newBalance, cost, enterprise.LowBalanceThreshold) {
		return
	}
	// Read all pages before sending, so a failed lookup cannot partially notify
	// the owners and make retries duplicate messages to the first page.
	var owners []EnterpriseMember
	for page := 1; ; page++ {
		members, result, err := s.enterpriseMemberRepo.List(ctx, enterpriseID,
			pagination.PaginationParams{Page: page, PageSize: 1000},
			EnterpriseMemberListFilters{Role: EnterpriseRoleOwner, Status: EnterpriseMemberStatusActive})
		if err != nil {
			slog.Warn("load enterprise owners for balance notification failed", "enterprise_id", enterpriseID, "error", err)
			return
		}
		owners = append(owners, members...)
		if len(members) == 0 || result == nil || page >= result.Pages {
			break
		}
	}
	subject := fmt.Sprintf("企业余额不足提醒 / Enterprise Balance Low: %s", sanitizeEmailHeader(enterprise.Name))
	body := fmt.Sprintf("<p>企业 %s 的余额已降至 $%.8f，提醒阈值为 $%.8f。请联系平台管理员充值。</p>", html.EscapeString(enterprise.Name), newBalance, *enterprise.LowBalanceThreshold)
	for _, to := range enterpriseOwnerEmails(owners) {
		if err := s.enterpriseEmailSender.SendEmail(ctx, to, subject, body); err != nil {
			slog.Warn("enterprise balance notification failed", "enterprise_id", enterpriseID, "error", err)
		}
	}
}

func enterpriseBalanceCrossedThreshold(newBalance, cost float64, threshold *float64) bool {
	return threshold != nil && cost > 0 && newBalance <= *threshold && QuantizeUsageBillingAmount(newBalance+cost) > *threshold
}

func enterpriseOwnerEmails(members []EnterpriseMember) []string {
	var recipients []string
	seen := make(map[string]bool)
	for _, m := range members {
		if m.Role != EnterpriseRoleOwner || m.Status != EnterpriseMemberStatusActive || m.DeletedAt != nil {
			continue
		}
		email := strings.TrimSpace(m.Email)
		key := strings.ToLower(email)
		if email != "" && !seen[key] {
			recipients = append(recipients, email)
			seen[key] = true
		}
	}
	return recipients
}
