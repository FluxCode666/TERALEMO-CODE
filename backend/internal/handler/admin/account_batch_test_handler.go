package admin

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// AccountBatchTestHandler streams results after validating the entire question set.
type AccountBatchTestHandler struct {
	service *service.AccountBatchTestService
}

func NewAccountBatchTestHandler(service *service.AccountBatchTestService) *AccountBatchTestHandler {
	return &AccountBatchTestHandler{service: service}
}

type AccountBatchTestRequest struct {
	AccountIDs  []int64 `json:"account_ids"`
	QuestionIDs []int64 `json:"question_ids"`
	ModelID     string  `json:"model_id"`
	Concurrency *int    `json:"concurrency"`
}

func (h *AccountBatchTestHandler) Test(c *gin.Context) {
	var req AccountBatchTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	ctx := c.Request.Context()
	plan, err := h.service.Prepare(ctx, service.AccountBatchTestInput{
		AccountIDs: req.AccountIDs, QuestionIDs: req.QuestionIDs, ModelID: req.ModelID, Concurrency: req.Concurrency,
	})
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		if errors.Is(err, service.ErrAccountBatchTestMixedPlatforms) {
			response.ErrorFrom(c, err)
			return
		}
		response.BadRequest(c, err.Error())
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	if err := h.service.Run(ctx, plan, func(event service.AccountBatchTestEvent) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		payload, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", payload); err != nil {
			return err
		}
		c.Writer.Flush()
		return nil
	}); err != nil && ctx.Err() == nil {
		_ = c.Error(err)
	}
}
