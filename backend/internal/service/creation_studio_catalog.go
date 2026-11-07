package service

import (
	"context"
	"net/http"
	"sort"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// Creation availability reason codes (contract §3.1).
const (
	CreationUnavailableNoGroup             = "no_group"
	CreationUnavailablePlatformUnsupported = "platform_unsupported"
	CreationUnavailableStorageDisabled     = "storage_disabled"
	CreationUnavailablePermissionDenied    = "permission_denied"
	CreationUnavailableNoAccounts          = "no_accounts"
	CreationUnavailableKeyUnavailable      = "key_unavailable"
)

// CreationParamKind is one of the four descriptor kinds. Booleans are select.
type CreationParamKind string

const (
	CreationParamKindSelect  CreationParamKind = "select"
	CreationParamKindInteger CreationParamKind = "integer"
	CreationParamKindRatio   CreationParamKind = "ratio"
	CreationParamKindImage   CreationParamKind = "image"
)

// CreationParamOption is one selectable value. Select options use the label
// fields, ratio options use W/H; empty values are omitted from the JSON so each
// kind matches its own TS shape.
type CreationParamOption struct {
	Value    string `json:"value"`
	LabelKey string `json:"labelKey,omitempty"`
	Label    string `json:"label,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	HintKey  string `json:"hintKey,omitempty"`
	W        int    `json:"w,omitempty"`
	H        int    `json:"h,omitempty"`
}

// CreationParamDescriptor is the panel-side description of one parameter.
type CreationParamDescriptor struct {
	Key            string                `json:"key"`
	Kind           CreationParamKind     `json:"kind"`
	LabelKey       string                `json:"labelKey"`
	Options        []CreationParamOption `json:"options,omitempty"`
	Min            int                   `json:"min,omitempty"`
	Max            int                   `json:"max,omitempty"`
	Step           int                   `json:"step,omitempty"`
	UnitKey        string                `json:"unitKey,omitempty"`
	MaxCount       int                   `json:"maxCount,omitempty"`
	Accept         []string              `json:"accept,omitempty"`
	MaxSizeMB      int                   `json:"maxSizeMB,omitempty"`
	Required       bool                  `json:"required,omitempty"`
	Default        any                   `json:"default,omitempty"`
	HintKey        string                `json:"hintKey,omitempty"`
	Disabled       bool                  `json:"disabled,omitempty"`
	DisabledReason string                `json:"disabledReason,omitempty"`
}

// CreationAvailability tells the panel whether a mode can be used.
type CreationAvailability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// CreationKeyInfo is the key identity echoed back to the panel.
type CreationKeyInfo struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	GroupID   *int64 `json:"group_id"`
	GroupName string `json:"group_name"`
	Platform  string `json:"platform"`
}

// CreationUploadLimits advertises the upload constraints.
type CreationUploadLimits struct {
	MaxBytes  int64    `json:"max_bytes"`
	Accept    []string `json:"accept"`
	MaxSidePx int      `json:"max_side_px"`
	MaxPixels int      `json:"max_pixels"`
}

// CreationPricingHint is a read-only price summary for the model picker.
type CreationPricingHint struct {
	Priced    bool     `json:"priced"`
	Mode      string   `json:"mode,omitempty"`
	UnitPrice *float64 `json:"unit_price"`
}

// CreationModelOption is one (mode, protocol, model) row.
type CreationModelOption struct {
	ID          string                    `json:"id"`
	Mode        string                    `json:"mode"`
	Protocol    string                    `json:"protocol"`
	Model       string                    `json:"model"`
	DisplayName string                    `json:"display_name"`
	Params      []CreationParamDescriptor `json:"params"`
	Pricing     CreationPricingHint       `json:"pricing"`
	Partial     bool                      `json:"partial"`
}

// CreationOptionsResponse is the options payload (contract §3.1).
type CreationOptionsResponse struct {
	APIKey       CreationKeyInfo       `json:"api_key"`
	Image        CreationAvailability  `json:"image"`
	Video        CreationAvailability  `json:"video"`
	Uploads      CreationAvailability  `json:"uploads"`
	UploadLimits CreationUploadLimits  `json:"upload_limits"`
	Models       []CreationModelOption `json:"models"`
}

// creationOptionsKeyUnavailable decides whether the key is unusable before group
// or storage are considered.
//
// An exhausted quota is deliberately NOT part of this: quota only blocks
// submission (the inner apiKeyAuth answers 429 API_KEY_QUOTA_EXHAUSTED), while
// the capability list must keep showing what the key could run. Disabled and
// expired keys stay unavailable, as before.
func creationOptionsKeyUnavailable(key *APIKey) bool {
	if key == nil {
		return true
	}
	return !key.IsActive() || key.IsExpired()
}

// CreationOptions builds the mode/model catalog of one API key. It only reads
// state: it never freezes money, writes storage or calls upstream.
func (s *OpenAIGatewayService) CreationOptions(ctx context.Context, key *APIKey, imageTasks *ImageTaskService) (*CreationOptionsResponse, error) {
	if key == nil {
		return nil, ErrCreationKeyNotFound
	}
	response := &CreationOptionsResponse{
		APIKey: CreationKeyInfo{ID: key.ID, Name: key.Name},
		UploadLimits: CreationUploadLimits{
			MaxBytes:  CreationUploadMaxBytes,
			Accept:    append([]string(nil), CreationUploadAccept...),
			MaxSidePx: CreationUploadMaxSidePx,
			MaxPixels: CreationUploadMaxPixels,
		},
		Models: []CreationModelOption{},
	}
	group := key.Group
	if group != nil {
		groupID := group.ID
		response.APIKey.GroupID = &groupID
		response.APIKey.GroupName = group.Name
		response.APIKey.Platform = group.Platform
	}
	storageEnabled := imageTasks != nil && imageTasks.Enabled()
	response.Uploads = CreationAvailability{Available: storageEnabled}
	if !storageEnabled {
		response.Uploads.Reason = CreationUnavailableStorageDisabled
	}
	if creationOptionsKeyUnavailable(key) {
		unavailable := CreationAvailability{Reason: CreationUnavailableKeyUnavailable}
		response.Image, response.Video, response.Uploads = unavailable, unavailable, unavailable
		return response, nil
	}
	if group == nil || group.ID <= 0 || key.GroupID == nil {
		unavailable := CreationAvailability{Reason: CreationUnavailableNoGroup}
		response.Image, response.Video = unavailable, unavailable
		return response, nil
	}

	accounts, err := s.creationStudioAccounts(ctx, group)
	if err != nil {
		return nil, ErrCreationEstimateUnavailable.WithCause(err)
	}
	candidates := s.creationStudioCandidates(accounts)
	imageAvailable, imageReason := creationStudioImageAvailability(group, storageEnabled, candidates)
	videoAvailable, videoReason := creationStudioVideoAvailability(group, candidates)
	response.Image = CreationAvailability{Available: imageAvailable, Reason: imageReason}
	response.Video = CreationAvailability{Available: videoAvailable, Reason: videoReason}

	rows := map[string]*creationModelRow{}
	for _, candidate := range candidates {
		// Rows are listed even when their mode is unavailable: the panel keeps the
		// picker populated and marks the reason, and image descriptors carry
		// disabled=true when uploads are off (contract §3.4).
		row := rows[candidate.id()]
		if row == nil {
			row = &creationModelRow{candidate: candidate}
			rows[candidate.id()] = row
		}
		row.params = append(row.params, candidate.descriptors(storageEnabled))
	}
	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		row := rows[id]
		params, partial := mergeCreationParams(row.params)
		option := CreationModelOption{
			ID: id, Mode: row.candidate.mode, Protocol: row.candidate.protocol, Model: row.candidate.model,
			DisplayName: row.candidate.model, Params: params, Partial: partial,
			Pricing: s.creationStudioPricingHint(ctx, key, row.candidate),
		}
		response.Models = append(response.Models, option)
	}
	return response, nil
}

// creationStudioAccounts lists the schedulable accounts of the group.
func (s *OpenAIGatewayService) creationStudioAccounts(ctx context.Context, group *Group) ([]Account, error) {
	if s == nil || s.accountRepo == nil || group == nil {
		return nil, nil
	}
	return s.accountRepo.ListSchedulableByGroupID(ctx, group.ID)
}

func creationStudioImageAvailability(group *Group, storageEnabled bool, candidates []creationCandidate) (bool, string) {
	if group.Platform != PlatformOpenAI && group.Platform != PlatformGrok {
		return false, CreationUnavailablePlatformUnsupported
	}
	if !storageEnabled {
		return false, CreationUnavailableStorageDisabled
	}
	if !GroupAllowsImageGeneration(group) {
		return false, CreationUnavailablePermissionDenied
	}
	for _, candidate := range candidates {
		if candidate.mode == CreationModeImage {
			return true, ""
		}
	}
	return false, CreationUnavailableNoAccounts
}

func creationStudioVideoAvailability(group *Group, candidates []creationCandidate) (bool, string) {
	switch group.Platform {
	case PlatformVideo, PlatformGrok:
	case PlatformOpenAI:
		// An OpenAI group only serves video through accounts that enabled the
		// Seedance endpoint capability.
		hasSeedance := false
		for _, candidate := range candidates {
			if candidate.mode == CreationModeVideo && candidate.protocol == CreationProtocolSeedance {
				hasSeedance = true
				break
			}
		}
		if !hasSeedance {
			return false, CreationUnavailablePlatformUnsupported
		}
	default:
		return false, CreationUnavailablePlatformUnsupported
	}
	for _, candidate := range candidates {
		if candidate.mode == CreationModeVideo {
			return true, ""
		}
	}
	return false, CreationUnavailableNoAccounts
}

// creationModelRow accumulates every account that carries one model row.
type creationModelRow struct {
	candidate creationCandidate
	params    [][]CreationParamDescriptor
}

// creationCandidate is one (mode, protocol, model) offered by one account.
type creationCandidate struct {
	mode     string
	protocol string
	model    string
	mapped   string
	limits   videoModelLimits
}

func (c creationCandidate) id() string {
	return c.mode + ":" + c.protocol + ":" + c.model
}

// creationStudioCandidates expands the schedulable accounts into model rows.
// Video models come from the same public aliases the account test picker uses,
// so a protocol never advertises a model the submission would reject.
func (s *OpenAIGatewayService) creationStudioCandidates(accounts []Account) []creationCandidate {
	candidates := make([]creationCandidate, 0, len(accounts))
	add := func(candidate creationCandidate) {
		if strings.TrimSpace(candidate.model) == "" {
			return
		}
		// One candidate per (account, model): the same public model carried by two
		// accounts must reach the merge step so capacity differences surface as
		// partial=true. Duplicates inside one account cannot occur because the
		// model lists are de-duplicated by their own builders.
		candidates = append(candidates, candidate)
	}
	for i := range accounts {
		account := &accounts[i]
		if !account.IsSchedulable() {
			continue
		}
		switch account.Platform {
		case PlatformOpenAI, PlatformGrok:
			for _, model := range creationStudioImageModels(account) {
				protocol := CreationProtocolOpenAI
				if IsGrokImageGenerationModel(model) {
					protocol = CreationProtocolGrok
				}
				if protocol == CreationProtocolOpenAI && account.Platform != PlatformOpenAI {
					continue
				}
				if protocol == CreationProtocolGrok && account.Platform != PlatformGrok {
					continue
				}
				add(creationCandidate{mode: CreationModeImage, protocol: protocol, model: model, mapped: account.GetMappedModel(model)})
			}
		}
		if account.Platform == PlatformVideo {
			protocol := account.VideoProtocol()
			for _, model := range account.VideoTestModelIDs() {
				limits, err := creationStudioVideoLimits(account, protocol, model)
				if err != nil {
					continue
				}
				add(creationCandidate{mode: CreationModeVideo, protocol: protocol, model: model, mapped: account.GetMappedModel(model), limits: limits})
			}
		}
		if account.Platform == PlatformGrok {
			for _, model := range creationStudioGrokVideoModels(account) {
				add(creationCandidate{mode: CreationModeVideo, protocol: CreationProtocolGrok, model: model, mapped: account.GetMappedModel(model)})
			}
		}
		if account.Platform == PlatformOpenAI && account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilitySeedance) {
			for _, model := range creationStudioSeedanceModels(account) {
				limits, err := creationStudioVideoLimits(account, CreationProtocolSeedance, model)
				if err != nil {
					continue
				}
				add(creationCandidate{mode: CreationModeVideo, protocol: CreationProtocolSeedance, model: model, mapped: account.GetMappedModel(model), limits: limits})
			}
		}
	}
	return candidates
}

// creationStudioVideoLimits resolves the effective capability table of one
// (account, protocol, model) and never invents a second table.
func creationStudioVideoLimits(account *Account, protocol, model string) (videoModelLimits, error) {
	mapped := account.GetMappedModel(model)
	switch protocol {
	case CreationProtocolSeedance:
		return resolveVideoAccountLimits(account, seedanceModelLimits(mapped))
	case CreationProtocolGemini:
		return resolveVideoAccountLimits(account, geminiVideoModelLimits(mapped))
	case CreationProtocolMiniMax:
		return miniMaxVideoModelLimits(account, mapped)
	default:
		return resolveVideoAccountLimits(account, videoModelLimits{Resolutions: append([]string(nil), grokVideoResolutionOrder...), MinDuration: 1, MaxDuration: 15})
	}
}

// grokVideoResolutionOrder is the fixed Grok video ladder (contract §3.3).
var grokVideoResolutionOrder = []string{"480p", "720p", "768p", "1080p", "2k"}

// creationStudioImageModels lists the image models an account can serve.
func creationStudioImageModels(account *Account) []string {
	mapping := account.GetModelMapping()
	if len(mapping) == 0 {
		if account.Platform == PlatformGrok {
			return []string{"grok-imagine-image"}
		}
		return []string{"gpt-image-2"}
	}
	models := make([]string, 0, len(mapping))
	for model := range mapping {
		model = strings.TrimSpace(model)
		if model == "" || strings.ContainsAny(model, "*?") {
			continue
		}
		if !IsGPTImageGenerationModel(model) && !IsGrokImageGenerationModel(model) {
			continue
		}
		models = append(models, model)
	}
	sort.Strings(models)
	return models
}

// creationStudioGrokVideoModels lists the Grok video models of an account.
func creationStudioGrokVideoModels(account *Account) []string {
	mapping := account.GetModelMapping()
	if len(mapping) == 0 {
		return []string{"grok-imagine-video"}
	}
	models := make([]string, 0, len(mapping))
	for model := range mapping {
		model = strings.TrimSpace(model)
		if model == "" || strings.ContainsAny(model, "*?") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(model), "grok-imagine-video") {
			models = append(models, model)
		}
	}
	sort.Strings(models)
	return models
}

// creationStudioSeedanceModels lists the Seedance models of an OpenAI account
// that opted into the Seedance endpoint capability.
//
// A mixed OpenAI account maps both text and video models, so every mapping key
// must be filtered by protocol: VideoTestModelMatchesProtocol rejects the other
// video families (veo/minimax/hailuo/t2v/i2v) but deliberately accepts unknown
// names as custom video relays, which would still list plain text models. A row
// is therefore offered only when either the public alias or its mapped upstream
// target is positively a Seedance model (contains "seedance" or is an "ep-"
// endpoint id). No mapping means "everything is allowed", so the default name is
// used; a mapping that filters down to nothing contributes no row.
func creationStudioSeedanceModels(account *Account) []string {
	mapping := account.GetModelMapping()
	if len(mapping) == 0 {
		return []string{"doubao-seedance-1-0-pro"}
	}
	models := make([]string, 0, len(mapping))
	for model := range mapping {
		model = strings.TrimSpace(model)
		if model == "" || strings.ContainsAny(model, "*?") {
			continue
		}
		mapped := account.GetMappedModel(model)
		if !VideoTestModelMatchesProtocol(mapped, CreationProtocolSeedance) {
			continue
		}
		if !creationStudioIsSeedanceModel(mapped) && !creationStudioIsSeedanceModel(model) {
			continue
		}
		models = append(models, model)
	}
	sort.Strings(models)
	return models
}

// creationStudioIsSeedanceModel reports whether a name positively identifies a
// Seedance model family.
func creationStudioIsSeedanceModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(model, "seedance") || strings.HasPrefix(model, "ep-")
}

// ImageParamKeys are the four image roles a descriptor may use.
var CreationImageParamKeys = []string{"source", "first_frame", "last_frame", "reference"}

// descriptors renders the parameter list of one candidate.
func (c creationCandidate) descriptors(storageEnabled bool) []CreationParamDescriptor {
	switch {
	case c.mode == CreationModeImage && c.protocol == CreationProtocolOpenAI:
		return []CreationParamDescriptor{
			creationSelectDescriptor("size", []string{"1024x1024", "1536x1024", "1024x1536", "auto"}, "1024x1024"),
			creationSelectDescriptor("quality", []string{"auto", "low", "medium", "high"}, "auto"),
			creationIntegerDescriptor("n", 1, 4, 1, 1),
			creationImageDescriptor(CreationImageRoleSource, 1, false, storageEnabled),
		}
	case c.mode == CreationModeImage && c.protocol == CreationProtocolGrok:
		return []CreationParamDescriptor{
			creationRatioDescriptor("aspect_ratio", []string{"1:1", "16:9", "9:16", "4:3", "3:4"}, "1:1"),
			creationSelectDescriptor("resolution", []string{"1k", "2k"}, "1k"),
			creationIntegerDescriptor("n", 1, 4, 1, 1),
			creationImageDescriptor(CreationImageRoleSource, 1, false, storageEnabled),
		}
	case c.protocol == CreationProtocolSeedance:
		return []CreationParamDescriptor{
			creationResolutionDescriptor(c.limits, VideoBillingResolution720P),
			creationIntegerDescriptor("duration", c.limits.MinDuration, c.limits.MaxDuration, 1, clampCreationDuration(5, c.limits)),
			creationRatioDescriptor("ratio", []string{"16:9", "9:16", "4:3", "3:4", "1:1", "21:9", "adaptive"}, "16:9"),
			creationBoolDescriptor("watermark", "false"),
			creationImageDescriptor("first_frame", 1, false, storageEnabled),
			creationImageDescriptor("last_frame", 1, false, storageEnabled),
			creationImageDescriptor("reference", 4, false, storageEnabled),
		}
	case c.protocol == CreationProtocolGemini:
		params := []CreationParamDescriptor{
			creationResolutionDescriptor(c.limits, VideoBillingResolution720P),
			creationIntegerDescriptor("duration", c.limits.MinDuration, c.limits.MaxDuration, creationDurationStep(c.limits, geminiVideoModelFamily(c.mapped) == "3.1"), clampCreationDuration(8, c.limits)),
			creationRatioDescriptor("aspect_ratio", []string{"16:9", "9:16"}, "16:9"),
			creationImageDescriptor("first_frame", 1, false, storageEnabled),
		}
		if geminiVideoModelFamily(c.mapped) == "3.1" {
			params = append(params, creationImageDescriptor("reference", 3, false, storageEnabled))
		}
		return params
	case c.protocol == CreationProtocolMiniMax:
		v2 := creationStudioIsMiniMaxV2(c.model)
		step := 1
		if !v2 && len(c.limits.Durations) > 0 && c.limits.MaxDuration > c.limits.MinDuration {
			step = c.limits.Durations[1] - c.limits.Durations[0]
		}
		params := []CreationParamDescriptor{
			creationResolutionDescriptor(c.limits, "768p"),
			creationIntegerDescriptor("duration", c.limits.MinDuration, c.limits.MaxDuration, step, clampCreationDuration(6, c.limits)),
			creationImageDescriptor("first_frame", 1, false, storageEnabled),
			creationImageDescriptor("last_frame", 1, false, storageEnabled),
		}
		if v2 {
			params = append(params,
				creationRatioDescriptor("ratio", []string{"16:9", "9:16", "4:3", "3:4", "1:1", "21:9", "adaptive"}, "16:9"),
				creationImageDescriptor("reference", 1, false, storageEnabled),
			)
		}
		return params
	case c.protocol == CreationProtocolGrok:
		return []CreationParamDescriptor{
			creationIntegerDescriptor("duration", 1, 15, 1, 8),
			creationSelectDescriptor("resolution", grokVideoResolutionOrder, VideoBillingResolution480P),
			creationImageDescriptor("first_frame", 1, false, storageEnabled),
		}
	default:
		return nil
	}
}

func creationSelectDescriptor(key string, values []string, defaultValue string) CreationParamDescriptor {
	options := make([]CreationParamOption, 0, len(values))
	for _, value := range values {
		options = append(options, CreationParamOption{Value: value})
	}
	return CreationParamDescriptor{Key: key, Kind: CreationParamKindSelect, LabelKey: "creationStudio.params." + key, Options: options, Default: defaultValue}
}

func creationBoolDescriptor(key, defaultValue string) CreationParamDescriptor {
	return creationSelectDescriptor(key, []string{"false", "true"}, defaultValue)
}

func creationRatioDescriptor(key string, values []string, defaultValue string) CreationParamDescriptor {
	options := make([]CreationParamOption, 0, len(values))
	for _, value := range values {
		width, height := creationRatioDimensions(value)
		options = append(options, CreationParamOption{Value: value, W: width, H: height})
	}
	return CreationParamDescriptor{Key: key, Kind: CreationParamKindRatio, LabelKey: "creationStudio.params." + key, Options: options, Default: defaultValue}
}

// creationRatioDimensions maps the advertised ratio ladder to display sizes.
func creationRatioDimensions(value string) (int, int) {
	switch value {
	case "16:9":
		return 16, 9
	case "9:16":
		return 9, 16
	case "4:3":
		return 4, 3
	case "3:4":
		return 3, 4
	case "21:9":
		return 21, 9
	case "1:1":
		return 1, 1
	default: // adaptive and anything unknown fall back to 1:1
		return 1, 1
	}
}

func creationIntegerDescriptor(key string, minValue, maxValue, step, defaultValue int) CreationParamDescriptor {
	if step <= 0 {
		step = 1
	}
	return CreationParamDescriptor{
		Key: key, Kind: CreationParamKindInteger, LabelKey: "creationStudio.params." + key,
		Min: minValue, Max: maxValue, Step: step, Default: defaultValue,
	}
}

func creationResolutionDescriptor(limits videoModelLimits, preferred string) CreationParamDescriptor {
	values := append([]string(nil), limits.Resolutions...)
	defaultValue := preferred
	if !containsString(values, defaultValue) {
		if len(values) == 0 {
			defaultValue = preferred
		} else {
			defaultValue = values[0]
		}
	}
	descriptor := creationSelectDescriptor("resolution", values, defaultValue)
	if strings.HasSuffix(descriptor.Key, "duration") {
		descriptor.UnitKey = "creationStudio.units.seconds"
	}
	return descriptor
}

func creationImageDescriptor(key string, maxCount int, required, storageEnabled bool) CreationParamDescriptor {
	descriptor := CreationParamDescriptor{
		Key: key, Kind: CreationParamKindImage, LabelKey: "creationStudio.params." + key,
		MaxCount: maxCount, Accept: append([]string(nil), CreationUploadAccept...), MaxSizeMB: int(CreationUploadMaxBytes >> 20),
		Required: required,
	}
	if !storageEnabled {
		descriptor.Disabled = true
		descriptor.DisabledReason = CreationUnavailableStorageDisabled
	}
	return descriptor
}

// creationDurationStep reports the admissible duration granularity.
func creationDurationStep(limits videoModelLimits, evenStep bool) int {
	if limits.DurationOverride {
		return 1
	}
	if evenStep {
		return 2
	}
	return 1
}

// clampCreationDuration converges a preferred default into the allowed range.
func clampCreationDuration(preferred int, limits videoModelLimits) int {
	if preferred < limits.MinDuration {
		preferred = limits.MinDuration
	}
	if preferred > limits.MaxDuration {
		preferred = limits.MaxDuration
	}
	return preferred
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

// mergeCreationParams merges the descriptors of every account carrying one model
// row: resolutions are unioned, duration bounds widened, image counts maximized.
// partial reports that the merged capability is wider than at least one account.
func mergeCreationParams(perAccount [][]CreationParamDescriptor) ([]CreationParamDescriptor, bool) {
	if len(perAccount) == 0 {
		return nil, false
	}
	if len(perAccount) == 1 {
		return perAccount[0], false
	}
	merged := make([]CreationParamDescriptor, len(perAccount[0]))
	copy(merged, perAccount[0])
	partial := false
	for _, descriptors := range perAccount[1:] {
		for i := range merged {
			if i >= len(descriptors) {
				continue
			}
			if merged[i].Key != descriptors[i].Key {
				continue
			}
			next, changed := mergeCreationParam(merged[i], descriptors[i])
			merged[i] = next
			partial = partial || changed
		}
	}
	for _, descriptor := range merged {
		for _, descriptors := range perAccount {
			for _, candidate := range descriptors {
				if candidate.Key != descriptor.Key {
					continue
				}
				if !creationParamEqual(candidate, descriptor) {
					partial = true
				}
			}
		}
	}
	return merged, partial
}

func mergeCreationParam(left, right CreationParamDescriptor) (CreationParamDescriptor, bool) {
	switch left.Kind {
	case CreationParamKindSelect, CreationParamKindRatio:
		values := make([]string, 0, len(left.Options)+len(right.Options))
		for _, option := range left.Options {
			values = append(values, option.Value)
		}
		for _, option := range right.Options {
			if !containsString(values, option.Value) {
				values = append(values, option.Value)
			}
		}
		values = canonicalResolutionOrder(values)
		merged := left
		merged.Options = make([]CreationParamOption, 0, len(values))
		for _, value := range values {
			option := creationParamOptionFor(value, left.Options, right.Options)
			merged.Options = append(merged.Options, option)
		}
		if defaultText, ok := merged.Default.(string); !ok || !containsString(values, defaultText) {
			if len(values) > 0 {
				merged.Default = values[0]
			}
		}
		return merged, len(values) != len(left.Options) || len(values) != len(right.Options)
	case CreationParamKindInteger:
		merged := left
		if right.Min < merged.Min {
			merged.Min = right.Min
		}
		if right.Max > merged.Max {
			merged.Max = right.Max
		}
		if right.Step > 0 && (merged.Step <= 0 || right.Step < merged.Step) {
			merged.Step = right.Step
		}
		if value, ok := creationParamInt(merged.Default); ok {
			merged.Default = clampInt(value, merged.Min, merged.Max)
		}
		return merged, merged.Min != left.Min || merged.Max != left.Max || merged.Step != left.Step
	case CreationParamKindImage:
		merged := left
		if right.MaxCount > merged.MaxCount {
			merged.MaxCount = right.MaxCount
		}
		merged.Disabled = left.Disabled && right.Disabled
		if !merged.Disabled {
			merged.DisabledReason = ""
		}
		return merged, merged.MaxCount != left.MaxCount || merged.Disabled != left.Disabled
	default:
		return left, false
	}
}

func creationParamOptionFor(value string, sets ...[]CreationParamOption) CreationParamOption {
	for _, options := range sets {
		for _, option := range options {
			if option.Value == value {
				return option
			}
		}
	}
	return CreationParamOption{Value: value}
}

func creationParamEqual(left, right CreationParamDescriptor) bool {
	if left.Kind != right.Kind || left.Min != right.Min || left.Max != right.Max || left.Step != right.Step || left.MaxCount != right.MaxCount {
		return false
	}
	if len(left.Options) != len(right.Options) {
		return false
	}
	for i := range left.Options {
		if left.Options[i].Value != right.Options[i].Value {
			return false
		}
	}
	return true
}

// canonicalResolutionOrder sorts resolution-like values into the billing order;
// values outside the ladder keep their input order at the end.
func canonicalResolutionOrder(values []string) []string {
	ordered := make([]string, 0, len(values))
	for _, candidate := range videoCapabilityResolutionOrder {
		if containsString(values, candidate) {
			ordered = append(ordered, candidate)
		}
	}
	for _, value := range values {
		if !containsString(ordered, value) {
			ordered = append(ordered, value)
		}
	}
	return ordered
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

// creationStudioPricingHint renders the read-only price summary of one row.
func (s *OpenAIGatewayService) creationStudioPricingHint(ctx context.Context, key *APIKey, candidate creationCandidate) CreationPricingHint {
	if candidate.mode == CreationModeImage {
		cost := s.calculateOpenAIImageCost(ctx, candidate.mapped, key, &OpenAIForwardResult{
			ImageSize: ImageBillingSize2K, ImageInputSize: ImageBillingSize2K, ImageCount: 1,
		}, 1)
		if cost == nil {
			return CreationPricingHint{Priced: false}
		}
		unit := cost.ActualCost
		return CreationPricingHint{Priced: true, Mode: "image", UnitPrice: &unit}
	}
	resolution := creationCandidateDefaultResolution(candidate, candidate.limits)
	price := resolveVideoTaskPrice(ctx, s.resolver, key, candidate.mapped, resolution)
	if price == nil {
		return CreationPricingHint{Priced: false}
	}
	unit := price.unitPrice
	return CreationPricingHint{Priced: true, Mode: string(price.mode), UnitPrice: &unit}
}

// creationCandidateDefaultResolution returns the descriptor default resolution.
func creationCandidateDefaultResolution(candidate creationCandidate, limits videoModelLimits) string {
	switch candidate.protocol {
	case CreationProtocolSeedance:
		if containsString(limits.Resolutions, VideoBillingResolution720P) {
			return VideoBillingResolution720P
		}
	case CreationProtocolGemini:
		if containsString(limits.Resolutions, VideoBillingResolution720P) {
			return VideoBillingResolution720P
		}
	case CreationProtocolMiniMax:
		// The native protocol spells resolutions in upper case.
		if containsString(limits.Resolutions, "768p") {
			return "768P"
		}
	case CreationProtocolGrok:
		return VideoBillingResolution480P
	}
	if len(limits.Resolutions) > 0 {
		return limits.Resolutions[0]
	}
	return VideoBillingResolution720P
}

// CreationKeyFromOptionsError keeps the 404 mapping in one place for the handler.
var ErrCreationOptionsUnavailable = infraerrors.New(http.StatusServiceUnavailable, "CREATION_ESTIMATE_UNAVAILABLE", "creation options are temporarily unavailable")
