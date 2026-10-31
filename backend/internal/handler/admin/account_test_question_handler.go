package admin

import (
	"errors"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// AccountTestQuestionHandler 处理账号测试题目的管理请求。
type AccountTestQuestionHandler struct {
	service *service.AccountTestQuestionService
}

func NewAccountTestQuestionHandler(service *service.AccountTestQuestionService) *AccountTestQuestionHandler {
	return &AccountTestQuestionHandler{service: service}
}

type CreateAccountTestQuestionRequest struct {
	Name           string   `json:"name" binding:"required"`
	Content        string   `json:"content" binding:"required"`
	AnswerPatterns []string `json:"answer_patterns"`
	Enabled        *bool    `json:"enabled"`
	SortOrder      int      `json:"sort_order"`
}

type UpdateAccountTestQuestionRequest struct {
	Name           *string   `json:"name"`
	Content        *string   `json:"content"`
	AnswerPatterns *[]string `json:"answer_patterns"`
	Enabled        *bool     `json:"enabled"`
	SortOrder      *int      `json:"sort_order"`
}

// List 获取所有题目，可仅筛选已启用的题目。
func (h *AccountTestQuestionHandler) List(c *gin.Context) {
	enabledOnly := false
	if value, exists := c.GetQuery("enabled"); exists {
		var err error
		enabledOnly, err = strconv.ParseBool(value)
		if err != nil {
			response.BadRequest(c, "Invalid enabled filter")
			return
		}
	}
	questions, err := h.service.List(c.Request.Context(), enabledOnly)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, questions)
}

func (h *AccountTestQuestionHandler) GetByID(c *gin.Context) {
	id, ok := parseAccountTestQuestionID(c)
	if !ok {
		return
	}
	question, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if question == nil {
		response.NotFound(c, "Test question not found")
		return
	}
	response.Success(c, question)
}

func (h *AccountTestQuestionHandler) Create(c *gin.Context) {
	var req CreateAccountTestQuestionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	question, err := h.service.Create(c.Request.Context(), service.CreateAccountTestQuestionInput{
		Name: req.Name, Content: req.Content, AnswerPatterns: req.AnswerPatterns,
		Enabled: req.Enabled, SortOrder: req.SortOrder,
	})
	if err != nil {
		writeAccountTestQuestionError(c, err)
		return
	}
	response.Success(c, question)
}

// Update 合并显式传入的字段，保留未传字段。
func (h *AccountTestQuestionHandler) Update(c *gin.Context) {
	id, ok := parseAccountTestQuestionID(c)
	if !ok {
		return
	}
	var req UpdateAccountTestQuestionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	question, err := h.service.Update(c.Request.Context(), id, service.UpdateAccountTestQuestionInput{
		Name: req.Name, Content: req.Content, AnswerPatterns: req.AnswerPatterns,
		Enabled: req.Enabled, SortOrder: req.SortOrder,
	})
	if err != nil {
		writeAccountTestQuestionError(c, err)
		return
	}
	response.Success(c, question)
}

func (h *AccountTestQuestionHandler) Delete(c *gin.Context) {
	id, ok := parseAccountTestQuestionID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Test question deleted successfully"})
}

func parseAccountTestQuestionID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid test question ID")
		return 0, false
	}
	return id, true
}

func writeAccountTestQuestionError(c *gin.Context, err error) {
	var validationError *model.ValidationError
	if errors.As(err, &validationError) {
		response.BadRequest(c, err.Error())
		return
	}
	response.ErrorFrom(c, err)
}
