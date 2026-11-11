package service

import (
	"context"
	"net/url"
	"sort"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/imagecompat"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

const (
	ImageProtocolOpenAI = "openai"
	ImageProtocolGemini = "gemini"
	ImageProtocolGrok   = "grok"
)

func (a *Account) ImageProtocol() string {
	protocol := strings.ToLower(strings.TrimSpace(a.GetCredential("image_protocol")))
	if protocol == "" {
		return ImageProtocolOpenAI
	}
	return protocol
}

func (a *Account) ImageBaseURL() string {
	if base := strings.TrimSpace(a.GetCredential("base_url")); base != "" {
		return base
	}
	switch a.ImageProtocol() {
	case ImageProtocolGemini:
		return "https://generativelanguage.googleapis.com"
	case ImageProtocolGrok:
		return "https://api.x.ai"
	default:
		return "https://api.openai.com"
	}
}

// ValidateImageAccount validates the merged credentials before any write.
// Deployment-specific outbound host policy is also enforced before forwarding.
func ValidateImageAccount(a *Account) error {
	if a == nil || a.Platform != PlatformImage {
		return nil
	}
	invalid := func(message string) error { return infraerrors.BadRequest("INVALID_IMAGE_ACCOUNT", message) }
	if a.Type != AccountTypeAPIKey {
		return invalid("image accounts only support apikey credentials")
	}
	if raw, exists := a.Credentials["image_protocol"]; exists {
		if _, ok := raw.(string); !ok {
			return invalid("image_protocol must be openai, gemini or grok")
		}
	}
	switch a.ImageProtocol() {
	case ImageProtocolOpenAI, ImageProtocolGemini, ImageProtocolGrok:
	default:
		return invalid("image_protocol must be openai, gemini or grok")
	}
	if strings.TrimSpace(a.GetCredential("api_key")) == "" {
		return invalid("image account requires api_key")
	}
	if raw, exists := a.Credentials["base_url"]; exists {
		if _, ok := raw.(string); !ok {
			return invalid("base_url must be a string")
		}
	}
	if parsed, err := url.Parse(a.ImageBaseURL()); err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return invalid("invalid image upstream base_url")
	}
	if _, err := urlvalidator.ValidateURLFormat(a.ImageBaseURL(), true); err != nil {
		return invalid("invalid image upstream base_url")
	}
	return nil
}

func DefaultImageModelIDs(protocol string) []string {
	switch protocol {
	case ImageProtocolGemini:
		return []string{"gemini-2.5-flash-image", "gemini-3-pro-image-preview", "imagen-4.0-generate-001"}
	case ImageProtocolGrok:
		return []string{"grok-imagine-image"}
	default:
		return []string{"gpt-image-2", "gpt-image-1", "dall-e-3"}
	}
}

func (a *Account) ImageModelIDs() []string {
	mapping := a.GetModelMapping()
	if len(mapping) == 0 {
		return DefaultImageModelIDs(a.ImageProtocol())
	}
	models := make([]string, 0, len(mapping))
	for model := range mapping {
		if strings.TrimSpace(model) != "" && !strings.ContainsAny(model, "*?") {
			models = append(models, model)
		}
	}
	sort.Strings(models)
	return models
}

func isImagePlatformBilling(ctx context.Context, key *APIKey) bool {
	if platform, _ := ResolvedTargetPlatformFromContext(ctx); platform == PlatformImage {
		return true
	}
	return key != nil && key.Group != nil && key.Group.Platform == PlatformImage
}

// Admission happens before account selection. Price the highest actual tier
// among eligible accounts, including their model mappings and resolution caps.
func imagePlatformPricingTier(accounts []Account, model, size string) string {
	tier := ""
	for i := range accounts {
		account := &accounts[i]
		if account.Platform != PlatformImage || account.Type != AccountTypeAPIKey || !account.IsModelSupported(model) {
			continue
		}
		candidate, err := imageProtocolSizeTier(account.ImageProtocol(), account.GetMappedModel(model), size)
		if err == nil && imageTierRank(candidate) > imageTierRank(tier) {
			tier = candidate
		}
	}
	if tier == "" {
		return NormalizeImageBillingTierOrDefault(size)
	}
	return tier
}

func imageProtocolSizeTier(protocol, model, size string) (string, error) {
	if protocol == ImageProtocolOpenAI {
		return NormalizeImageBillingTierOrDefault(size), nil
	}
	plan, err := imagecompat.BuildPlan(imagecompat.Protocol(protocol), imagecompat.Request{
		Operation: imagecompat.OperationGenerations, Model: model, Prompt: "image", Size: size, N: 1,
	})
	if err != nil {
		return "", err
	}
	if plan.SizeTier != "" {
		return plan.SizeTier, nil
	}
	return NormalizeImageBillingTierOrDefault(size), nil
}

func imagePricingAccounts(ctx context.Context, key *APIKey, snapshot *SchedulerSnapshotService, repo AccountRepository) []Account {
	if key == nil {
		return nil
	}
	if snapshot != nil {
		accounts, _, err := snapshot.ListSchedulableAccounts(ctx, key.GroupID, PlatformImage, false)
		if err == nil {
			return accounts
		}
	}
	if repo == nil {
		return nil
	}
	var accounts []Account
	if key.GroupID != nil {
		accounts, _ = repo.ListSchedulableByGroupID(ctx, *key.GroupID)
	} else {
		accounts, _ = repo.ListSchedulableUngroupedByPlatform(ctx, PlatformImage)
	}
	return accounts
}
