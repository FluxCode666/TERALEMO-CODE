package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/imagecompat"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func isImagePlatformRequest(c *gin.Context) bool {
	platform, _ := ResolvedTargetPlatformFromContext(c.Request.Context())
	if platform == PlatformImage {
		return true
	}
	key, _ := c.Get("api_key")
	apiKey, ok := key.(*APIKey)
	return ok && apiKey != nil && apiKey.Group != nil && apiKey.Group.Platform == PlatformImage
}

func imageRequestError(param, message string) *imagecompat.Error {
	return &imagecompat.Error{HTTPStatus: http.StatusBadRequest, Type: "invalid_request_error", Param: param, Message: message, CallIndex: -1}
}

func redactImageAccountText(account *Account, text string) string {
	if account == nil {
		return text
	}
	if key := account.GetCredential("api_key"); key != "" {
		text = strings.ReplaceAll(text, key, "[redacted]")
	}
	base := account.ImageBaseURL()
	text = strings.ReplaceAll(text, base, "[redacted]")
	if parsed, err := url.Parse(base); err == nil && parsed.Hostname() != "" {
		text = strings.ReplaceAll(text, parsed.Hostname(), "[redacted]")
	}
	return text
}

func sanitizeImageAccountError(account *Account, typed *imagecompat.Error) *imagecompat.Error {
	copy := *typed
	copy.Message = redactImageAccountText(account, copy.Message)
	if account != nil && len(account.GetCredential("api_key")) > 0 && len(account.GetCredential("api_key")) < 6 {
		// Short secrets can occur in normalized codes/params as ordinary letters.
		// These fields come from the converter; discard provider prose instead.
		copy.Message = "Upstream rejected the request"
	} else {
		copy.Code = redactImageAccountText(account, copy.Code)
		copy.Param = redactImageAccountText(account, copy.Param)
	}
	return &copy
}

func sanitizeImagePlatformStreamError(account *Account, upstream *OpenAIImagesUpstreamError) (*OpenAIImagesUpstreamError, []byte) {
	typed := sanitizeImageAccountError(account, imagecompat.ConvertUpstreamError(imagecompat.ProtocolOpenAI, upstream.StatusCode, openAIImagesUpstreamErrorResponseBody(upstream)))
	if typed.Type == "content_policy_violation" {
		typed.Param = redactImageAccountText(account, upstream.Param)
		if upstream.Code != "" {
			typed.Code = redactImageAccountText(account, upstream.Code)
		}
	}
	upstream.ImagePlatformError = typed
	upstream.Message = typed.Message
	upstream.ErrorType = typed.Type
	upstream.Code = typed.Code
	upstream.Param = typed.Param
	body, _ := sjson.SetBytes(typed.OpenAIErrorBody(), "type", "error")
	return upstream, body
}

func (s *OpenAIGatewayService) imageCompatRequest(ctx context.Context, account *Account, parsed *OpenAIImagesRequest, model string) (imagecompat.Request, error) {
	req := imagecompat.Request{
		Operation: imagecompat.OperationGenerations, Model: model, Prompt: parsed.Prompt, N: parsed.N,
		Size: parsed.Size, Quality: parsed.Quality, ResponseFormat: parsed.ResponseFormat,
		Background: parsed.Background, OutputFormat: parsed.OutputFormat, Style: parsed.Style,
		Moderation: parsed.Moderation, InputFidelity: parsed.InputFidelity,
		OutputCompression: parsed.OutputCompression, PartialImages: parsed.PartialImages,
		Stream: parsed.Stream, HasMask: parsed.HasMask,
	}
	if err := validateImageCompatFields(parsed); err != nil {
		return req, err
	}
	if parsed.IsEdits() {
		req.Operation = imagecompat.OperationEdits
	}
	for _, upload := range parsed.Uploads {
		mimeType := upload.ContentType
		if mimeType == "" || mimeType == "application/octet-stream" {
			mimeType = detectedImageContentType(upload.Data)
		}
		req.Images = append(req.Images, imagecompat.InputImage{Data: upload.Data, MIMEType: mimeType})
	}
	protocol := imagecompat.Protocol(account.ImageProtocol())
	for _, raw := range parsed.InputImageURLs {
		input := imagecompat.InputImage{URL: raw}
		if strings.HasPrefix(strings.ToLower(raw), "data:") {
			parts := strings.SplitN(raw, ",", 2)
			if len(parts) != 2 || !strings.HasSuffix(strings.ToLower(parts[0]), ";base64") {
				return req, imageRequestError("image", "Reference image must be a base64 image data URL")
			}
			data, err := base64.StdEncoding.DecodeString(parts[1])
			if err != nil || len(data) == 0 || len(data) > openAIImageMaxDownloadBytes {
				return req, imageRequestError("image", "Reference image data is invalid or exceeds 20 MiB")
			}
			input = imagecompat.InputImage{Data: data, MIMEType: strings.TrimSuffix(strings.TrimPrefix(parts[0], "data:"), ";base64")}
		} else if imagecompat.NeedsInlineImageData(protocol) {
			encoded, err := s.fetchOpenAIImageURLBase64(ctx, account, raw)
			if err != nil {
				return req, imageRequestError("image", "Reference image could not be downloaded from an allowed public URL")
			}
			data, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return req, imageRequestError("image", "Reference image data is invalid")
			}
			input = imagecompat.InputImage{Data: data, MIMEType: detectedImageContentType(data)}
		}
		req.Images = append(req.Images, input)
	}
	return req, nil
}

