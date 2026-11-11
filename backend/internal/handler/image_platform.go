package handler

import (
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/pkg/imagecompat"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpenAIGatewayHandler) ImagePlatformImages(c *gin.Context) {
	c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformImage))
	h.Images(c)
}

func writeImageCompatErrorResponse(c *gin.Context, err error) bool {
	var typed *imagecompat.Error
	if !errors.As(err, &typed) || typed == nil {
		return false
	}
	c.Header("Content-Type", "application/json")
	c.Data(typed.HTTPStatus, "application/json", typed.OpenAIErrorBody())
	service.MarkResponseCommitted(c)
	return true
}
