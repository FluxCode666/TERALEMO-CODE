package service

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// The version belongs to the configured API path, not to a particular proxy.
func miniMaxVideoV2(base string) bool {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return false
	}
	path := strings.TrimRight(u.Path, "/")
	for _, suffix := range []string{"/query/video_generation", "/files/retrieve", "/video_generation"} {
		path = strings.TrimSuffix(path, suffix)
	}
	return strings.HasSuffix(path, "/v2")
}

func miniMaxH3Model(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "minimax-h3")
}

// Accept legacy prompt/first-frame clients on a v2 account as well. Native v2
// content and additional fields remain intact; validation never retries creates.
func prepareMiniMaxVideoV2(body []byte) ([]byte, error) {
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return nil, fmt.Errorf("request body must be a JSON object")
	}
	if gjson.GetBytes(body, "content").Exists() {
		return body, nil
	}
	content := []map[string]any{{"type": "text", "text": gjson.GetBytes(body, "prompt").String()}}
	ratio := "16:9"
	for _, field := range []string{"first_frame_image", "last_frame_image"} {
		if image := gjson.GetBytes(body, field).String(); image != "" {
			content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": image}, "role": strings.TrimSuffix(field, "_image")})
			ratio = "adaptive"
		}
	}
	for _, subject := range gjson.GetBytes(body, "subject_reference").Array() {
		for _, image := range subject.Get("image").Array() {
			content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": image.String()}, "role": "reference_image"})
			ratio = "adaptive"
		}
	}
	var err error
	body, err = sjson.SetBytes(body, "content", content)
	if err != nil {
		return nil, err
	}
	if !gjson.GetBytes(body, "ratio").Exists() {
		body, err = sjson.SetBytes(body, "ratio", ratio)
		if err != nil {
			return nil, err
		}
	}
	for _, field := range []string{"prompt", "first_frame_image", "last_frame_image", "subject_reference"} {
		body, err = sjson.DeleteBytes(body, field)
		if err != nil {
			return nil, err
		}
	}
	return body, nil
}

func parseMiniMaxVideoV2(body []byte, info GrokMediaRequestInfo) (GrokMediaRequestInfo, error) {
	return parseMiniMaxVideoV2WithLimits(body, info, nil)
}

func parseMiniMaxVideoV2WithLimits(body []byte, info GrokMediaRequestInfo, limits *videoModelLimits) (GrokMediaRequestInfo, error) {
	info.N = 1
	content := gjson.GetBytes(body, "content")
	if !content.IsArray() || len(content.Array()) == 0 {
		return info, fmt.Errorf("MiniMax v2 content must be a non-empty array")
	}
	media, frames, references := false, false, false
	for _, item := range content.Array() {
		switch kind := item.Get("type").String(); kind {
		case "text":
			if text := item.Get("text"); text.Type == gjson.String && strings.TrimSpace(text.String()) != "" {
				if info.Prompt != "" {
					info.Prompt += "\n"
				}
				info.Prompt += text.String()
			}
		case "image_url", "video_url", "audio_url":
			link := item.Get(kind + ".url")
			if link.Type != gjson.String || strings.TrimSpace(link.String()) == "" {
				return info, fmt.Errorf("MiniMax v2 %s.url is required", kind)
			}
			media = true
			if kind == "image_url" {
				info.InputImageURLs = append(info.InputImageURLs, link.String())
			}
			role := item.Get("role").String()
			frames = frames || role == "first_frame" || role == "last_frame" || kind == "image_url" && role == ""
			references = references || strings.HasPrefix(role, "reference_")
		default:
			return info, fmt.Errorf("unsupported MiniMax v2 content type")
		}
	}
	if strings.TrimSpace(info.Prompt) == "" {
		return info, fmt.Errorf("MiniMax v2 content requires a non-empty text prompt")
	}
	if frames && references {
		return info, fmt.Errorf("MiniMax v2 first/last frames cannot be mixed with reference media")
	}
	ratio := gjson.GetBytes(body, "ratio").String()
	switch ratio {
	case "", "adaptive":
		if !media {
			return info, fmt.Errorf("MiniMax v2 text-to-video requires ratio, for example 16:9")
		}
	case "21:9", "16:9", "4:3", "1:1", "3:4", "9:16":
	default:
		return info, fmt.Errorf("invalid MiniMax v2 ratio")
	}
	if limits != nil {
		duration := gjson.GetBytes(body, "duration")
		seconds, err := geminiVideoInteger(duration, "MiniMax v2 duration", 6, 1, 60)
		if err != nil {
			return info, err
		}
		resolution := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "resolution").String()))
		if resolution != "" && !slices.Contains(videoCapabilityResolutionOrder, resolution) {
			return info, fmt.Errorf("MiniMax v2 resolution must be 480P, 720P, 768P, 1080P or 2K")
		}
		explicit := resolution != ""
		if !explicit {
			resolution = VideoBillingResolution768P
		}
		info.Resolution, info.DurationSeconds, err = limits.normalize(resolution, seconds, explicit, duration.Exists())
		info.DurationSecondsExact = float64(info.DurationSeconds)
		return info, err
	}
	info.DurationSeconds = 6
	minimum := int64(4)
	if strings.EqualFold(info.Model, "MiniMax-H3-Max") {
		minimum = 5
	}
	if duration := gjson.GetBytes(body, "duration"); duration.Exists() {
		if duration.Type != gjson.Number || duration.Float() != float64(duration.Int()) || duration.Int() < minimum || duration.Int() > 15 {
			return info, fmt.Errorf("MiniMax v2 duration must be an integer from %d to 15 seconds", minimum)
		}
		info.DurationSeconds = int(duration.Int())
	}
	switch resolution := strings.ToUpper(strings.TrimSpace(gjson.GetBytes(body, "resolution").String())); resolution {
	case "", "768P":
		info.Resolution = VideoBillingResolution768P
	case "480P":
		if strings.EqualFold(info.Model, "MiniMax-H3") {
			return info, fmt.Errorf("MiniMax-H3 supports 768P; use MiniMax-H3-Max for 480P")
		}
		info.Resolution = VideoBillingResolution480P
	case "2K":
		if strings.EqualFold(info.Model, "MiniMax-H3-Max") {
			return info, fmt.Errorf("MiniMax-H3-Max supports 480P or 768P; use MiniMax-H3 for 2K")
		}
		info.Resolution = VideoBillingResolution2K
	default:
		return info, fmt.Errorf("MiniMax v2 resolution must be 480P, 768P or 2K")
	}
	return info, nil
}

func observeMiniMaxVideoV2(body []byte, id string) (*OpenAIForwardResult, error) {
	task := gjson.GetBytes(body, "task")
	if !task.IsObject() || task.Get("id").String() != id {
		return nil, fmt.Errorf("MiniMax returned a different task ID or missing task")
	}
	result := &OpenAIForwardResult{AsyncTaskStatus: "processing"}
	switch task.Get("status").String() {
	case "queued", "running":
	case "succeeded":
		result.AsyncTaskURL = task.Get("content.url").String()
		if strings.TrimSpace(result.AsyncTaskURL) == "" {
			return nil, fmt.Errorf("MiniMax completed task missing video URL")
		}
		result.AsyncTaskStatus, result.VideoCount = "completed", 1
	case "failed", "cancelled":
		result.AsyncTaskStatus = task.Get("status").String()
		result.AsyncTaskError = task.Get("error.message").String()
	default:
		return nil, fmt.Errorf("MiniMax returned an unknown task status")
	}
	return result, nil
}
