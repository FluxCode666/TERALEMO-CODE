package admin

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const alertNotifyMaxRequestBytes = 512 << 10

type AlertNotifyHandler struct {
	service *service.AlertNotifyService
}

func NewAlertNotifyHandler(notifier *service.AlertNotifyService) *AlertNotifyHandler {
	return &AlertNotifyHandler{service: notifier}
}

func (h *AlertNotifyHandler) GetChannels(c *gin.Context) {
	view, err := h.service.GetChannels(c.Request.Context())
	if !response.ErrorFrom(c, err) {
		response.Success(c, view)
	}
}

func (h *AlertNotifyHandler) PutChannels(c *gin.Context) {
	var input struct {
		Channels []service.AlertNotifyChannelInput `json:"channels"`
	}
	if !decodeAlertNotifyRequest(c, &input) {
		return
	}
	if input.Channels == nil {
		response.BadRequest(c, "channels must be an array")
		return
	}
	view, err := h.service.ReplaceChannels(c.Request.Context(), input.Channels)
	if !response.ErrorFrom(c, err) {
		response.Success(c, view)
	}
}

func (h *AlertNotifyHandler) TestChannel(c *gin.Context) {
	var input service.AlertNotifyChannelInput
	if !decodeAlertNotifyRequest(c, &input) {
		return
	}
	if !response.ErrorFrom(c, h.service.TestChannel(c.Request.Context(), input)) {
		response.Success(c, struct {
			Success bool `json:"success"`
		}{Success: true})
	}
}

func decodeAlertNotifyRequest(c *gin.Context, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, alertNotifyMaxRequestBytes))
	if err := decoder.Decode(target); err != nil || decoder.Decode(new(any)) != io.EOF {
		response.BadRequest(c, "invalid alert notification request body")
		return false
	}
	return true
}
