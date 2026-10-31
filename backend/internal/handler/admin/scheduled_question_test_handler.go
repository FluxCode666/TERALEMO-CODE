package admin

import (
	"context"
	"errors"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type scheduledQuestionTestHandlerService interface {
	List(context.Context) ([]*service.ScheduledQuestionTestPlan, error)
	GetByID(context.Context, int64) (*service.ScheduledQuestionTestPlan, error)
	Create(context.Context, service.ScheduledQuestionTestPlanInput) (*service.ScheduledQuestionTestPlan, error)
	Update(context.Context, int64, service.ScheduledQuestionTestPlanInput) (*service.ScheduledQuestionTestPlan, error)
	Delete(context.Context, int64) error
	ListRuns(context.Context, int64, int) ([]*service.ScheduledQuestionTestRun, error)
	RunNow(context.Context, int64) (*service.ScheduledQuestionTestAccepted, error)
}

// ScheduledQuestionTestHandler handles scheduled question plans and run history.
type ScheduledQuestionTestHandler struct {
	service scheduledQuestionTestHandlerService
}

func NewScheduledQuestionTestHandler(svc *service.ScheduledQuestionTestService) *ScheduledQuestionTestHandler {
	return &ScheduledQuestionTestHandler{service: svc}
}

type scheduledQuestionTestPlanRequest struct {
	Name            *string  `json:"name"`
	AccountIDs      *[]int64 `json:"account_ids"`
	QuestionIDs     *[]int64 `json:"question_ids"`
	ModelID         *string  `json:"model_id"`
	CronExpression  *string  `json:"cron_expression"`
	Enabled         *bool    `json:"enabled"`
	MinPassRatio    *int     `json:"min_pass_ratio"`
	CooldownMinutes *int     `json:"cooldown_minutes"`
	NotifyEnabled   *bool    `json:"notify_enabled"`
}

func (r scheduledQuestionTestPlanRequest) input() service.ScheduledQuestionTestPlanInput {
	return service.ScheduledQuestionTestPlanInput{
		Name: r.Name, AccountIDs: r.AccountIDs, QuestionIDs: r.QuestionIDs,
		ModelID: r.ModelID, CronExpression: r.CronExpression, Enabled: r.Enabled,
		MinPassRatio: r.MinPassRatio, CooldownMinutes: r.CooldownMinutes,
		NotifyEnabled: r.NotifyEnabled,
	}
}

func (h *ScheduledQuestionTestHandler) List(c *gin.Context) {
	plans, err := h.service.List(c.Request.Context())
	if err != nil {
		writeScheduledQuestionTestError(c, err)
		return
	}
	if plans == nil {
		plans = []*service.ScheduledQuestionTestPlan{}
	}
	response.Success(c, plans)
}

func (h *ScheduledQuestionTestHandler) GetByID(c *gin.Context) {
	id, ok := parseScheduledQuestionTestID(c)
	if !ok {
		return
	}
	plan, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		writeScheduledQuestionTestError(c, err)
		return
	}
	if plan == nil {
		response.NotFound(c, "Scheduled question test plan not found")
		return
	}
	response.Success(c, plan)
}

func (h *ScheduledQuestionTestHandler) Create(c *gin.Context) {
	var req scheduledQuestionTestPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	plan, err := h.service.Create(c.Request.Context(), req.input())
	if err != nil {
		writeScheduledQuestionTestError(c, err)
		return
	}
	response.Success(c, plan)
}

func (h *ScheduledQuestionTestHandler) Update(c *gin.Context) {
	id, ok := parseScheduledQuestionTestID(c)
	if !ok {
		return
	}
	var req scheduledQuestionTestPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	plan, err := h.service.Update(c.Request.Context(), id, req.input())
	if err != nil {
		writeScheduledQuestionTestError(c, err)
		return
	}
	response.Success(c, plan)
}

func (h *ScheduledQuestionTestHandler) Delete(c *gin.Context) {
	id, ok := parseScheduledQuestionTestID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		writeScheduledQuestionTestError(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Scheduled question test plan deleted successfully"})
}

func (h *ScheduledQuestionTestHandler) ListRuns(c *gin.Context) {
	id, ok := parseScheduledQuestionTestID(c)
	if !ok {
		return
	}
	limit := 50
	query := c.Request.URL.Query()
	if query.Has("limit") {
		var err error
		limit, err = strconv.Atoi(query.Get("limit"))
		if err != nil || limit < 1 || limit > 200 {
			response.BadRequest(c, "Limit must be an integer between 1 and 200")
			return
		}
	}
	runs, err := h.service.ListRuns(c.Request.Context(), id, limit)
	if err != nil {
		writeScheduledQuestionTestError(c, err)
		return
	}
	if runs == nil {
		runs = []*service.ScheduledQuestionTestRun{}
	}
	response.Success(c, runs)
}

func (h *ScheduledQuestionTestHandler) RunNow(c *gin.Context) {
	id, ok := parseScheduledQuestionTestID(c)
	if !ok {
		return
	}
	accepted, err := h.service.RunNow(c.Request.Context(), id)
	if err != nil {
		writeScheduledQuestionTestError(c, err)
		return
	}
	response.Accepted(c, accepted)
}

func parseScheduledQuestionTestID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid scheduled question test plan ID")
		return 0, false
	}
	return id, true
}

func writeScheduledQuestionTestError(c *gin.Context, err error) {
	var validationError *model.ValidationError
	if errors.As(err, &validationError) {
		response.BadRequest(c, err.Error())
		return
	}
	response.ErrorFrom(c, err)
}
