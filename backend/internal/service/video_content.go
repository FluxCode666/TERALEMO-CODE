package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// ForwardNativeVideoContent resolves a server-owned task, refreshes its output
// URL, and streams bytes. The caller never supplies a URL, host or upstream key.
func (s *OpenAIGatewayService) ForwardNativeVideoContent(ctx context.Context, c *gin.Context, task *AsyncTaskRecord, index int) error {
	if index < 0 || index > 7 {
		return fmt.Errorf("invalid video output index")
	}
	account, err := s.resolveVideoContentAccount(ctx, task.Protocol, task)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if strings.HasPrefix(task.RequestID, "minimax-file:") {
		if index != 0 || !account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityMiniMaxVideo) {
			return fmt.Errorf("video file is unavailable")
		}
		if err := s.observeReservedVideoDownload(ctx, VideoProtocolMiniMax, task.RequestID); err != nil {
			return err
		}
		link, err := s.miniMaxVideoFile(ctx, account, strings.TrimPrefix(task.RequestID, "minimax-file:"))
		if err != nil {
			return err
		}
		return s.forwardPublicVideoURL(ctx, c, account, link)
	}
	recorder := httptest.NewRecorder()
	poll, _ := gin.CreateTestContext(recorder)
	poll.Request = c.Request.WithContext(ctx)
	poll.Set(videoPrivacyInternalRead, true)
	var result *OpenAIForwardResult
	switch task.Protocol {
	case VideoProtocolMiniMax:
		result, err = s.ForwardMiniMaxVideo(ctx, poll, account, MiniMaxVideoEndpointStatus, task.RequestID, nil)
	case VideoProtocolGemini:
		result, err = s.ForwardGeminiVideo(ctx, poll, account, GeminiVideoEndpointStatus, task.RequestID, nil)
	case VideoProtocolSeedance:
		result, err = s.ForwardSeedance(ctx, poll, account, SeedanceEndpointStatus, task.RequestID, nil)
	default:
		return fmt.Errorf("unsupported video protocol")
	}
	if err != nil {
		return err
	}
	if _, err := s.ObserveVideoTaskBilling(ctx, task.Protocol, task.RequestID, result, false); err != nil {
		return err
	}
	if result == nil {
		return fmt.Errorf("video task returned no observation")
	}
	if result.AsyncTaskStatus != "completed" {
		return fmt.Errorf("video is not available")
	}
	link := result.AsyncTaskURL
	if links := nativeVideoResultURLs(task.Protocol, recorder.Body.Bytes()); len(links) > 0 {
		if index >= len(links) {
			return fmt.Errorf("video output is not available")
		}
		link = links[index]
	} else if index != 0 {
		return fmt.Errorf("video output is not available")
	}
	if task.Protocol == VideoProtocolGemini {
		copy := *task
		copy.ResultURL = link
		return s.ForwardGeminiVideoContent(ctx, c, &copy)
	}
	return s.forwardPublicVideoURL(ctx, c, account, link)
}

// MiniMax download URLs expire after one hour. Retrieve a fresh signed URL using
// the owned task's account, then download without forwarding any credentials.
func (s *OpenAIGatewayService) ForwardMiniMaxVideoContent(ctx context.Context, c *gin.Context, task *AsyncTaskRecord) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	account, err := s.resolveVideoContentAccount(ctx, VideoProtocolMiniMax, task)
	if err != nil {
		return err
	}
	if !account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityMiniMaxVideo) {
		return fmt.Errorf("task account no longer supports MiniMax video")
	}
	id := strings.TrimPrefix(task.RequestID, "minimax:")
	_, status, err := s.miniMaxVideoRequest(ctx, account, MiniMaxVideoEndpointStatus, id, nil)
	if err != nil {
		return err
	}
	var link string
	if miniMaxVideoV2(account.VideoBaseURL()) {
		observed, err := observeMiniMaxVideoV2(status, id)
		if err != nil {
			return fmt.Errorf("MiniMax video is not available")
		}
		if _, err := s.ObserveVideoTaskBilling(ctx, VideoProtocolMiniMax, task.RequestID, observed, false); err != nil {
			return err
		}
		if observed.AsyncTaskStatus != "completed" {
			return fmt.Errorf("MiniMax video is not available")
		}
		link = observed.AsyncTaskURL
	} else {
		if gjson.GetBytes(status, "task_id").String() != id {
			return fmt.Errorf("MiniMax video is not available")
		}
		observation := &OpenAIForwardResult{AsyncTaskStatus: "processing", VideoFileID: gjson.GetBytes(status, "file_id").String()}
		switch strings.ToLower(gjson.GetBytes(status, "status").String()) {
		case "success":
			observation.AsyncTaskStatus, observation.VideoCount = "completed", 1
		case "fail", "failed":
			observation.AsyncTaskStatus = "failed"
		}
		if _, err := s.ObserveVideoTaskBilling(ctx, VideoProtocolMiniMax, task.RequestID, observation, false); err != nil {
			return err
		}
		if observation.AsyncTaskStatus != "completed" {
			return fmt.Errorf("MiniMax video is not available")
		}
		link, err = s.miniMaxVideoFile(ctx, account, gjson.GetBytes(status, "file_id").String())
		if err != nil {
			return err
		}
	}
	return s.forwardPublicVideoURL(ctx, c, account, link)
}

