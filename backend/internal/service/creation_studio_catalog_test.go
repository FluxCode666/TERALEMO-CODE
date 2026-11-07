//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type creationCatalogRepo struct {
	AccountRepository
	accounts []Account
	err      error
}

func (r *creationCatalogRepo) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]Account, error) {
	return r.accounts, r.err
}

func newCreationCatalogGateway(t *testing.T, accounts ...Account) *OpenAIGatewayService {
	t.Helper()
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	return &OpenAIGatewayService{
		accountRepo:    &creationCatalogRepo{accounts: accounts},
		cfg:            cfg,
		billingService: NewBillingService(cfg, nil),
	}
}

func creationCatalogAccount(platform, accountType string, mapping map[string]any, credentials map[string]any) Account {
	values := map[string]any{}
	for key, value := range credentials {
		values[key] = value
	}
	if len(mapping) > 0 {
		values["model_mapping"] = mapping
	}
	return Account{
		ID: 1, Platform: platform, Type: accountType, Status: StatusActive, Schedulable: true,
		Credentials: values,
	}
}

func creationCatalogKey(group *Group, status string) *APIKey {
	key := &APIKey{ID: 9, UserID: 7, Name: "key", Status: status, Group: group}
	if group != nil {
		groupID := group.ID
		key.GroupID = &groupID
	}
	return key
}

func creationCatalogStorage(t *testing.T) *ImageTaskService {
	t.Helper()
	store := newCreationUploadTestStore()
	storage := &creationUploadTestStorage{fakeObjectStorage: newFakeObjectStorage()}
	return NewImageTaskServiceWithUploader(store, NewImageResultUploader(storage, "images/", 1<<20, nil), 0, 0)
}

func disabledCreationStorage(t *testing.T) *ImageTaskService {
	t.Helper()
	return NewImageTaskService(newCreationUploadTestStore())
}

func requireOnlyAllowedKinds(t *testing.T, options *CreationOptionsResponse) {
	t.Helper()
	allowed := map[CreationParamKind]bool{
		CreationParamKindSelect: true, CreationParamKindInteger: true,
		CreationParamKindRatio: true, CreationParamKindImage: true,
	}
	for _, option := range options.Models {
		for _, descriptor := range option.Params {
			require.True(t, allowed[descriptor.Kind], "unexpected descriptor kind %q", descriptor.Kind)
		}
	}
}

func TestCreationOptionsOpenAIGroup(t *testing.T) {
	gateway := newCreationCatalogGateway(t, creationCatalogAccount(PlatformOpenAI, AccountTypeAPIKey,
		map[string]any{"gpt-image-2": "gpt-image-2"}, nil))
	group := &Group{ID: 3, Name: "images", Platform: PlatformOpenAI, AllowImageGeneration: true, RateMultiplier: 1, Status: StatusActive}
	options, err := gateway.CreationOptions(context.Background(), creationCatalogKey(group, StatusActive), creationCatalogStorage(t))
	require.NoError(t, err)
	require.True(t, options.Image.Available)
	require.Empty(t, options.Image.Reason)
	require.False(t, options.Video.Available)
	require.Equal(t, CreationUnavailablePlatformUnsupported, options.Video.Reason)
	require.Equal(t, int64(10485760), options.UploadLimits.MaxBytes)
	require.Equal(t, []string{"image/png", "image/jpeg", "image/webp"}, options.UploadLimits.Accept)
	require.Equal(t, 4096, options.UploadLimits.MaxSidePx)
	require.Equal(t, 16777216, options.UploadLimits.MaxPixels)
	require.Len(t, options.Models, 1)

	row := options.Models[0]
	require.Equal(t, "image:openai:gpt-image-2", row.ID)
	require.Equal(t, CreationModeImage, row.Mode)
	require.Equal(t, CreationProtocolOpenAI, row.Protocol)
	require.Equal(t, "gpt-image-2", row.Model)
	require.False(t, row.Partial)
	keys := make([]string, 0, len(row.Params))
	for _, descriptor := range row.Params {
		keys = append(keys, descriptor.Key)
		require.Equal(t, "creationStudio.params."+descriptor.Key, descriptor.LabelKey)
	}
	require.Equal(t, []string{"size", "quality", "n", "source"}, keys)
	require.True(t, row.Pricing.Priced)
	require.Equal(t, "image", row.Pricing.Mode)
	require.NotNil(t, row.Pricing.UnitPrice)
	requireOnlyAllowedKinds(t, options)
}

