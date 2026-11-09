//go:build unit

package service

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestValidateImageAccount(t *testing.T) {
	for _, protocol := range []string{"", "openai", "gemini", "grok", " GEMINI "} {
		a := &Account{Platform: PlatformImage, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret", "image_protocol": protocol}}
		require.NoError(t, ValidateImageAccount(a))
		require.NotEmpty(t, a.ImageBaseURL())
		require.NotEmpty(t, a.ImageModelIDs())
	}
	for _, tc := range []struct {
		name  string
		typ   string
		creds map[string]any
	}{
		{"oauth", AccountTypeOAuth, map[string]any{"api_key": "secret"}},
		{"key", AccountTypeAPIKey, map[string]any{}},
		{"protocol", AccountTypeAPIKey, map[string]any{"api_key": "secret", "image_protocol": "other"}},
		{"typed protocol", AccountTypeAPIKey, map[string]any{"api_key": "secret", "image_protocol": 123}},
		{"url", AccountTypeAPIKey, map[string]any{"api_key": "secret", "base_url": "file:///tmp/key"}},
		{"userinfo", AccountTypeAPIKey, map[string]any{"api_key": "secret", "base_url": "https://user:secret@example.com"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateImageAccount(&Account{Platform: PlatformImage, Type: tc.typ, Credentials: tc.creds})
			require.Error(t, err)
			require.Equal(t, 400, infraerrors.Code(err))
			require.Equal(t, "INVALID_IMAGE_ACCOUNT", infraerrors.Reason(err))
		})
	}
	require.NoError(t, ValidateImageAccount(&Account{Platform: PlatformOpenAI}))
}

func TestImagePlatformCreationCatalog(t *testing.T) {
	for _, protocol := range []string{ImageProtocolOpenAI, ImageProtocolGemini, ImageProtocolGrok} {
		t.Run(protocol, func(t *testing.T) {
			account := creationCatalogAccount(PlatformImage, AccountTypeAPIKey, nil, map[string]any{"image_protocol": protocol, "api_key": "secret"})
			gateway := newCreationCatalogGateway(t, account)
			group := &Group{ID: 3, Platform: PlatformImage, AllowImageGeneration: true, RateMultiplier: 1, Status: StatusActive}
			key := creationCatalogKey(group, StatusActive)
			options, err := gateway.CreationOptions(context.Background(), key, creationCatalogStorage(t))
			require.NoError(t, err)
			require.True(t, options.Image.Available)
			require.False(t, options.Video.Available)
			require.Len(t, options.Models, len(account.ImageModelIDs()))
			for _, model := range options.Models {
				require.Equal(t, CreationModeImage, model.Mode)
				require.Equal(t, CreationProtocolOpenAI, model.Protocol)
				input := CreationNativeRequestInput{Mode: model.Mode, Protocol: model.Protocol, Model: model.Model, Prompt: "draw"}
				native, err := BuildCreationNativeRequest(input)
				require.NoError(t, err)
				require.Equal(t, CreationPathImageGenerations, native.Path)
			}
		})
	}
}

func TestImagePlatformEstimatesActualTier(t *testing.T) {
	account := creationCatalogAccount(PlatformImage, AccountTypeAPIKey, map[string]any{"public-image": "gemini-2.5-flash-image"}, map[string]any{"image_protocol": "gemini", "api_key": "secret"})
	gateway := newCreationCatalogGateway(t, account)
	p1, p2, p4 := .1, .8, 1.5
	key := creationCatalogKey(&Group{ID: 3, Platform: PlatformImage, RateMultiplier: 1, AllowImageGeneration: true, ImagePrice1K: &p1, ImagePrice2K: &p2, ImagePrice4K: &p4}, StatusActive)
	key.User = &User{ID: 7}
	for _, size := range []string{"1536x1024", "4096x4096", "16:9"} {
		estimate := gateway.EstimateCreationImageCost(context.Background(), key, 7, "public-image", size, 3, 100)
		require.True(t, estimate.Priced)
		require.Equal(t, "1K", estimate.Tier)
		require.InDelta(t, .3, estimate.Amount, 1e-9)
		amount, priced := gateway.EstimateInflightReservation(context.Background(), key, InflightEstimateRequest{Model: "public-image", ImageSize: size, Units: 3, Kind: InflightEstimateImage})
		require.True(t, priced)
		require.InDelta(t, estimate.Amount, amount, 1e-9)
	}
	options, err := gateway.CreationOptions(context.Background(), key, creationCatalogStorage(t))
	require.NoError(t, err)
	require.Len(t, options.Models, 1)
	require.InDelta(t, .1, *options.Models[0].Pricing.UnitPrice, 1e-9)
	var sizes []string
	for _, param := range options.Models[0].Params {
		if param.Key == "size" {
			for _, option := range param.Options {
				sizes = append(sizes, option.Value)
			}
		}
	}
	require.Contains(t, sizes, "1536x1024")
	require.Contains(t, sizes, "4096x4096")
	imagen := creationCandidate{mode: CreationModeImage, imageProtocol: "gemini", mapped: "imagen-4.0-generate-001"}
	for _, param := range imagen.descriptors(true) {
		for _, option := range param.Options {
			require.NotEqual(t, "1536x1024", option.Value, "Imagen cannot produce the 3:2 ratio")
		}
	}
}

func TestImagePlatformCreateAndUpdateValidation(t *testing.T) {
	for _, admin := range []bool{false, true} {
		for _, credentials := range []map[string]any{{"image_protocol": "gemini"}, {"api_key": "secret", "image_protocol": "bad"}} {
			repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: {ID: 1, Platform: PlatformImage, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret"}}}}
			var createErr, updateErr error
			if admin {
				svc := &adminServiceImpl{accountRepo: repo}
				_, createErr = svc.CreateAccount(context.Background(), &CreateAccountInput{Name: "image", Platform: PlatformImage, Type: AccountTypeAPIKey, Credentials: credentials, SkipDefaultGroupBind: true})
				// An explicit blank key tests invalid merged credentials.
				_, updateErr = svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Credentials: map[string]any{"api_key": "", "image_protocol": "gemini"}})
			} else {
				svc := NewAccountService(repo, nil)
				_, createErr = svc.Create(context.Background(), CreateAccountRequest{Name: "image", Platform: PlatformImage, Type: AccountTypeAPIKey, Credentials: credentials})
				_, updateErr = svc.Update(context.Background(), 1, UpdateAccountRequest{Credentials: &credentials})
			}
			for _, err := range []error{createErr, updateErr} {
				require.Equal(t, 400, infraerrors.Code(err))
				require.Equal(t, "INVALID_IMAGE_ACCOUNT", infraerrors.Reason(err))
			}
		}
	}
}

func TestImagePlatformBatchExplicitlyRejected(t *testing.T) {
	svc, repo, queue, provider, _ := newTestBatchImagePublicService(true)
	gid := int64(3)
	svc.GroupRepo = &publicBatchImageGroupRepo{groups: map[int64]*Group{gid: {ID: gid, Platform: PlatformImage, AllowBatchImageGeneration: true}}}
	_, err := svc.Submit(context.Background(), BatchImageOwner{UserID: 11, APIKeyID: 22, GroupID: &gid}, validBatchImageSubmitRequest(), "")
	require.ErrorIs(t, err, ErrBatchImageGroupDisabled)
	require.Empty(t, repo.jobs)
	require.Empty(t, queue.enqueued)
	require.Empty(t, provider.submits)
}
