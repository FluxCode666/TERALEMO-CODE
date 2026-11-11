package service

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"strings"

	"github.com/tidwall/gjson"
)

func validateImagePlatformInput(parsed *OpenAIImagesRequest, converted bool) error {
	if parsed.Multipart {
		_, params, err := mime.ParseMediaType(parsed.ContentType)
		if err != nil {
			return imageRequestError("body", "Invalid multipart request")
		}
		reader := multipart.NewReader(bytes.NewReader(parsed.Body), params["boundary"])
		seen := map[string]bool{}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return imageRequestError("body", "Invalid multipart request")
			}
			name := part.FormName()
			isImage := name == "image" || name == "image[]" || strings.HasPrefix(name, "image[")
			isFile := part.FileName() != ""
			_ = part.Close()
			if name == "mask" && converted {
				return imageRequestError("mask", "Mask is not supported for this image protocol")
			}
			if isFile && !isImage && name != "mask" {
				return imageRequestError(name, "Image option must be a text field")
			}
			if isImage && (!parsed.IsEdits() || !isFile) {
				return imageRequestError(name, "Reference images require edits and a file upload")
			}
			if name == "images" {
				return imageRequestError(name, "Use image file uploads for multipart edits")
			}
			if !isFile && seen[name] {
				return imageRequestError(name, "Duplicate image option")
			}
			seen[name] = true
		}
		return nil
	}
	root := gjson.ParseBytes(parsed.Body)
	if !root.IsObject() {
		return imageRequestError("body", "Image request must be a JSON object")
	}
	if !parsed.IsEdits() && (root.Get("image").Exists() || root.Get("images").Exists()) {
		return imageRequestError("images", "Reference images require the edits endpoint")
	}
	for _, name := range []string{"n", "partial_images", "output_compression"} {
		value := root.Get(name)
		if value.Exists() && (value.Type != gjson.Number || math.Trunc(value.Float()) != value.Float()) {
			return imageRequestError(name, "Image option must be an integer")
		}
	}
	for _, name := range []string{"model", "prompt", "size", "quality", "response_format", "background", "output_format", "style", "moderation", "input_fidelity"} {
		value := root.Get(name)
		if value.Exists() && value.Type != gjson.String {
			return imageRequestError(name, "Image option must be a string")
		}
	}
	if value := root.Get("stream"); value.Exists() && value.Type != gjson.True && value.Type != gjson.False {
		return imageRequestError("stream", "Stream must be a boolean")
	}
	if parsed.IsEdits() {
		if root.Get("image").Exists() {
			return imageRequestError("image", "Use images[].image_url for JSON edits")
		}
		for _, item := range root.Get("images").Array() {
			var fields map[string]json.RawMessage
			if !item.IsObject() || json.Unmarshal([]byte(item.Raw), &fields) != nil || len(fields) != 1 || item.Get("image_url").Type != gjson.String || strings.TrimSpace(item.Get("image_url").String()) == "" {
				return imageRequestError("images", "Each reference must contain only image_url")
			}
		}
	}
	return nil
}