func TestCreationOptionsStorageDisabledKeepsImageRows(t *testing.T) {
	gateway := newCreationCatalogGateway(t,
		creationCatalogAccount(PlatformOpenAI, AccountTypeAPIKey, map[string]any{"gpt-image-2": "gpt-image-2"}, nil),
	)
	group := &Group{ID: 3, Name: "images", Platform: PlatformOpenAI, AllowImageGeneration: true, Status: StatusActive}
	options, err := gateway.CreationOptions(context.Background(), creationCatalogKey(group, StatusActive), disabledCreationStorage(t))
	require.NoError(t, err)
	require.False(t, options.Image.Available)
	require.Equal(t, CreationUnavailableStorageDisabled, options.Image.Reason)
	require.False(t, options.Uploads.Available)
	require.Equal(t, CreationUnavailableStorageDisabled, options.Uploads.Reason)
	require.Len(t, options.Models, 1)
	for _, descriptor := range options.Models[0].Params {
		if descriptor.Kind != CreationParamKindImage {
			continue
		}
		require.True(t, descriptor.Disabled)
		require.Equal(t, CreationUnavailableStorageDisabled, descriptor.DisabledReason)
	}
}

func TestCreationOptionsVideoGroupListsThreeProtocols(t *testing.T) {
	seedance := creationCatalogAccount(PlatformVideo, AccountTypeAPIKey, map[string]any{"doubao-seedance-1-0-pro": "doubao-seedance-1-0-pro"},
		map[string]any{"video_protocol": VideoProtocolSeedance})
	gemini := creationCatalogAccount(PlatformVideo, AccountTypeAPIKey, map[string]any{"veo-3.1-generate-001": "veo-3.1-generate-001"},
		map[string]any{"video_protocol": VideoProtocolGemini})
	minimax := creationCatalogAccount(PlatformVideo, AccountTypeAPIKey, map[string]any{"MiniMax-Hailuo-2.3": "MiniMax-Hailuo-2.3"},
		map[string]any{"video_protocol": VideoProtocolMiniMax, "base_url": "https://api.minimax.io"})
	gateway := newCreationCatalogGateway(t, seedance, gemini, minimax)
	group := &Group{ID: 4, Name: "video", Platform: PlatformVideo, Status: StatusActive}

	options, err := gateway.CreationOptions(context.Background(), creationCatalogKey(group, StatusActive), disabledCreationStorage(t))
	require.NoError(t, err)
	require.False(t, options.Image.Available)
	require.Equal(t, CreationUnavailablePlatformUnsupported, options.Image.Reason)
	require.True(t, options.Video.Available)
	require.Len(t, options.Models, 3)

	protocols := map[string]bool{}
	resolutionOptions := map[string][]string{}
	for _, row := range options.Models {
		protocols[row.Protocol] = true
		for _, descriptor := range row.Params {
			require.NotEqual(t, "n", descriptor.Key, "video descriptors must not offer a count field")
			if descriptor.Key == "resolution" {
				for _, option := range descriptor.Options {
					resolutionOptions[row.Protocol] = append(resolutionOptions[row.Protocol], option.Value)
				}
			}
			if descriptor.Kind == CreationParamKindImage {
				require.True(t, descriptor.Disabled, "uploads are off, every image descriptor must be disabled")
			}
		}
	}
	require.True(t, protocols[CreationProtocolSeedance])
	require.True(t, protocols[CreationProtocolGemini])
	require.True(t, protocols[CreationProtocolMiniMax])
	require.Contains(t, resolutionOptions[CreationProtocolGemini], VideoBillingResolution1080P)
	requireOnlyAllowedKinds(t, options)

	// Gemini duration defaults to 8 seconds and keeps the Veo 3.1 step of 2.
	for _, row := range options.Models {
		if row.Protocol != CreationProtocolGemini {
			continue
		}
		for _, descriptor := range row.Params {
			if descriptor.Key == "duration" {
				require.Equal(t, 8, descriptor.Default)
				require.Equal(t, 2, descriptor.Step)
				require.Equal(t, 4, descriptor.Min)
			}
		}
	}
}

func TestCreationOptionsHonorsAccountResolutionOverride(t *testing.T) {
	seedance := creationCatalogAccount(PlatformVideo, AccountTypeAPIKey, map[string]any{"doubao-seedance-1-0-pro": "doubao-seedance-1-0-pro"},
		map[string]any{"video_protocol": VideoProtocolSeedance, "video_resolutions": []string{"480p", "1080p"}})
	gateway := newCreationCatalogGateway(t, seedance)
	group := &Group{ID: 4, Name: "video", Platform: PlatformVideo, Status: StatusActive}
	options, err := gateway.CreationOptions(context.Background(), creationCatalogKey(group, StatusActive), disabledCreationStorage(t))
	require.NoError(t, err)
	require.Len(t, options.Models, 1)
	for _, descriptor := range options.Models[0].Params {
		if descriptor.Key != "resolution" {
			continue
		}
		values := make([]string, 0, len(descriptor.Options))
		for _, option := range descriptor.Options {
			values = append(values, option.Value)
		}
		require.Equal(t, []string{"480p", "1080p"}, values)
		require.Equal(t, "480p", descriptor.Default, "720p is absent, so the lowest tier wins")
	}
}

