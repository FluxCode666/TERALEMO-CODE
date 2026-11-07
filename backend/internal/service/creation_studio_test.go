//go:build unit

package service

import (
	"strings"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func creationImageOptionFixture() CreationModelOption {
	candidate := creationCandidate{mode: CreationModeImage, protocol: CreationProtocolOpenAI, model: "gpt-image-2"}
	return CreationModelOption{
		ID: "image:openai:gpt-image-2", Mode: CreationModeImage, Protocol: CreationProtocolOpenAI,
		Model: "gpt-image-2", DisplayName: "gpt-image-2", Params: candidate.descriptors(true),
	}
}

func creationSeedanceOptionFixture(t *testing.T) CreationModelOption {
	t.Helper()
	limits, err := resolveVideoAccountLimits(nil, seedanceModelLimits("doubao-seedance-1-0-pro"))
	require.NoError(t, err)
	candidate := creationCandidate{
		mode: CreationModeVideo, protocol: CreationProtocolSeedance, model: "doubao-seedance-1-0-pro",
		mapped: "doubao-seedance-1-0-pro", limits: limits,
	}
	return CreationModelOption{
		ID: "video:seedance:doubao-seedance-1-0-pro", Mode: CreationModeVideo, Protocol: CreationProtocolSeedance,
		Model: "doubao-seedance-1-0-pro", Params: candidate.descriptors(true),
	}
}

func creationMiniMaxV1OptionFixture() CreationModelOption {
	limits := videoModelLimits{Resolutions: videoCapabilityResolutionOrder, MinDuration: 6, MaxDuration: 10, Durations: []int{6, 10}}
	candidate := creationCandidate{
		mode: CreationModeVideo, protocol: CreationProtocolMiniMax, model: "MiniMax-Hailuo-2.3",
		mapped: "MiniMax-Hailuo-2.3", limits: limits,
	}
	return CreationModelOption{
		ID: "video:minimax:MiniMax-Hailuo-2.3", Mode: CreationModeVideo, Protocol: CreationProtocolMiniMax,
		Model: "MiniMax-Hailuo-2.3", Params: candidate.descriptors(true),
	}
}

func creationGeminiOptionFixture() CreationModelOption {
	limits := geminiVideoModelLimits("veo-3.1-generate-001")
	candidate := creationCandidate{
		mode: CreationModeVideo, protocol: CreationProtocolGemini, model: "veo-3.1-generate-001",
		mapped: "veo-3.1-generate-001", limits: limits,
	}
	return CreationModelOption{
		ID: "video:gemini:veo-3.1-generate-001", Mode: CreationModeVideo, Protocol: CreationProtocolGemini,
		Model: "veo-3.1-generate-001", Params: candidate.descriptors(true),
	}
}

func submitRequest(option CreationModelOption, mutate func(*CreationSubmitRequest)) *CreationSubmitRequest {
	request := &CreationSubmitRequest{
		APIKeyID: 1, Mode: option.Mode, Model: option.Model, Protocol: option.Protocol,
		Prompt: "a cat", Params: map[string]any{},
	}
	if mutate != nil {
		mutate(request)
	}
	return request
}

func TestValidateCreationSubmitV3Rules(t *testing.T) {
	option := creationImageOptionFixture()
	cases := []struct {
		name    string
		mutate  func(*CreationSubmitRequest)
		wantErr bool
	}{
		{name: "valid", mutate: func(r *CreationSubmitRequest) { r.Prompt = "a cat" }},
		{name: "blank prompt", mutate: func(r *CreationSubmitRequest) { r.Prompt = "   " }, wantErr: true},
		{name: "8000 runes accepted", mutate: func(r *CreationSubmitRequest) { r.Prompt = strings.Repeat("猫", CreationMaxPromptRunes) }},
		{name: "8001 runes rejected", mutate: func(r *CreationSubmitRequest) { r.Prompt = strings.Repeat("猫", CreationMaxPromptRunes+1) }, wantErr: true},
		{name: "invalid mode", mutate: func(r *CreationSubmitRequest) { r.Mode = "audio" }, wantErr: true},
		{name: "empty model", mutate: func(r *CreationSubmitRequest) { r.Model = "  " }, wantErr: true},
		{name: "invalid protocol", mutate: func(r *CreationSubmitRequest) { r.Protocol = "grpc" }, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := submitRequest(option, tc.mutate)
			err := ValidateCreationSubmit(request, option)
			if tc.wantErr {
				require.Error(t, err)
				require.Equal(t, "INVALID_CREATION_REQUEST", infraerrors.Reason(err))
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateCreationSubmitV5Params(t *testing.T) {
	option := creationImageOptionFixture()
	cases := []struct {
		name    string
		params  map[string]any
		wantErr bool
	}{
		{name: "select ok", params: map[string]any{"size": "1536x1024", "quality": "high"}},
		{name: "integer ok", params: map[string]any{"n": 4}},
		{name: "integer as string ok", params: map[string]any{"n": "2"}},
		{name: "unknown key", params: map[string]any{"seed": 1}, wantErr: true},
		{name: "image role is not a param", params: map[string]any{"source": "cupl_x"}, wantErr: true},
		{name: "select value not offered", params: map[string]any{"size": "4096x4096"}, wantErr: true},
		{name: "select wrong type", params: map[string]any{"size": true}, wantErr: true},
		{name: "integer above max", params: map[string]any{"n": 5}, wantErr: true},
		{name: "integer below min", params: map[string]any{"n": 0}, wantErr: true},
		{name: "integer not a number", params: map[string]any{"n": "two"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := submitRequest(option, func(r *CreationSubmitRequest) { r.Params = tc.params })
			err := ValidateCreationSubmit(request, option)
			if tc.wantErr {
				require.Error(t, err)
				require.Equal(t, "INVALID_CREATION_REQUEST", infraerrors.Reason(err))
				return
			}
			require.NoError(t, err)
		})
	}

	t.Run("duration step", func(t *testing.T) {
		minimax := creationMiniMaxV1OptionFixture()
		valid := submitRequest(minimax, func(r *CreationSubmitRequest) { r.Params = map[string]any{"duration": 10} })
		require.NoError(t, ValidateCreationSubmit(valid, minimax))
		invalid := submitRequest(minimax, func(r *CreationSubmitRequest) { r.Params = map[string]any{"duration": 7} })
		require.Error(t, ValidateCreationSubmit(invalid, minimax))
	})

	t.Run("ratio", func(t *testing.T) {
		candidate := creationCandidate{mode: CreationModeVideo, protocol: CreationProtocolGrok, model: "grok-imagine-video"}
		grokVideo := CreationModelOption{
			Mode: CreationModeVideo, Protocol: CreationProtocolGrok, Model: "grok-imagine-video",
			Params: candidate.descriptors(true),
		}
		require.NoError(t, ValidateCreationSubmit(submitRequest(grokVideo, func(r *CreationSubmitRequest) {
			r.Params = map[string]any{"resolution": "1080p", "duration": 10}
		}), grokVideo))
		err := ValidateCreationSubmit(submitRequest(grokVideo, func(r *CreationSubmitRequest) {
			r.Params = map[string]any{"resolution": "4k"}
		}), grokVideo)
		require.Error(t, err)
	})
}

func TestValidateCreationSubmitV6Images(t *testing.T) {
	option := creationImageOptionFixture()
	cases := []struct {
		name    string
		images  []CreationImageRef
		wantErr bool
	}{
		{name: "one source", images: []CreationImageRef{{Role: "source", UploadID: "cupl_a"}}},
		{name: "unknown role", images: []CreationImageRef{{Role: "reference", UploadID: "cupl_a"}}, wantErr: true},
		{name: "max count exceeded", images: []CreationImageRef{
			{Role: "source", UploadID: "cupl_a"}, {Role: "source", UploadID: "cupl_b"},
		}, wantErr: true},
		{name: "duplicate upload", images: []CreationImageRef{
			{Role: "source", UploadID: "cupl_a"}, {Role: "source", UploadID: "cupl_a"},
		}, wantErr: true},
		{name: "empty upload id", images: []CreationImageRef{{Role: "source", UploadID: " "}}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := submitRequest(option, func(r *CreationSubmitRequest) { r.Images = tc.images })
			err := ValidateCreationSubmit(request, option)
			if tc.wantErr {
				require.Error(t, err)
				require.Equal(t, "INVALID_CREATION_REQUEST", infraerrors.Reason(err))
				return
			}
			require.NoError(t, err)
		})
	}

	t.Run("disabled image descriptor", func(t *testing.T) {
		disabled := creationImageOptionFixture()
		for i := range disabled.Params {
			if disabled.Params[i].Kind == CreationParamKindImage {
				disabled.Params[i].Disabled = true
			}
		}
		request := submitRequest(disabled, func(r *CreationSubmitRequest) {
			r.Images = []CreationImageRef{{Role: "source", UploadID: "cupl_a"}}
		})
		require.Error(t, ValidateCreationSubmit(request, disabled))
	})
}

func TestValidateCreationSubmitV9Exclusivity(t *testing.T) {
	seedance := creationSeedanceOptionFixture(t)
	mixed := submitRequest(seedance, func(r *CreationSubmitRequest) {
		r.Images = []CreationImageRef{{Role: "reference", UploadID: "cupl_a"}, {Role: "first_frame", UploadID: "cupl_b"}}
	})
	require.Error(t, ValidateCreationSubmit(mixed, seedance))

	referenceOnly := submitRequest(seedance, func(r *CreationSubmitRequest) {
		r.Images = []CreationImageRef{{Role: "reference", UploadID: "cupl_a"}, {Role: "reference", UploadID: "cupl_b"}}
	})
	require.NoError(t, ValidateCreationSubmit(referenceOnly, seedance))

	// MiniMax v2 shares the rule; the v1 shape has no reference role at all.
	minimaxV2 := CreationModelOption{
		Mode: CreationModeVideo, Protocol: CreationProtocolMiniMax, Model: "MiniMax-H3",
		Params: creationCandidate{
			mode: CreationModeVideo, protocol: CreationProtocolMiniMax, model: "MiniMax-H3",
			limits: videoModelLimits{Resolutions: []string{"768p", "2k"}, MinDuration: 4, MaxDuration: 15},
		}.descriptors(true),
	}
	mixedV2 := submitRequest(minimaxV2, func(r *CreationSubmitRequest) {
		r.Images = []CreationImageRef{{Role: "reference", UploadID: "cupl_a"}, {Role: "last_frame", UploadID: "cupl_b"}}
	})
	require.Error(t, ValidateCreationSubmit(mixedV2, minimaxV2))

	// Gemini accepts a first frame together with references.
	gemini := creationGeminiOptionFixture()
	geminiMixed := submitRequest(gemini, func(r *CreationSubmitRequest) {
		r.Images = []CreationImageRef{{Role: "reference", UploadID: "cupl_a"}, {Role: "first_frame", UploadID: "cupl_b"}}
	})
	require.NoError(t, ValidateCreationSubmit(geminiMixed, gemini))
}

func TestResolveCreationParamsFillsDefaults(t *testing.T) {
	option := creationImageOptionFixture()
	resolved, err := ResolveCreationParams(option, map[string]any{"n": 2})
	require.NoError(t, err)
	require.Equal(t, "1024x1024", resolved["size"])
	require.Equal(t, "auto", resolved["quality"])
	require.Equal(t, 2, resolved["n"])
	_, hasImage := resolved["source"]
	require.False(t, hasImage, "image descriptors are resolved from uploads, not params")

	_, err = ResolveCreationParams(option, map[string]any{"unknown": 1})
	require.Error(t, err)
}

func TestCreationReplayMarkerIsContextScoped(t *testing.T) {
	require.False(t, IsCreationStudioReplay(nil))
	require.False(t, IsCreationStudioReplay(t.Context()))
	marked := WithCreationStudioReplay(t.Context())
	require.True(t, IsCreationStudioReplay(marked))
	// A derived context keeps the marker; a fresh one does not.
	require.True(t, IsCreationStudioReplay(marked))
	require.False(t, IsCreationStudioReplay(t.Context()))
}