func (s *OpenAIGatewayService) forwardPublicVideoURL(ctx context.Context, c *gin.Context, account *Account, link string) error {
	target, err := url.Parse(link)
	if err != nil || target.User != nil || target.Fragment != "" {
		return fmt.Errorf("invalid MiniMax download URL")
	}
	if _, err := urlvalidator.ValidateHTTPSURL(link, urlvalidator.ValidationOptions{}); err != nil {
		return fmt.Errorf("invalid MiniMax download URL")
	}
	if _, err := s.validateOutboundURL(link); err != nil {
		return fmt.Errorf("MiniMax download URL is not allowed")
	}
	downloadCtx := WithHTTPUpstreamPublicHostsOnly(WithHTTPUpstreamRedirectsDisabled(ctx))
	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, link, nil)
	if err != nil {
		return err
	}
	proxy := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(req, proxy, account.ID, account.Concurrency)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return streamVideoContent(c, resp)
}

// ForwardGeminiVideoContent authenticates file downloads on the same upstream
// origin. Never send upstream credentials to an arbitrary URL or a redirect.
func (s *OpenAIGatewayService) ForwardGeminiVideoContent(ctx context.Context, c *gin.Context, task *AsyncTaskRecord) error {
	account, err := s.resolveVideoContentAccount(ctx, VideoProtocolGemini, task)
	if err != nil {
		return err
	}
	if !account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityGeminiVideo) {
		return fmt.Errorf("task account no longer supports Gemini video")
	}
	if err := s.observeReservedVideoDownload(ctx, VideoProtocolGemini, task.RequestID); err != nil {
		return err
	}
	base, err := s.validateUpstreamBaseURL(account.VideoBaseURL())
	if err != nil {
		return err
	}
	origin, err := url.Parse(base)
	if err != nil {
		return err
	}
	target, err := url.Parse(task.ResultURL)
	if err != nil || target.User != nil || target.Fragment != "" || target.Scheme != origin.Scheme || !strings.EqualFold(target.Host, origin.Host) {
		return fmt.Errorf("video result URL must use the configured upstream origin")
	}
	query := target.Query()
	query.Del("key")
	query.Del("api_key")
	target.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), http.MethodGet, target.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-goog-api-key", account.GetCredential("api_key"))
	proxy := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(req, proxy, account.ID, account.Concurrency)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return streamVideoContent(c, resp)
}

func streamVideoContent(c *gin.Context, resp *http.Response) error {
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("video download upstream status %d", resp.StatusCode)
	}
	const maxSize = 1 << 30
	if resp.ContentLength > maxSize {
		return fmt.Errorf("video exceeds download size limit")
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "video/") && !strings.HasPrefix(contentType, "application/octet-stream") {
		return fmt.Errorf("upstream returned a non-video response")
	}
	c.Header("Cache-Control", "no-store")
	c.DataFromReader(http.StatusOK, resp.ContentLength, contentType, io.LimitReader(resp.Body, maxSize), map[string]string{"Content-Disposition": `attachment; filename="generated-video.mp4"`})
	return nil
}