func TestCreationOptionsMarksPartialCapabilities(t *testing.T) {
	first := creationCatalogAccount(PlatformVideo, AccountTypeAPIKey, map[string]any{"doubao-seedance-1-0-pro": "doubao-seedance-1-0-pro"},
		map[string]any{"video_protocol": VideoProtocolSeedance, "video_resolutions": []string{"480p"}})
	second := creationCatalogAccount(PlatformVideo, AccountTypeAPIKey, map[string]any{"doubao-seedance-1-0-pro": "doubao-seedance-1-0-pro"},
		map[string]any{"video_protocol": VideoProtocolSeedance, "video_resolutions": []string{"1080p"}})
	second.ID = 2
	gateway := newCreationCatalogGateway(t, first, second)
	group := &Group{ID: 4, Name: "video", Platform: PlatformVideo, Status: StatusActive}
	options, err := gateway.CreationOptions(context.Background(), creationCatalogKey(group, StatusActive), disabledCreationStorage(t))
	require.NoError(t, err)
	require.Len(t, options.Models, 1)
	require.True(t, options.Models[0].Partial)
	for _, descriptor := range options.Models[0].Params {
		if descriptor.Key != "resolution" {
			continue
		}
		require.Len(t, descriptor.Options, 2)
	}
}

func TestCreationOptionsUnavailableReasons(t *testing.T) {
	gateway := newCreationCatalogGateway(t)
	ctx := context.Background()

	t.Run("no group", func(t *testing.T) {
		options, err := gateway.CreationOptions(ctx, creationCatalogKey(nil, StatusActive), creationCatalogStorage(t))
		require.NoError(t, err)
		require.Equal(t, CreationUnavailableNoGroup, options.Image.Reason)
		require.Equal(t, CreationUnavailableNoGroup, options.Video.Reason)
		require.NotNil(t, options.Uploads)
		require.Empty(t, options.Models)
	})

	t.Run("composite platform", func(t *testing.T) {
		group := &Group{ID: 5, Name: "composite", Platform: PlatformComposite, Status: StatusActive}
		options, err := gateway.CreationOptions(ctx, creationCatalogKey(group, StatusActive), creationCatalogStorage(t))
		require.NoError(t, err)
		require.Equal(t, CreationUnavailablePlatformUnsupported, options.Image.Reason)
		require.Equal(t, CreationUnavailablePlatformUnsupported, options.Video.Reason)
	})

	t.Run("inactive key", func(t *testing.T) {
		group := &Group{ID: 3, Name: "images", Platform: PlatformOpenAI, AllowImageGeneration: true, Status: StatusActive}
		options, err := gateway.CreationOptions(ctx, creationCatalogKey(group, StatusDisabled), creationCatalogStorage(t))
		require.NoError(t, err)
		require.Equal(t, CreationUnavailableKeyUnavailable, options.Image.Reason)
		require.Equal(t, CreationUnavailableKeyUnavailable, options.Video.Reason)
		require.Equal(t, CreationUnavailableKeyUnavailable, options.Uploads.Reason)
	})

	t.Run("image permission denied", func(t *testing.T) {
		gateway := newCreationCatalogGateway(t, creationCatalogAccount(PlatformOpenAI, AccountTypeAPIKey,
			map[string]any{"gpt-image-2": "gpt-image-2"}, nil))
		group := &Group{ID: 3, Name: "images", Platform: PlatformOpenAI, AllowImageGeneration: false, Status: StatusActive}
		options, err := gateway.CreationOptions(ctx, creationCatalogKey(group, StatusActive), creationCatalogStorage(t))
		require.NoError(t, err)
		require.Equal(t, CreationUnavailablePermissionDenied, options.Image.Reason)
	})

	t.Run("video without accounts", func(t *testing.T) {
		group := &Group{ID: 4, Name: "video", Platform: PlatformVideo, Status: StatusActive}
		options, err := gateway.CreationOptions(ctx, creationCatalogKey(group, StatusActive), creationCatalogStorage(t))
		require.NoError(t, err)
		require.Equal(t, CreationUnavailableNoAccounts, options.Video.Reason)
		require.Empty(t, options.Models)
	})
}

