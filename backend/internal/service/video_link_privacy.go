package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const videoPrivacyInternalRead = "video_privacy_internal_read"

var videoUpstreamURLPattern = regexp.MustCompile(`(?i)(?:https?://|gs://)[^\s"'<>\\]+`)

func (a *Account) HideVideoUpstreamURL() bool {
	if a == nil {
		return false
	}
	hide, _ := a.Credentials["hide_video_upstream_url"].(bool)
	return hide
}

// The resource contains only a task/file identity, never an upstream URL or key.
// Downloads require the same API key, user and group that created the task.
func VideoContentProxyURL(taskID string, index int) string {
	return "/v1/video-tasks/" + base64.RawURLEncoding.EncodeToString([]byte(taskID)) + "/content?index=" + strconv.Itoa(index)
}

func ParseVideoContentResource(resource string) (string, error) {
	if len(resource) > 768 {
		return "", fmt.Errorf("invalid video resource")
	}
	data, err := base64.RawURLEncoding.DecodeString(resource)
	if err != nil {
		return "", fmt.Errorf("invalid video resource")
	}
	id := string(data)
	switch {
	case strings.HasPrefix(id, "minimax:"), strings.HasPrefix(id, "minimax-file:"):
		_, value, _ := strings.Cut(id, ":")
		err = ValidateMiniMaxVideoID(value)
	case strings.HasPrefix(id, "gemini:"):
		err = ValidateGeminiVideoOperation(strings.TrimPrefix(id, "gemini:"))
	case strings.HasPrefix(id, "seedance:"):
		value := strings.TrimPrefix(id, "seedance:")
		if value == "" {
			return "", fmt.Errorf("invalid video resource")
		}
		err = validateUpstreamPathSegment("task", value)
	default:
		return "", fmt.Errorf("invalid video resource")
	}
	if err != nil {
		return "", fmt.Errorf("invalid video resource")
	}
	return id, nil
}

func nativeVideoResultURLs(protocol string, body []byte) []string {
	switch protocol {
	case VideoProtocolGemini:
		var links []string
		for _, path := range []string{"response.generateVideoResponse.generatedSamples", "response.generatedVideos"} {
			for _, sample := range gjson.GetBytes(body, path).Array() {
				if link := sample.Get("video.uri").String(); link != "" {
					links = append(links, link)
				}
			}
		}
		return links
	case VideoProtocolMiniMax:
		for _, path := range []string{"task.content.url", "file.download_url"} {
			if link := gjson.GetBytes(body, path).String(); link != "" {
				return []string{link}
			}
		}
	case VideoProtocolSeedance:
		for _, path := range []string{"content.video_url", "video.url"} {
			if link := gjson.GetBytes(body, path).String(); link != "" {
				return []string{link}
			}
		}
	}
	return nil
}

func redactVideoURLs(value any, replacements map[string]string) any {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			v[key] = redactVideoURLs(child, replacements)
		}
	case []any:
		for i, child := range v {
			v[i] = redactVideoURLs(child, replacements)
		}
	case string:
		if replacement, ok := replacements[v]; ok {
			return replacement
		}
		return videoUpstreamURLPattern.ReplaceAllString(v, "[hidden]")
	}
	return value
}

func (s *OpenAIGatewayService) writeNativeVideoResponse(c *gin.Context, account *Account, resp *http.Response, body []byte, taskID string) {
	if !account.HideVideoUpstreamURL() || c.GetBool(videoPrivacyInternalRead) {
		writeGrokMediaResponse(c, resp, body, s.responseHeaderFilter)
		return
	}
	// An empty body (e.g. 204 after a Seedance cancel) carries no URLs to
	// rewrite and is not JSON; keep its status instead of reporting a 502.
	if len(bytes.TrimSpace(body)) == 0 {
		copy := videoPrivateResponseHeaders(resp)
		c.Header("Cache-Control", "no-store")
		writeGrokMediaResponse(c, copy, body, s.responseHeaderFilter)
		return
	}
	// URL-bearing diagnostic/future fields and headers must not bypass the switch.
	replacements := make(map[string]string)
	if taskID != "" {
		for index, link := range nativeVideoResultURLs(account.VideoProtocol(), body) {
			if _, exists := replacements[link]; !exists {
				replacements[link] = VideoContentProxyURL(taskID, index)
			}
		}
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber() // MiniMax file IDs can exceed JavaScript's integer range.
	if err := decoder.Decode(&value); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "Invalid upstream video response"}})
		return
	}
	safeBody, err := json.Marshal(redactVideoURLs(value, replacements))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "Invalid upstream video response"}})
		return
	}
	copy := videoPrivateResponseHeaders(resp)
	copy.Header.Set("Content-Type", "application/json")
	c.Header("Cache-Control", "no-store")
	writeGrokMediaResponse(c, copy, safeBody, s.responseHeaderFilter)
}

// videoPrivateResponseHeaders copies the upstream response without headers
// that expose an upstream URL; the body length is recomputed by the writer.
func videoPrivateResponseHeaders(resp *http.Response) *http.Response {
	copy := *resp
	copy.Header = resp.Header.Clone()
	for key, values := range copy.Header {
		for _, value := range values {
			if videoUpstreamURLPattern.MatchString(value) {
				copy.Header.Del(key)
				break
			}
		}
	}
	copy.Header.Del("Content-Length")
	return &copy
}

func rejectPrivateVideoCallback(c *gin.Context, account *Account, body []byte) error {
	if account.HideVideoUpstreamURL() && strings.TrimSpace(gjson.GetBytes(body, "callback_url").String()) != "" {
		message := "callback_url is unavailable when upstream video URLs are hidden; poll the task endpoint instead"
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": message}})
		return fmt.Errorf("%s", message)
	}
	return nil
}

// Apply the current account setting at every panel read, including old completed
// records. Resolve failures hide links rather than exposing stale upstream URLs.
func (s *OpenAIGatewayService) PublicVideoTask(ctx context.Context, userID int64, task AsyncTask) AsyncTask {
	if task.Kind != "video" || task.Protocol == "grok" {
		return task
	}
	requestID := task.Protocol + ":" + task.TaskID
	id, err := s.ResolveGrokMediaVideoRequestAccount(ctx, task.GroupID, requestID, userID, task.APIKeyID)
	var account *Account
	if err == nil && id > 0 {
		account, err = s.accountRepo.GetByID(ctx, id)
	}
	if err != nil || account == nil || account.HideVideoUpstreamURL() {
		if task.ResultURL != "" {
			task.ResultURL = "/api/v1/user/async-tasks/" + task.ID + "/content"
		}
		task.Error = videoUpstreamURLPattern.ReplaceAllString(task.Error, "[hidden]")
	}
	return task
}
