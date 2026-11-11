package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/imagecompat"
	"github.com/gin-gonic/gin"
)

type imageAccountTestUpstream struct {
	HTTPUpstream
	ctx context.Context
}

type imageAccountTestBody struct {
	io.ReadCloser
	release func()
}

func (b *imageAccountTestBody) Close() error {
	defer b.release()
	return b.ReadCloser.Close()
}

func (u imageAccountTestUpstream) Do(req *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	// Normal image requests detach after submission. Tests retain cancellation
	// and their deadline while preserving the gateway's transport context flags.
	ctx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(u.ctx, cancel)
	release := func() { stop(); cancel() }
	if err := u.ctx.Err(); err != nil {
		release()
		return nil, err
	}
	resp, err := u.HTTPUpstream.Do(req.WithContext(ctx), proxy, accountID, concurrency)
	if resp != nil {
		recordAccountTestUpstreamStatus(u.ctx, resp.StatusCode)
	}
	if err != nil || resp == nil || resp.Body == nil {
		release()
	} else {
		resp.Body = &imageAccountTestBody{ReadCloser: resp.Body, release: release}
	}
	return resp, err
}

// Capture the normal Images response separately so the test endpoint keeps its
// SSE contract. A forwarding-only gateway has no user billing or state writers.
func (s *AccountTestService) testImageAccountConnection(c *gin.Context, account *Account, modelID, prompt string, opts AccountTestOptions) error {
	s.prepareGrokTestSSE(c)
	fail := func(message string) error {
		typed := sanitizeImageAccountError(account, imageRequestError("", message))
		return s.sendErrorAndEnd(c, typed.Message)
	}
	if err := ValidateImageAccount(account); err != nil {
		return fail(err.Error())
	}
	if s.httpUpstream == nil {
		return fail("Image test HTTP client is not configured")
	}
	if c.Request.Context().Err() != nil {
		return fail("Image test canceled before submission")
	}
	if modelID = strings.TrimSpace(modelID); modelID == "" {
		models := imageAccountModels(account)
		if len(models) == 0 {
			return fail("No image test model is configured for this protocol")
		}
		modelID = models[0].ID
	}
	if !account.IsModelSupported(modelID) {
		return fail("The selected model is not available for this image account")
	}
	if prompt = strings.TrimSpace(prompt); prompt == "" {
		prompt = defaultOpenAIImageTestPrompt
	}
	endpoint := openAIImagesGenerationsEndpoint
	payload := map[string]any{
		"model": modelID, "prompt": prompt, "n": 1,
		"size": "1024x1024", "response_format": "b64_json",
	}
	if image := strings.TrimSpace(opts.ImageDataURL); image != "" {
		if !imageModelSupportsReference(account, modelID) {
			return fail("Imagen models do not support reference images or edits")
		}
		if err := validateAccountTestDataURL(image, "image/"); err != nil {
			return fail("Reference image must be a valid image data URL within the size limit")
		}
		endpoint = openAIImagesEditsEndpoint
		payload["images"] = []map[string]string{{"image_url": image}}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fail("Failed to encode image test request")
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()
	ctx = WithResolvedTargetPlatform(ctx, PlatformImage)
	recorder := httptest.NewRecorder()
	forwardContext, _ := gin.CreateTestContext(recorder)
	forwardContext.Request = httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body)).WithContext(ctx)
	forwardContext.Request.Header.Set("Content-Type", "application/json")
	gateway := &OpenAIGatewayService{cfg: s.cfg, httpUpstream: imageAccountTestUpstream{HTTPUpstream: s.httpUpstream, ctx: ctx}}
	parsed, err := gateway.ParseOpenAIImagesRequest(forwardContext, body)
	if err != nil {
		return fail("Failed to prepare image test request")
	}
	s.sendEvent(c, TestEvent{Type: "test_start", Model: modelID})
	s.sendEvent(c, TestEvent{Type: "status", Text: "Generating one image (" + account.ImageProtocol() + ")..."})
	s.sendEvent(c, TestEvent{Type: "content", Text: "Image connection tests generate one 1K image and may incur upstream charges.\n"})
	result, err := gateway.ForwardImages(ctx, forwardContext, account, body, parsed, "")
	if err != nil {
		var typed *imagecompat.Error
		var failover *UpstreamFailoverError
		if errors.As(err, &failover) {
			typed = failover.ImagePlatformError
		} else {
			_ = errors.As(err, &typed)
		}
		if typed == nil {
			return fail("Image account test failed")
		}
		recordAccountTestUpstreamStatus(ctx, typed.UpstreamStatus)
		return s.sendErrorAndEnd(c, sanitizeImageAccountError(account, typed).Message)
	}
	var response struct {
		Data []struct {
			Base64        string `json:"b64_json"`
			URL           string `json:"url"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
	}
	if json.Unmarshal(recorder.Body.Bytes(), &response) != nil || result == nil || result.ImageCount == 0 {
		return fail("Image test returned no usable image")
	}
	for _, item := range response.Data {
		imageURL, mimeType := item.URL, "image/png"
		if item.Base64 != "" {
			data, decodeErr := base64.StdEncoding.DecodeString(item.Base64)
			if decodeErr != nil || len(data) == 0 {
				continue
			}
			mimeType = detectImageContentType(data)
			imageURL = "data:" + mimeType + ";base64," + item.Base64
		}
		if imageURL == "" {
			continue
		}
		if item.RevisedPrompt != "" {
			s.sendEvent(c, TestEvent{Type: "content", Text: item.RevisedPrompt})
		}
		s.sendEvent(c, TestEvent{Type: "image", ImageURL: imageURL, MimeType: mimeType})
		recordAccountTestUpstreamStatus(ctx, http.StatusOK)
		s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
		return nil
	}
	return fail("Image test returned no usable image")
}
