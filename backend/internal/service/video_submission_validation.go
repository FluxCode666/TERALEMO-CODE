package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/tidwall/gjson"
)

// ValidateVideoSubmissionJSON prevents ambiguous JSON from being interpreted
// differently by the gateway's first-key parser and an upstream's last-key
// parser. It also rejects multiple values, a non-object root and excess depth.
func ValidateVideoSubmissionJSON(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return fmt.Errorf("request body must be a JSON object")
	}
	var readValue func(json.Token, int) error
	readValue = func(token json.Token, depth int) error {
		if depth > 128 {
			return fmt.Errorf("video request JSON exceeds maximum nesting depth")
		}
		delim, composite := token.(json.Delim)
		if !composite {
			return nil
		}
		seen := map[string]struct{}{}
		for decoder.More() {
			if delim == '{' {
				keyToken, tokenErr := decoder.Token()
				if tokenErr != nil {
					return fmt.Errorf("invalid video request JSON: %w", tokenErr)
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("invalid video request JSON object key")
				}
				if _, duplicate := seen[key]; duplicate {
					return fmt.Errorf("duplicate video request JSON object key %q", key)
				}
				seen[key] = struct{}{}
			}
			value, tokenErr := decoder.Token()
			if tokenErr != nil {
				return fmt.Errorf("invalid video request JSON: %w", tokenErr)
			}
			if err := readValue(value, depth+1); err != nil {
				return err
			}
		}
		closing, tokenErr := decoder.Token()
		if tokenErr != nil || delim == '{' && closing != json.Delim('}') || delim == '[' && closing != json.Delim(']') {
			return fmt.Errorf("invalid video request JSON container")
		}
		return nil
	}
	if err := readValue(first, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("video request body must contain exactly one JSON object")
	}
	return nil
}

// RejectUnsupportedVideoBillingFields rejects explicit cross-protocol output
// controls, rather than allowing a compatible upstream to apply controls absent
// from the frozen contract. Only known configuration containers are inspected;
// content, media metadata and arbitrary future fields retain native passthrough.
func RejectUnsupportedVideoBillingFields(body []byte, protocol string) error {
	if protocol != VideoProtocolSeedance && protocol != VideoProtocolMiniMax {
		return fmt.Errorf("unsupported video billing validation protocol")
	}
	quantity := map[string]bool{
		"n": true, "num": true, "count": true, "sampleCount": true, "sample_count": true,
		"num_videos": true, "numVideos": true, "number_of_videos": true, "numberOfVideos": true,
		"video_count": true, "videoCount": true, "batch_size": true, "batchSize": true,
	}
	aliases := map[string]bool{
		"duration_seconds": true, "durationSeconds": true, "durationseconds": true,
		"fps": true, "frame": true, "framespersecond": true, "frames_per_second": true,
		"num_frames": true, "numFrames": true, "frame_count": true, "frameCount": true,
		"video_resolution": true, "videoResolution": true, "size": true, "width": true, "height": true,
	}
	containers := map[string]bool{
		"parameters": true, "generation_config": true, "generationConfig": true,
		"video_config": true, "videoConfig": true, "extra": true,
		"extra_body": true, "extraBody": true, "options": true,
	}
	var inspect func(gjson.Result, string, bool) error
	inspect = func(object gjson.Result, path string, nested bool) error {
		var fieldErr error
		object.ForEach(func(key, value gjson.Result) bool {
			name := key.String()
			field := strings.TrimPrefix(path+"."+name, ".")
			if quantity[name] || aliases[name] || name == "frames" && protocol == VideoProtocolMiniMax ||
				nested && (name == "duration" || name == "frames" || name == "resolution") {
				fieldErr = fmt.Errorf("unsupported %s video billing field %s", protocol, field)
				return false
			}
			if containers[name] && (value.IsObject() || value.IsArray()) {
				if value.IsArray() {
					for _, item := range value.Array() {
						if fieldErr = inspect(item, field, true); fieldErr != nil {
							return false
						}
					}
				} else if fieldErr = inspect(value, field, true); fieldErr != nil {
					return false
				}
			}
			return true
		})
		return fieldErr
	}
	return inspect(gjson.ParseBytes(body), "", false)
}