func TestCreationOptionsGrokGroupServesImagesAndVideo(t *testing.T) {
	grok := creationCatalogAccount(PlatformGrok, AccountTypeOAuth,
		map[string]any{"grok-imagine-image": "grok-imagine-image", "grok-imagine-video": "grok-imagine-video"}, nil)
	gateway := newCreationCatalogGateway(t, grok)
	group := &Group{ID: 6, Name: "grok", Platform: PlatformGrok, AllowImageGeneration: true, Status: StatusActive}
	options, err := gateway.CreationOptions(context.Background(), creationCatalogKey(group, StatusActive), creationCatalogStorage(t))
	require.NoError(t, err)
	require.True(t, options.Image.Available)
	require.True(t, options.Video.Available)
	require.Len(t, options.Models, 2)
	var grokImage, grokVideo *CreationModelOption
	for i := range options.Models {
		switch options.Models[i].Mode {
		case CreationModeImage:
			grokImage = &options.Models[i]
		case CreationModeVideo:
			grokVideo = &options.Models[i]
		}
	}
	require.NotNil(t, grokImage)
	require.NotNil(t, grokVideo)
	require.Equal(t, CreationProtocolGrok, grokImage.Protocol)
	require.Equal(t, CreationProtocolGrok, grokVideo.Protocol)

	// The Grok image resolution descriptor maps to the native size field.
	sizeValues := []string{}
	for _, descriptor := range grokImage.Params {
		if descriptor.Key == "resolution" {
			for _, option := range descriptor.Options {
				sizeValues = append(sizeValues, option.Value)
			}
		}
	}
	require.Equal(t, []string{"1k", "2k"}, sizeValues)
}

func TestCreationStudioSeedanceModelsFiltersByProtocol(t *testing.T) {
	// A mixed OpenAI account maps text and video models side by side.
	mixed := creationCatalogAccount(PlatformOpenAI, AccountTypeAPIKey, map[string]any{
		"doubao-seedance-1-0-pro": "ep-20250101-abc",
		"seedance-lite":           "doubao-seedance-1-0-pro",
		"gpt-4o":                  "gpt-4o",
		"claude-sonnet-4":         "claude-sonnet-4",
		"veo-3.1-generate-001":    "veo-3.1-generate-001",
		"MiniMax-Hailuo-2.3":      "MiniMax-Hailuo-2.3",
	}, nil)
	require.Equal(t,
		[]string{"doubao-seedance-1-0-pro", "seedance-lite"},
		creationStudioSeedanceModels(&mixed))

	// Nothing Seedance-shaped: the account contributes no video row at all.
	textOnly := creationCatalogAccount(PlatformOpenAI, AccountTypeAPIKey, map[string]any{
		"gpt-4o":          "gpt-4o",
		"claude-sonnet-4": "claude-sonnet-4",
		"qwen-max":        "qwen-max",
	}, nil)
	require.Empty(t, creationStudioSeedanceModels(&textOnly))

	// An empty mapping means "every model is allowed", so the default stays.
	noMapping := creationCatalogAccount(PlatformOpenAI, AccountTypeAPIKey, nil, nil)
	require.Equal(t, []string{"doubao-seedance-1-0-pro"}, creationStudioSeedanceModels(&noMapping))
}

// TestCreationOptionsKeepsModelsForQuotaExhaustedKey: an exhausted quota only
// blocks submission, so the capability list must stay visible.
func TestCreationOptionsKeepsModelsForQuotaExhaustedKey(t *testing.T) {
	gateway := newCreationCatalogGateway(t, creationCatalogAccount(PlatformOpenAI, AccountTypeAPIKey,
		map[string]any{"gpt-image-2": "gpt-image-2"}, nil))
	group := &Group{ID: 3, Name: "images", Platform: PlatformOpenAI, AllowImageGeneration: true, RateMultiplier: 1, Status: StatusActive}
	key := creationCatalogKey(group, StatusActive)
	key.Quota, key.QuotaUsed = 0.1, 3.2
	require.True(t, key.IsQuotaExhausted())

	options, err := gateway.CreationOptions(context.Background(), key, creationCatalogStorage(t))
	require.NoError(t, err)
	require.True(t, options.Image.Available, "an exhausted quota must not hide the capability list")
	require.Empty(t, options.Image.Reason)
	require.Len(t, options.Models, 1)

	// A disabled key keeps the previous behaviour.
	disabled := creationCatalogKey(group, StatusDisabled)
	options, err = gateway.CreationOptions(context.Background(), disabled, creationCatalogStorage(t))
	require.NoError(t, err)
	require.False(t, options.Image.Available)
	require.Equal(t, CreationUnavailableKeyUnavailable, options.Image.Reason)
	require.Empty(t, options.Models)
}
