//go:build unit

package service

import (
	"context"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type enterpriseNotifyReaderStub struct {
	enterprise *Enterprise
	owners     []EnterpriseMember
}

func (s *enterpriseNotifyReaderStub) GetByID(context.Context, int64) (*Enterprise, error) {
	return s.enterprise, nil
}
func (s *enterpriseNotifyReaderStub) List(context.Context, int64, pagination.PaginationParams, EnterpriseMemberListFilters) ([]EnterpriseMember, *pagination.PaginationResult, error) {
	return s.owners, &pagination.PaginationResult{Pages: 1}, nil
}

type enterpriseNotifyEmailStub struct {
	mu         sync.Mutex
	recipients []string
}

func (s *enterpriseNotifyEmailStub) SendEmail(_ context.Context, to, _, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recipients = append(s.recipients, to)
	return nil
}

func TestEnterpriseBalanceNotifyConcurrentCrossing(t *testing.T) {
	threshold := 100.0
	repo := &enterpriseNotifyReaderStub{enterprise: &Enterprise{ID: 1, Name: "<Acme>", LowBalanceThreshold: &threshold}, owners: []EnterpriseMember{
		{Role: EnterpriseRoleOwner, Status: EnterpriseMemberStatusActive, Email: "owner@example.com"},
		{Role: EnterpriseRoleOwner, Status: EnterpriseMemberStatusActive, Email: "OWNER@example.com"},
		{Role: EnterpriseRoleOwner, Status: EnterpriseMemberStatusActive, Email: "second@example.com"},
		{Role: EnterpriseRoleOwner, Status: EnterpriseMemberStatusDisabled, Email: "disabled@example.com"},
		{Role: EnterpriseRoleMember, Status: EnterpriseMemberStatusActive, Email: "member@example.com"},
	}}
	sender := &enterpriseNotifyEmailStub{}
	svc := &BalanceNotifyService{enterpriseRepo: repo, enterpriseMemberRepo: repo, enterpriseEmailSender: sender}
	var wg sync.WaitGroup
	for _, balance := range []float64{101.5, 100.5, 99.5, 98.5} {
		wg.Add(1)
		go func(balance float64) {
			defer wg.Done()
			svc.CheckEnterpriseBalanceAfterDeduction(context.Background(), 1, balance, 1)
		}(balance)
	}
	wg.Wait()
	require.ElementsMatch(t, []string{"owner@example.com", "second@example.com"}, sender.recipients)
}

func TestEnterpriseBalanceThresholdBoundaries(t *testing.T) {
	threshold := 100.0
	for _, tc := range []struct {
		name          string
		balance, cost float64
		threshold     *float64
		want          bool
	}{
		{"disabled", 0, 1, nil, false},
		{"lands on threshold", 100, 1, &threshold, true},
		{"starts on threshold", 99, 1, &threshold, false},
		{"already below", 98, 1, &threshold, false},
		{"no deduction", 100, 0, &threshold, false},
		{"decimal addition stays on threshold", 99.9, 0.1, &threshold, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, enterpriseBalanceCrossedThreshold(tc.balance, tc.cost, tc.threshold))
		})
	}
}
