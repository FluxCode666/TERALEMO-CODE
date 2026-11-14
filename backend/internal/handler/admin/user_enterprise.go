package admin

import "github.com/Wei-Shaw/sub2api/internal/service"

type userEnterpriseSummary struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
	// Balance 仅企业负责人（owner）下发，其余角色为 null。
	Balance *float64 `json:"balance"`
}

func newUserEnterpriseSummary(m *service.EnterpriseMemberContext) *userEnterpriseSummary {
	if m == nil {
		return nil
	}
	s := &userEnterpriseSummary{ID: m.EnterpriseID, Name: m.EnterpriseName, Role: m.Role}
	if m.Role == service.EnterpriseRoleOwner {
		b := m.EnterpriseBalance
		s.Balance = &b
	}
	return s
}

func (h *UserHandler) SetEnterpriseRepository(repo service.EnterpriseRepository) {
	h.enterprises = repo
}
