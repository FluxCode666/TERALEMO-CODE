package service

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

// Only Ark's documented draft reference shape is accepted. Ownership and the
// inherited billing inputs are resolved separately, before account admission.
func seedanceDraftTaskID(body []byte) (string, error) {
	for _, name := range []string{"draft_task_id", "draftTaskId", "draft_task"} {
		if gjson.GetBytes(body, name).Exists() {
			return "", ErrSeedanceDraftTaskUnavailable
		}
	}
	items := gjson.GetBytes(body, "content").Array()
	found := false
	for _, item := range items {
		if strings.EqualFold(item.Get("type").String(), "draft_task") || item.Get("draft_task").Exists() || item.Get("draft_task_id").Exists() {
			found = true
		}
	}
	if !found {
		return "", nil
	}
	if len(items) != 1 || items[0].Get("type").String() != "draft_task" || len(items[0].Map()) != 2 || len(items[0].Get("draft_task").Map()) != 1 {
		return "", ErrSeedanceDraftTaskUnavailable
	}
	id := items[0].Get("draft_task.id")
	if id.Type != gjson.String || strings.TrimSpace(id.String()) != id.String() || id.String() == "" || validateUpstreamPathSegment("Seedance draft task ID", id.String()) != nil {
		return "", ErrSeedanceDraftTaskUnavailable
	}
	// Ark inherits duration/ratio/seed/audio and every input media item. Even
	// equal values are forbidden; do not populate them from the billing receipt.
	allowed := map[string]bool{"model": true, "content": true, "resolution": true, "draft": true, "return_last_frame": true, "output_format": true, "watermark": true, "service_tier": true, "execution_expires_after": true, "priority": true, "callback_url": true, "safety_identifier": true}
	for name := range gjson.ParseBytes(body).Map() {
		if !allowed[name] {
			return "", ErrSeedanceDraftTaskUnavailable
		}
	}
	if gjson.GetBytes(body, "resolution").String() != "1080p" || gjson.GetBytes(body, "draft").Exists() && gjson.GetBytes(body, "draft").Type != gjson.False {
		return "", ErrSeedanceDraftTaskUnavailable
	}
	return id.String(), nil
}

func validateSeedanceDraftMode(body []byte, model, resolution string) error {
	draft := gjson.GetBytes(body, "draft")
	if !draft.Exists() {
		return nil
	}
	if draft.Type != gjson.True && draft.Type != gjson.False {
		return fmt.Errorf("invalid video request: Seedance draft must be a boolean")
	}
	if !draft.Bool() {
		return nil
	}
	if resolution != VideoBillingResolution480P {
		return fmt.Errorf("invalid video request: Seedance draft mode requires resolution 480p")
	}
	if model != "" {
		model = strings.ToLower(model)
		if !strings.Contains(model, "seedance-2-5") && !strings.Contains(model, "seedance-2.5") {
			if strings.Contains(model, "seedance") {
				return fmt.Errorf("invalid video request: Seedance draft mode requires a Seedance 2.5 model")
			}
			var texts []string
			for _, item := range gjson.GetBytes(body, "content").Array() {
				if item.Get("type").String() == "text" {
					texts = append(texts, item.Get("text").String())
				}
			}
			duration, frames, specified, err := seedanceTiming(body, strings.Join(texts, "\n"))
			if err != nil || !specified || duration <= 0 || frames > 0 {
				return fmt.Errorf("invalid video request: Seedance draft mode with an opaque deployment requires explicit fixed duration")
			}
		}
	}
	return nil
}
