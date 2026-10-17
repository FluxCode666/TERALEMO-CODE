package admin

import "github.com/Wei-Shaw/sub2api/internal/service"

type userEnterpriseSummary struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

func (h *UserHandler) SetEnterpriseRepository(repo service.EnterpriseRepository) {
	h.enterprises = repo
}