func validateImageCompatFields(parsed *OpenAIImagesRequest) error {
	allowed := map[string]bool{
		"model": true, "prompt": true, "n": true, "size": true, "quality": true,
		"response_format": true, "background": true, "output_format": true,
		"style": true, "moderation": true, "input_fidelity": true,
		"output_compression": true, "partial_images": true, "stream": true,
		"mask": true, "images": true, "image": true,
	}
	if parsed.Multipart {
		_, params, err := mime.ParseMediaType(parsed.ContentType)
		if err != nil {
			return imageRequestError("body", "Invalid multipart request")
		}
		reader := multipart.NewReader(bytes.NewReader(parsed.Body), params["boundary"])
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return imageRequestError("body", "Invalid multipart request")
			}
			name := part.FormName()
			_ = part.Close()
			if !allowed[name] && !strings.HasPrefix(name, "image[") {
				return unsupportedImageRequestField(name)
			}
		}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(parsed.Body, &fields) != nil {
		return imageRequestError("body", "Invalid image request")
	}
	for name := range fields {
		if !allowed[name] {
			return unsupportedImageRequestField(name)
		}
	}
	return nil
}

func unsupportedImageRequestField(name string) *imagecompat.Error {
	err := imageRequestError(name, "Unsupported image request parameter")
	err.Code = "unsupported_parameter"
	return err
}

func (s *OpenAIGatewayService) writeImagePlatformError(c *gin.Context, err error) error {
	var typed *imagecompat.Error
	if !errors.As(err, &typed) {
		typed = &imagecompat.Error{HTTPStatus: http.StatusBadGateway, Type: "upstream_error", Message: "Image upstream request failed", CallIndex: -1}
	}
	if value, ok := c.Get("image_platform_account"); ok {
		if account, ok := value.(*Account); ok {
			typed = sanitizeImageAccountError(account, typed)
		}
	}
	c.Header("Content-Type", "application/json")
	c.Data(typed.HTTPStatus, "application/json", typed.OpenAIErrorBody())
	MarkResponseCommitted(c)
	return typed
}

func (s *OpenAIGatewayService) handleImagePlatformUpstreamError(ctx context.Context, c *gin.Context, account *Account, resp *http.Response, body []byte, model string) error {
	typed := sanitizeImageAccountError(account, imagecompat.ConvertUpstreamError(imagecompat.Protocol(account.ImageProtocol()), resp.StatusCode, body))
	// Keep cooling/rate-limit side effects, but never put an upstream body in a
	// failover error: the exhausted handler may expose its error message.
	s.imagePlatformFailureSideEffects(ctx, resp, account, typed)
	if typed.Retryable {
		return imagePlatformFailoverError(typed, resp.Header)
	}
	return s.writeImagePlatformError(c, typed)
}

func imagePlatformFailoverError(typed *imagecompat.Error, headers http.Header) *UpstreamFailoverError {
	safeHeaders := http.Header{}
	if retry := headers.Get("Retry-After"); retry != "" {
		safeHeaders.Set("Retry-After", retry)
	}
	return &UpstreamFailoverError{StatusCode: typed.UpstreamStatus, ResponseBody: typed.OpenAIErrorBody(), ResponseHeaders: safeHeaders, ImagePlatformError: typed}
}

