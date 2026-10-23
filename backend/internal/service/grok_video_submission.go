package service

import (
	"fmt"
	"math"
	"mime"
	"regexp"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// xAI's REST API documents top-level duration (including its seconds alias),
// resolution and a single result per task. SDK providerOptions and text command
// syntaxes are not REST controls. Never forward an unpriced alternate control.
// https://docs.x.ai/developers/rest-api-reference/inference/videos
var grokVideoUsageCommand = regexp.MustCompile(`(?i)(^|[\s\p{Z}])--(?:n|num|count|(?:num|number[-_]?of)[-_]?(?:videos|outputs)|(?:video|output|sample|candidate)[-_]?count|batch[-_]?size|max[-_]?num[-_]?results|duration|dur|seconds|duration[-_]?(?:seconds|secs|sec)|video[-_]?(?:duration|length|resolution)|length(?:[-_]?seconds)?|resolution|res|output[-_]?resolution|size|width|height|frames?|num[-_]?frames|frame[-_]?count|fps|frame[-_]?rate|frames[-_]?per[-_]?second)(?:[=:\s\p{Z}]|$)`)

// NormalizeGrokVideoSubmission freezes the same generation controls that are
// sent upstream. The handler must call it before reserving money; forwarding
// also calls it so internal callers cannot bypass the protocol validation.
//
// Edit inherits source duration/resolution, and extension duration describes
// only the new segment. Preserve their established forwarding and parsing here;
// reservation policy for these source-dependent endpoints is handled separately.
func NormalizeGrokVideoSubmission(body []byte, contentType string, endpoint GrokMediaEndpoint) ([]byte, GrokMediaRequestInfo, error) {
	var info GrokMediaRequestInfo
	switch endpoint {
	case GrokMediaEndpointVideosEdits, GrokMediaEndpointVideosExtensions:
		return body, ParseGrokMediaRequest(contentType, body), nil
	case GrokMediaEndpointVideosGenerations:
	default:
		return nil, info, fmt.Errorf("internal error: Grok video submission normalization requires a video creation endpoint")
	}
	if strings.TrimSpace(contentType) != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || !strings.EqualFold(mediaType, "application/json") {
			return nil, info, fmt.Errorf("invalid video request: Grok video submissions require Content-Type application/json")
		}
	}
	if err := ValidateVideoSubmissionJSON(body); err != nil {
		return nil, info, err
	}
	root := gjson.ParseBytes(body)
	if err := rejectGrokVideoAlternateControls(root, ""); err != nil {
		return nil, info, err
	}
	model := root.Get("model")
	if model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		return nil, info, fmt.Errorf("model must be a non-empty string")
	}
	if prompt := root.Get("prompt"); prompt.Exists() {
		if prompt.Type != gjson.String {
			return nil, info, fmt.Errorf("invalid video request: Grok video prompt must be a string")
		}
		if grokVideoUsageCommand.MatchString(prompt.String()) {
			return nil, info, fmt.Errorf("invalid video request: Grok video prompt usage commands are unsupported; use top-level duration, seconds or resolution fields")
		}
	}
	if n := root.Get("n"); n.Exists() && (n.Type != gjson.Number || n.Float() != 1) {
		return nil, info, fmt.Errorf("invalid video request: Grok video generation returns one video per task; n must be 1")
	}
	duration, err := grokVideoSubmissionDuration(root)
	if err != nil {
		return nil, info, err
	}
	resolution := VideoBillingResolution480P
	if value := root.Get("resolution"); value.Exists() {
		if value.Type != gjson.String {
			return nil, info, fmt.Errorf("invalid video request: Grok video resolution must be a recognized resolution string")
		}
		var known bool
		resolution, known = LookupVideoBillingResolution(value.String())
		if !known {
			return nil, info, fmt.Errorf("invalid video request: Grok video resolution must be 480p, 720p, 768p, 1080p or 2K")
		}
	}
	// Keep explicit 768p/2K relay tiers: an account can map even a built-in
	// public model to a compatible upstream. Official xAI model restrictions
	// cannot safely be inferred from the public model name before selection.
	out := body
	for _, field := range []struct {
		name  string
		value any
	}{{"duration", duration}, {"resolution", resolution}} {
		out, err = sjson.SetBytes(out, field.name, field.value)
		if err != nil {
			return nil, info, fmt.Errorf("normalize Grok video %s: %w", field.name, err)
		}
	}
	// seconds is an official input alias, while n=1 is a gateway compatibility
	// hint. Forward only the canonical duration and the native single result.
	for _, field := range []string{"seconds", "n"} {
		out, err = sjson.DeleteBytes(out, field)
		if err != nil {
			return nil, info, fmt.Errorf("normalize Grok video %s: %w", field, err)
		}
	}
	info = ParseGrokMediaRequest("application/json", out)
	info.N = 1
	info.DurationSeconds, info.DurationSecondsExact = duration, float64(duration)
	info.Resolution = resolution
	return out, info, nil
}

