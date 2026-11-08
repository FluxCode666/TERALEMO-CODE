package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const defaultOpenAIImagesAsyncPollTimeout = 1800 * time.Second
const openAIImagesAsyncUpstreamErrorKey = "openai_images_async_upstream_error"

var openAIImagesAsyncTaskIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

type openAIImagesAsyncUpstreamTask struct {
	id         string
	status     string
	httpStatus int
	result     json.RawMessage
	errorObj   json.RawMessage
}

func OpenAIImagesAsyncUpstreamErrorFromContext(c *gin.Context) *OpenAIImagesUpstreamError {
	if c == nil {
		return nil
	}
	value, exists := c.Get(openAIImagesAsyncUpstreamErrorKey)
	if !exists {
		return nil
	}
	upstreamErr, _ := value.(*OpenAIImagesUpstreamError)
	return upstreamErr
}

func parseOpenAIImagesAsyncUpstreamTask(body []byte) *openAIImagesAsyncUpstreamTask {
	if !gjson.ValidBytes(body) {
		return nil
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return nil
	}
	taskID := root.Get("task_id").Str
	if taskID == "" {
		taskID = root.Get("id").Str
	}
	return &openAIImagesAsyncUpstreamTask{
		id: taskID, status: root.Get("status").Str,
		httpStatus: int(root.Get("http_status").Int()),
		result:     json.RawMessage(root.Get("result").Raw),
		errorObj:   json.RawMessage(root.Get("error").Raw),
	}
}

func detectOpenAIImagesAsyncUpstreamTask(status int, body []byte) *openAIImagesAsyncUpstreamTask {
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil
	}
	task := parseOpenAIImagesAsyncUpstreamTask(body)
	if task == nil || gjson.GetBytes(body, "data").Exists() {
		return nil
	}
	if gjson.GetBytes(body, "object").Str == "image.generation.task" ||
		(status == http.StatusAccepted && task.id != "" && openAIImagesAsyncTaskProcessing(task.status)) {
		return task
	}
	return nil
}

func openAIImagesAsyncTaskProcessing(status string) bool {
	switch status {
	case "processing", "queued", "pending", "in_progress":
		return true
	default:
		return false
	}
}

func openAIImagesAsyncPollInterval(header http.Header) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(header.Get("Retry-After")))
	if err != nil {
		return 3 * time.Second
	}
	if seconds < 1 {
		seconds = 1
	}
	if seconds > 10 {
		seconds = 10
	}
	return time.Duration(seconds) * time.Second
}

func (s *OpenAIGatewayService) openAIImagesAsyncPollTimeout() time.Duration {
	if s.cfg != nil && s.cfg.Gateway.ImageAsyncUpstreamPollTimeout > 0 {
		return time.Duration(s.cfg.Gateway.ImageAsyncUpstreamPollTimeout) * time.Second
	}
	return defaultOpenAIImagesAsyncPollTimeout
}

func (s *OpenAIGatewayService) buildOpenAIImagesAsyncPollRequest(ctx context.Context, account *Account, token, taskID string) (*http.Request, error) {
	baseURL, err := s.validateUpstreamBaseURL(account.GetOpenAIBaseURL())
	if err != nil {
		return nil, err
	}
	target := buildOpenAIEndpointURL(baseURL, "/v1/images/tasks/"+url.PathEscape(taskID))
	ctx = WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header, err = s.buildOpenAIAuthenticationHeaders(ctx, account, token)
	if err != nil {
		return nil, err
	}
	if userAgent := account.GetOpenAIUserAgent(); userAgent != "" {
		request.Header.Set("User-Agent", userAgent)
	}
	account.ApplyHeaderOverrides(request.Header)
	return request, nil
}

