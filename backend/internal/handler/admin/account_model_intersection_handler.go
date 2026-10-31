package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// AccountModelIntersectionHandler exposes the shared model picker intersection.
type AccountModelIntersectionHandler struct {
	catalog *service.AccountModelCatalogService
}

func NewAccountModelIntersectionHandler(catalog *service.AccountModelCatalogService) *AccountModelIntersectionHandler {
	return &AccountModelIntersectionHandler{catalog: catalog}
}

type AccountModelIntersectionRequest struct {
	AccountIDs []int64 `json:"account_ids"`
}

func (h *AccountModelIntersectionHandler) Get(c *gin.Context) {
	var req AccountModelIntersectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.catalog.ModelIntersection(c.Request.Context(), req.AccountIDs)
	if c.Request.Context().Err() != nil {
		return
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