func imagePlatformResponseFailoverError(typed *imagecompat.Error, responses []*http.Response) *UpstreamFailoverError {
	var headers http.Header
	if typed.CallIndex >= 0 && typed.CallIndex < len(responses) && responses[typed.CallIndex] != nil {
		headers = responses[typed.CallIndex].Header
	}
	return imagePlatformFailoverError(typed, headers)
}

func (s *OpenAIGatewayService) imagePlatformFailureSideEffects(ctx context.Context, resp *http.Response, account *Account, typed *imagecompat.Error) {
	if s.rateLimitService == nil || !typed.Retryable {
		return
	}
	status := resp.StatusCode
	if status == 401 || (status == 400 && typed.Code == "upstream_auth_failed") {
		status = http.StatusUnauthorized
	} else if status == http.StatusBadRequest {
		status = http.StatusPaymentRequired
	}
	// Classification used the raw response; state/log consumers receive only
	// the normalized error, never a provider body or echoed credentials.
	s.rateLimitService.HandleUpstreamError(ctx, account, status, resp.Header, typed.OpenAIErrorBody())
}

func imageFailurePriority(typed *imagecompat.Error) int {
	if !typed.Retryable {
		return 0
	}
	if typed.UpstreamStatus == 403 || typed.UpstreamStatus == 402 {
		return 4
	}
	switch typed.Code {
	case "upstream_auth_failed":
		return 5
	case "upstream_quota_exhausted", "upstream_account_unavailable":
		return 4
	}
	if typed.UpstreamStatus == 429 {
		return 3
	}
	return 1
}

func (s *OpenAIGatewayService) forwardImagePlatformCompatible(ctx context.Context, c *gin.Context, account *Account, parsed *OpenAIImagesRequest, channelModel string) (*OpenAIForwardResult, error) {
	started := time.Now()
	if account.Type != AccountTypeAPIKey {
		return nil, s.writeImagePlatformError(c, imageRequestError("account", "Image accounts require API key credentials"))
	}
	model := parsed.Model
	if strings.TrimSpace(channelModel) != "" {
		model = channelModel
	}
	upstreamModel := account.GetMappedModel(model)
	SetOpsUpstreamModel(c, upstreamModel)
	protocol, err := imagecompat.ParseProtocol(account.ImageProtocol())
	if err != nil {
		return nil, s.writeImagePlatformError(c, imageRequestError("image_protocol", "Invalid image account protocol"))
	}
	request, err := s.imageCompatRequest(ctx, account, parsed, upstreamModel)
	if err != nil {
		return nil, s.writeImagePlatformError(c, err)
	}
	plan, err := imagecompat.BuildPlan(protocol, request)
	if err != nil {
		return nil, s.writeImagePlatformError(c, err)
	}
	if len(plan.Calls) > 0 {
		SetActualOpenAIUpstreamEndpoint(c, plan.Calls[0].Path)
	}
	base, err := s.validateUpstreamBaseURL(account.ImageBaseURL())
	if err != nil {
		return nil, s.writeImagePlatformError(c, errors.New("invalid upstream configuration"))
	}
	base = strings.TrimRight(base, "/")
	base = strings.TrimSuffix(strings.TrimSuffix(base, "/v1beta"), "/v1")
	proxy := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	// Match the image gateway's disconnect semantics with a finite timeout.
	upstreamCtx, release := detachUpstreamContext(ctx)
	defer release()
	upstreamTimeout := 10 * time.Minute
	if deadline, ok := ctx.Deadline(); ok {
		upstreamTimeout = min(upstreamTimeout, time.Until(deadline))
	}
	upstreamCtx, cancel := context.WithTimeout(WithHTTPUpstreamRedirectsDisabled(upstreamCtx), upstreamTimeout)
	defer cancel()
	results, responses := s.executeImageCompatPlan(upstreamCtx, account, plan, base, proxy)
	var lastResponse *http.Response
	failureIndex, priority := -1, 0
	for i, resp := range responses {
		if resp == nil {
			continue
		}
		lastResponse = resp
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			typed := imagecompat.ConvertUpstreamError(protocol, resp.StatusCode, results[i].Body)
			if rank := imageFailurePriority(typed); rank > priority {
				failureIndex, priority = i, rank
			}
		}
	}
	if failureIndex >= 0 {
		resp := responses[failureIndex]
		typed := sanitizeImageAccountError(account, imagecompat.ConvertUpstreamError(protocol, resp.StatusCode, results[failureIndex].Body))
		s.imagePlatformFailureSideEffects(upstreamCtx, resp, account, typed)
	}
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(started).Milliseconds())
	response, err := imagecompat.BuildResponse(plan, results, time.Now())
	if err != nil {
		var typed *imagecompat.Error
		if errors.As(err, &typed) {
			typed = sanitizeImageAccountError(account, typed)
			if typed.Retryable {
				return nil, imagePlatformResponseFailoverError(typed, responses)
			}
			err = typed
		}
		return nil, s.writeImagePlatformError(c, err)
	}
	if protocol == imagecompat.ProtocolGrok && plan.ResponseFormat != "url" {
		if err := s.backfillImagePlatformResponse(upstreamCtx, account, response); err != nil {
			return nil, s.writeImagePlatformError(c, err)
		}
	}
	usage := OpenAIUsage{}
	if response.Usage != nil {
		usage.InputTokens, usage.OutputTokens = response.Usage.InputTokens, response.Usage.OutputTokens
	}
	c.Data(http.StatusOK, "application/json", response.Body)
	result := &OpenAIForwardResult{Model: model, UpstreamModel: upstreamModel, Usage: usage,
		ImageCount: response.Images, ImageSize: parsed.SizeTier, ImageInputSize: parsed.Size, ImagePlanSizeTier: plan.SizeTier,
		ImageOutputSizes: collectOpenAIResponseImageOutputSizesFromJSONBytes(response.Body), Duration: time.Since(started)}
	ApplyOpenAIImageBillingResolution(result)
	if lastResponse != nil {
		result.RequestID = lastResponse.Header.Get("x-request-id")
		result.ResponseHeaders = lastResponse.Header.Clone()
		result.UpstreamHeaders = lastResponse.Header.Clone()
	}
	return result, nil
}