func (s *OpenAIGatewayService) resolveOpenAIImagesAsyncUpstream(
	ctx context.Context, c *gin.Context, account *Account, token, proxyURL string,
	response *http.Response, body []byte, upstreamStart time.Time,
) ([]byte, error) {
	task := detectOpenAIImagesAsyncUpstreamTask(response.StatusCode, body)
	if task == nil {
		return body, nil
	}
	defer func() { SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds()) }()
	polls := 0
	taskID := task.id
	logEvent := func(event string, status int) {
		fields := []zap.Field{
			zap.Int64("account_id", account.ID), zap.String("upstream_task_id", taskID),
			zap.Int("poll_count", polls), zap.Int64("elapsed_ms", time.Since(upstreamStart).Milliseconds()),
			zap.Int("upstream_status", status),
		}
		if event == "timeout" {
			logger.L().Warn("openai.images.async_upstream."+event, fields...)
		} else {
			logger.L().Info("openai.images.async_upstream."+event, fields...)
		}
	}
	fail := func(status int, errorType, message, kind string, upstreamStatus int, errorObject json.RawMessage) ([]byte, error) {
		upstreamErr := &OpenAIImagesUpstreamError{
			StatusCode: status, ErrorType: errorType, Message: message,
			AsyncTaskAccepted: true,
		}
		if task.status == ImageTaskStatusFailed && kind == "async_task_failed" {
			upstreamErr = s.openAIImagesAsyncTaskError(ctx, c, account, status, response.Header, errorObject)
		}
		c.Set(openAIImagesAsyncUpstreamErrorKey, upstreamErr)
		setOpsUpstreamError(c, upstreamStatus, message, "")
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			UpstreamStatusCode: upstreamStatus, Kind: kind, Message: message,
		})
		logEvent("failed", upstreamStatus)
		writeOpenAIImagesUpstreamErrorResponse(c, upstreamErr)
		return nil, upstreamErr
	}
	if !openAIImagesAsyncTaskIDPattern.MatchString(taskID) || taskID == "." || taskID == ".." {
		taskID = ""
		return fail(http.StatusBadGateway, "upstream_error", "Upstream async image task returned an invalid task id", "async_poll_error", response.StatusCode, nil)
	}
	logEvent("detected", response.StatusCode)
	interval := openAIImagesAsyncPollInterval(response.Header)
	requestID := response.Header.Get("x-request-id")
	response.Header = http.Header{"Content-Type": {"application/json"}}
	if requestID != "" {
		response.Header.Set("x-request-id", requestID)
	}
	pollCtx, cancel := context.WithTimeout(ctx, s.openAIImagesAsyncPollTimeout())
	defer cancel()
	notFoundCount := 0
	for {
		switch task.status {
		case ImageTaskStatusCompleted:
			if !openAIImagesAsyncResultHasValidImages(task.result) {
				return fail(http.StatusBadGateway, "upstream_error", "Upstream async image task completed without images", "async_task_failed", http.StatusOK, nil)
			}
			response.StatusCode = http.StatusOK
			logEvent("completed", http.StatusOK)
			return task.result, nil
		case ImageTaskStatusFailed:
			status := task.httpStatus
			if status < http.StatusBadRequest || status > 599 {
				status = http.StatusBadGateway
			}
			errorObject := task.errorObj
			if !gjson.ParseBytes(errorObject).IsObject() {
				errorObject = nil
			}
			return fail(status, "upstream_error", "Upstream async image task failed", "async_task_failed", task.httpStatus, errorObject)
		default:
			if !openAIImagesAsyncTaskProcessing(task.status) {
				return fail(http.StatusBadGateway, "upstream_error", "Upstream async image task returned an invalid response", "async_poll_error", http.StatusOK, nil)
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-pollCtx.Done():
			timer.Stop()
			logEvent("timeout", 0)
			return fail(http.StatusGatewayTimeout, "timeout_error", "Upstream async image task timed out", "async_poll_error", 0, nil)
		case <-timer.C:
		}
		request, err := s.buildOpenAIImagesAsyncPollRequest(pollCtx, account, token, taskID)
		if err != nil {
			return fail(http.StatusBadGateway, "upstream_error", "Failed to build upstream async image task poll request", "async_poll_error", 0, nil)
		}
		polls++
		pollResponse, err := s.doOpenAIUpstream(request, proxyURL, account)
		if err != nil {
			notFoundCount = 0
			logEvent("poll_retry", 0)
			continue
		}
		interval = openAIImagesAsyncPollInterval(pollResponse.Header)
		pollBody, readErr := ReadUpstreamResponseBody(pollResponse.Body, s.cfg, c, nil)
		_ = pollResponse.Body.Close()
		status := pollResponse.StatusCode
		if status == http.StatusNotFound {
			notFoundCount++
			if notFoundCount >= 3 {
				return fail(http.StatusBadGateway, "upstream_error", "Upstream async image task not found or expired", "async_poll_error", status, nil)
			}
			logEvent("poll_retry", status)
			continue
		}
		notFoundCount = 0
		if status == http.StatusTooManyRequests || status >= http.StatusInternalServerError {
			logEvent("poll_retry", status)
			continue
		}
		if status < http.StatusOK || status >= http.StatusMultipleChoices {
			return fail(http.StatusBadGateway, "upstream_error", "Upstream async image task polling failed", "async_poll_error", status, nil)
		}
		if readErr != nil {
			if errors.Is(readErr, ErrUpstreamResponseBodyTooLarge) {
				return fail(http.StatusBadGateway, "upstream_error", "Upstream async image task response too large", "async_poll_error", status, nil)
			}
			logEvent("poll_retry", status)
			continue
		}
		nextTask := parseOpenAIImagesAsyncUpstreamTask(pollBody)
		if nextTask == nil {
			return fail(http.StatusBadGateway, "upstream_error", "Upstream async image task returned an invalid response", "async_poll_error", status, nil)
		}
		task = nextTask
	}
}

func (s *OpenAIGatewayService) openAIImagesAsyncTaskError(ctx context.Context, c *gin.Context, account *Account, status int, header http.Header, errorObject json.RawMessage) *OpenAIImagesUpstreamError {
	body := []byte(`{"error":{"type":"upstream_error","message":"Upstream async image task failed"}}`)
	if len(errorObject) > 0 {
		body = append(append([]byte(`{"error":`), errorObject...), '}')
	}
	body = s.redactAgentIdentitySensitiveBody(ctx, account, body)
	var upstreamErr *OpenAIImagesUpstreamError
	if responseStatus, errorType, message, matched := applyErrorPassthroughRule(c, account.Platform, status, body, http.StatusBadGateway, "upstream_error", "Upstream request failed"); matched {
		upstreamErr = &OpenAIImagesUpstreamError{
			StatusCode: responseStatus, ErrorType: errorType,
			Message: sanitizeUpstreamErrorMessage(message), UpstreamRequestID: header.Get("x-request-id"),
		}
	} else {
		upstreamErr = openAIImagesUpstreamErrorFromHTTP(status, header, body)
	}
	upstreamErr.AsyncTaskAccepted = true
	return upstreamErr
}

func openAIImagesAsyncResultHasValidImages(body []byte) bool {
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return false
	}
	for _, path := range []string{"data", "output", "response.output"} {
		for _, item := range gjson.GetBytes(body, path).Array() {
			fields := []string{"url", "b64_json"}
			if path != "data" {
				fields = append(fields, "result")
			}
			for _, name := range fields {
				field := item.Get(name)
				if field.Type != gjson.Null && field.Type != gjson.String {
					return false
				}
			}
		}
	}
	return countOpenAIResponseImageOutputsFromJSONBytes(body) > 0
}