func grokVideoSubmissionDuration(root gjson.Result) (int, error) {
	duration := 0
	for _, field := range []string{"duration", "seconds"} {
		value := root.Get(field)
		// Both fields are nullable in the official compatibility schema.
		if !value.Exists() || value.Type == gjson.Null {
			continue
		}
		var parsed int
		switch value.Type {
		case gjson.Number:
			seconds := value.Float()
			if math.IsInf(seconds, 0) || math.IsNaN(seconds) || seconds != math.Trunc(seconds) || seconds < 1 || seconds > 15 {
				return 0, fmt.Errorf("invalid video request: Grok video %s must be an integer from 1 to 15 seconds", field)
			}
			parsed = int(seconds)
		case gjson.String:
			var err error
			parsed, err = strconv.Atoi(strings.TrimSpace(value.String()))
			if err != nil || parsed < 1 || parsed > 15 {
				return 0, fmt.Errorf("invalid video request: Grok video %s must be an integer from 1 to 15 seconds", field)
			}
		default:
			return 0, fmt.Errorf("invalid video request: Grok video %s must be an integer from 1 to 15 seconds", field)
		}
		if duration != 0 && duration != parsed {
			return 0, fmt.Errorf("invalid video request: Grok video duration and seconds must agree")
		}
		duration = parsed
	}
	if duration == 0 {
		duration = VideoBillingDefaultDurationSeconds
	}
	return duration, nil
}

func rejectGrokVideoAlternateControls(value gjson.Result, path string) error {
	if value.IsArray() {
		for i, item := range value.Array() {
			if err := rejectGrokVideoAlternateControls(item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	}
	if !value.IsObject() {
		return nil
	}
	var fieldErr error
	value.ForEach(func(key, child gjson.Result) bool {
		name := key.String()
		field := name
		if path != "" {
			field = path + "." + name
		}
		canonical := path == "" && (name == "n" || name == "duration" || name == "seconds" || name == "resolution")
		if !canonical && grokVideoUsageControlName(name) {
			fieldErr = fmt.Errorf("unsupported Grok video usage control %s; use top-level duration, seconds and resolution and submit one video per task", field)
			return false
		}
		// The video REST endpoint already selects the mode. Do not let relay-only
		// inputs silently turn generation into source-dependent edit/extension.
		switch normalizedGrokVideoControlName(name) {
		case "video", "videourl", "sourcevideo", "sourcevideourl", "mode":
			fieldErr = fmt.Errorf("invalid video request: Grok video generation does not accept source-dependent mode control %s", field)
			return false
		}
		if path != "" && (name == "prompt" || name == "text") && child.Type == gjson.String && grokVideoUsageCommand.MatchString(child.String()) {
			fieldErr = fmt.Errorf("unsupported Grok video prompt usage command in %s", field)
			return false
		}
		fieldErr = rejectGrokVideoAlternateControls(child, field)
		return fieldErr == nil
	})
	return fieldErr
}

func normalizedGrokVideoControlName(name string) string {
	return strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(strings.ToLower(name))
}

func grokVideoUsageControlName(name string) bool {
	switch normalizedGrokVideoControlName(name) {
	case "n", "num", "count", "videocount", "numvideos", "numberofvideos", "numoutputs", "numberofoutputs", "outputcount", "samplecount", "candidatecount", "batchsize", "maxnumresults",
		"duration", "seconds", "durationseconds", "durationsecs", "durationsec", "durations", "videoduration", "videolength", "length", "lengthseconds",
		"resolution", "videoresolution", "outputresolution", "size", "width", "height", "frames", "frame", "numframes", "framecount", "fps", "framerate", "framespersecond":
		return true
	default:
		return false
	}
}