func (s *OpenAIGatewayService) backfillImagePlatformResponse(ctx context.Context, account *Account, response *imagecompat.Response) error {
	for i, item := range gjson.GetBytes(response.Body, "data").Array() {
		if item.Get("b64_json").String() != "" || item.Get("url").String() == "" {
			continue
		}
		encoded, err := s.fetchOpenAIImageURLBase64(ctx, account, item.Get("url").String())
		if err != nil {
			return errors.New("image response could not be converted to b64_json")
		}
		response.Body, err = sjson.SetBytes(response.Body, fmt.Sprintf("data.%d.b64_json", i), encoded)
		if err != nil {
			return err
		}
	}
	return nil
}

// Workers only write their own result slot. Gin and account side effects remain
// on the caller goroutine, after every call has finished (including failures).
func (s *OpenAIGatewayService) executeImageCompatPlan(ctx context.Context, account *Account, plan *imagecompat.Plan, base, proxy string) ([]imagecompat.UpstreamResult, []*http.Response) {
	results := make([]imagecompat.UpstreamResult, len(plan.Calls))
	responses := make([]*http.Response, len(plan.Calls))
	var wg sync.WaitGroup
	limit := make(chan struct{}, 4)
	for i, call := range plan.Calls {
		wg.Go(func() {
			limit <- struct{}{}
			defer func() { <-limit }()
			results[i].StatusCode = http.StatusBadGateway
			req, err := http.NewRequestWithContext(ctx, call.Method, base+call.Path, bytes.NewReader(call.Body))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", call.ContentType)
			req.Header.Set("Accept", "application/json")
			if plan.Protocol == imagecompat.ProtocolGemini {
				req.Header.Set("x-goog-api-key", account.GetCredential("api_key"))
			} else {
				req.Header.Set("Authorization", "Bearer "+account.GetCredential("api_key"))
			}
			resp, err := s.httpUpstream.Do(req, proxy, account.ID, account.Concurrency)
			if err != nil {
				return
			}
			responses[i] = resp
			body, err := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
			_ = resp.Body.Close()
			if err == nil {
				results[i] = imagecompat.UpstreamResult{StatusCode: resp.StatusCode, Body: body}
			}
		})
	}
	wg.Wait()
	return results, responses
}
