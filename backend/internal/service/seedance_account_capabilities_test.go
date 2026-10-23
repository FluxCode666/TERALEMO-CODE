//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func seedanceCapabilityAccount(mappedModel string, overrides map[string]any) *Account {
	account := seedanceTestAccount()
	account.Platform = PlatformVideo
	account.Credentials["video_protocol"] = VideoProtocolSeedance
	account.Credentials["model_mapping"] = map[string]any{"video": mappedModel}
	for name, value := range overrides {
		account.Credentials[name] = value
	}
	return account
}

func TestSeedanceAccountCapabilitiesAbsentPreservesExistingNormalization(t *testing.T) {
	for _, tc := range []struct{ model, extra string }{
		{"doubao-seedance-1-0-pro", ""},
		{"doubao-seedance-2-5-pro", ""},
		{"doubao-seedance-2-5-pro", `,"draft":true`},
		{"doubao-seedance-1-0-pro", `,"frames":29`},
		{"doubao-seedance-1-0-pro", `,"resolution":"2K"`},
		{"doubao-seedance-2-0-fast", `,"resolution":"1080p"`},
		{"doubao-seedance-1-0-pro", `,"duration":40`},
		{"ep-deployment", `,"duration":-1`},
	} {
		t.Run(tc.model+tc.extra, func(t *testing.T) {
			body := []byte(`{"model":"video","content":[{"type":"text","text":"waves"}]` + tc.extra + `}`)
			wantBody, wantInfo, wantErr := NormalizeSeedanceSubmission(body, tc.model)
			gotBody, gotInfo, gotErr := NormalizeSeedanceSubmissionWithAccount(body, seedanceCapabilityAccount(tc.model, nil))
			if wantErr != nil {
				require.ErrorIs(t, gotErr, ErrVideoAccountCapabilityIncompatible)
				require.ErrorContains(t, gotErr, wantErr.Error())
				return
			}
			require.NoError(t, gotErr)
			require.Equal(t, wantBody, gotBody)
			require.Equal(t, wantInfo, gotInfo)
		})
	}
	// These credentials deliberately have no effect on legacy OpenAI accounts.
	legacy := seedanceCapabilityAccount("doubao-seedance-1-0-pro", map[string]any{"video_resolutions": []string{"1080p"}, "video_min_duration_seconds": 9})
	legacy.Platform = PlatformOpenAI
	body := []byte(`{"model":"video","content":[{"type":"text","text":"waves"}]}`)
	_, info, err := NormalizeSeedanceSubmissionWithAccount(body, legacy)
	require.NoError(t, err)
	require.Equal(t, "720p", info.Resolution)
	require.Equal(t, 5, info.DurationSeconds)
}

func TestSeedanceAccountCapabilitiesSelectDefaultAndReplaceModelResolutionLimits(t *testing.T) {
	for _, tc := range []struct {
		name, model, prompt, extra, want string
		resolutions                      []string
		incompatible                     bool
	}{
		{"lowest configured default", "doubao-seedance-1-0-pro", "waves", "", "1080p", []string{"2K", "1080P"}, false},
		{"2K only default", "doubao-seedance-1-0-pro", "waves", "", "2k", []string{"2K"}, false},
		{"mapped fast proxy override", "doubao-seedance-2-0-fast", "waves", `,"resolution":"1080p"`, "1080p", []string{"1080p"}, false},
		{"strong unsupported", "doubao-seedance-1-0-pro", "waves", `,"resolution":"720p"`, "", []string{"480p"}, true},
		{"weak unsupported stays explicit", "doubao-seedance-1-0-pro", "waves --rs 720p", "", "", []string{"480p"}, true},
		{"weak supported", "doubao-seedance-1-0-pro", "waves --resolution 1080p", "", "1080p", []string{"1080p"}, false},
		{"draft uses account 480 default", "doubao-seedance-2-5-pro", "waves", `,"draft":true`, "480p", []string{"480p"}, false},
		{"draft explicit 720 never falls back", "doubao-seedance-2-5-pro", "waves", `,"draft":true,"resolution":"720p"`, "", []string{"480p"}, true},
		{"draft cannot use configured 720", "doubao-seedance-2-5-pro", "waves", `,"draft":true`, "", []string{"720p"}, true},
		{"opaque draft explicit fixed timing", "ep-deployment", "waves", `,"draft":true,"duration":6`, "480p", []string{"480p"}, false},
		{"opaque draft default is not explicit timing", "ep-deployment", "waves", `,"draft":true`, "", []string{"480p"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"model":"video","content":[{"type":"text","text":"` + tc.prompt + `"}]` + tc.extra + `}`)
			if tc.name == "draft uses account 480 default" {
				_, scheduled, pinnedAccount, err := (&OpenAIGatewayService{}).PrepareSeedanceDraftSubmission(context.Background(), nil, 0, body)
				require.NoError(t, err, "ordinary draft parsing must wait for account defaults")
				require.Zero(t, pinnedAccount)
				require.Equal(t, "720p", scheduled.Resolution, "pre-selection keeps the existing provisional tier")
				require.False(t, gjson.GetBytes(body, "resolution").Exists(), "preparation never injects a raw strong field")
			}
			account := seedanceCapabilityAccount(tc.model, map[string]any{"video_resolutions": tc.resolutions})
			prepared, info, err := NormalizeSeedanceSubmissionWithAccount(body, account)
			if tc.incompatible {
				require.ErrorIs(t, err, ErrVideoAccountCapabilityIncompatible)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, info.Resolution)
			require.Equal(t, tc.want, gjson.GetBytes(prepared, "resolution").String())
			require.Equal(t, "video", info.Model)
			require.Equal(t, "video", gjson.GetBytes(prepared, "model").String(), "normalization does not map twice")
			second, secondInfo, err := NormalizeSeedanceSubmissionWithAccount(prepared, account)
			require.NoError(t, err)
			require.Equal(t, prepared, second)
			require.Equal(t, info, secondInfo)
		})
	}
}

func TestSeedanceAccountCapabilitiesClampDefaultsAndKeepSmartCeiling(t *testing.T) {
	for _, tc := range []struct {
		name, model, extra string
		minimum, maximum   int
		want, ceiling      int
		incompatible       bool
	}{
		{"default up to configured min", "doubao-seedance-1-0-pro", "", 8, 0, 8, 8, false},
		{"default down to configured max", "doubao-seedance-1-0-pro", "", 0, 3, 3, 3, false},
		{"fixed 60 supported by proxy", "doubao-seedance-1-0-pro", `,"duration":60`, 1, 60, 60, 60, false},
		{"fixed duration beyond native range", "doubao-seedance-2-5-pro", `,"duration":40`, 0, 45, 40, 40, false},
		{"explicit below minimum", "doubao-seedance-1-0-pro", `,"duration":3`, 4, 0, 0, 0, true},
		{"explicit above maximum", "doubao-seedance-2-5-pro", `,"duration":11`, 0, 10, 0, 0, true},
		{"smart default expands upper bound", "doubao-seedance-2-5-pro", "", 0, 60, -1, 60, false},
		{"smart explicit narrows upper bound", "doubao-seedance-2-5-pro", `,"duration":-1`, 0, 10, -1, 10, false},
		{"smart min-only keeps native ceiling", "doubao-seedance-2-5-pro", "", 6, 0, -1, 30, false},
		{"one-sided minimum conflict", "doubao-seedance-1-0-pro", "", 13, 0, 0, 0, true},
		{"one-sided maximum conflict", "doubao-seedance-1-0-pro", "", 0, 1, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caps := map[string]any{}
			if tc.minimum > 0 {
				caps["video_min_duration_seconds"] = tc.minimum
			}
			if tc.maximum > 0 {
				caps["video_max_duration_seconds"] = tc.maximum
			}
			account := seedanceCapabilityAccount(tc.model, caps)
			body := []byte(`{"model":"video","content":[{"type":"text","text":"waves"}]` + tc.extra + `}`)
			prepared, info, err := NormalizeSeedanceSubmissionWithAccount(body, account)
			if tc.incompatible {
				require.ErrorIs(t, err, ErrVideoAccountCapabilityIncompatible)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, info.DurationSeconds)
			require.EqualValues(t, tc.want, gjson.GetBytes(prepared, "duration").Int())
			require.Equal(t, tc.ceiling, SeedanceReservationDurationForAccount(info, account))
			second, secondInfo, err := NormalizeSeedanceSubmissionWithAccount(prepared, account)
			require.NoError(t, err)
			require.Equal(t, prepared, second)
			require.Equal(t, info, secondInfo)
		})
	}
	account := seedanceCapabilityAccount("ep-opaque", map[string]any{"video_max_duration_seconds": 40})
	prepared, info, err := NormalizeSeedanceSubmissionWithAccount([]byte(`{"model":"video","content":[{"type":"text","text":"waves"}],"duration":-1}`), account)
	require.NoError(t, err)
	require.EqualValues(t, -1, gjson.GetBytes(prepared, "duration").Int())
	require.Equal(t, 40, SeedanceReservationDurationForAccount(info, account))
	account.Credentials["video_resolutions"] = []string{"480p"}
	_, info, err = NormalizeSeedanceSubmissionWithAccount([]byte(`{"model":"video","content":[{"type":"text","text":"waves"}],"duration":-1,"draft":true}`), account)
	require.NoError(t, err, "an explicit ceiling also bounds an opaque draft")
	require.Equal(t, "480p", info.Resolution)
	account = seedanceCapabilityAccount("ep-opaque", map[string]any{"video_min_duration_seconds": 2})
	_, _, err = NormalizeSeedanceSubmissionWithAccount([]byte(`{"model":"video","content":[{"type":"text","text":"waves"}],"duration":-1}`), account)
	require.ErrorIs(t, err, ErrVideoAccountCapabilityIncompatible, "a minimum alone does not establish a smart ceiling")
	for _, timing := range []string{`"duration":6.5`, `"duration":0`, `"duration":61`, `"frames":30`} {
		_, _, err = NormalizeSeedanceSubmissionWithAccount([]byte(`{"model":"video","content":[{"type":"text","text":"waves"}],`+timing+`}`), seedanceCapabilityAccount("doubao-seedance-1-0-pro", map[string]any{"video_min_duration_seconds": 1, "video_max_duration_seconds": 60}))
		require.Error(t, err, "structural native timing restrictions remain")
	}
}

func TestSeedanceAccountCapabilitiesCheckExactFramesWithoutClamping(t *testing.T) {
	for _, tc := range []struct {
		frames, minimum, maximum int
		incompatible             bool
	}{
		{49, 1, 2, true}, // 2.0417s must not pass a 2s maximum.
		{45, 2, 5, true}, // 1.875s must not round up into a 2s minimum.
		{49, 2, 3, false},
		{29, 0, 0, false},  // Unconfigured legal 29-frame requests remain valid.
		{29, 0, 5, false},  // A max alone does not add an integer-duration minimum.
		{289, 1, 0, false}, // A min alone preserves the native upper frame endpoint.
	} {
		caps := map[string]any{}
		if tc.minimum > 0 {
			caps["video_min_duration_seconds"] = tc.minimum
		}
		if tc.maximum > 0 {
			caps["video_max_duration_seconds"] = tc.maximum
		}
		account := seedanceCapabilityAccount("doubao-seedance-1-0-pro", caps)
		body, err := sjson.SetBytes([]byte(`{"model":"video","content":[{"type":"text","text":"waves"}],"duration":60}`), "frames", tc.frames)
		require.NoError(t, err)
		prepared, info, err := NormalizeSeedanceSubmissionWithAccount(body, account)
		if tc.incompatible {
			require.ErrorIs(t, err, ErrVideoAccountCapabilityIncompatible)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, float64(tc.frames)/24, info.DurationSecondsExact)
		require.EqualValues(t, tc.frames, gjson.GetBytes(prepared, "frames").Int())
		require.False(t, gjson.GetBytes(prepared, "duration").Exists(), "frames keep precedence over duration")
		second, secondInfo, err := NormalizeSeedanceSubmissionWithAccount(prepared, account)
		require.NoError(t, err)
		require.Equal(t, prepared, second)
		require.Equal(t, info, secondInfo)
	}
}

func TestSeedanceAccountCapabilitiesForwardUsesNormalizedInputs(t *testing.T) {
	account := seedanceCapabilityAccount("doubao-seedance-2-5-pro", map[string]any{"video_resolutions": []string{"1080p"}, "video_max_duration_seconds": 45})
	body := []byte(`{"model":"video","content":[{"type":"text","text":"waves"}]}`)
	prepared, info, err := NormalizeSeedanceSubmissionWithAccount(body, account)
	require.NoError(t, err)
	upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"id":"task-1"}`)}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	c, _ := grokMediaContentTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", nil)
	result, err := svc.ForwardSeedance(context.Background(), c, account, SeedanceEndpointCreate, "", prepared)
	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	payload, err := io.ReadAll(upstream.request.Body)
	require.NoError(t, err)
	require.Equal(t, "doubao-seedance-2-5-pro", gjson.GetBytes(payload, "model").String())
	require.Equal(t, "1080p", gjson.GetBytes(payload, "resolution").String())
	require.EqualValues(t, -1, gjson.GetBytes(payload, "duration").Int())
	require.Equal(t, info.Resolution, result.VideoResolution)
	require.Equal(t, info.DurationSeconds, result.VideoDurationSeconds)
	require.Equal(t, 45, SeedanceReservationDurationForAccount(info, account))
}

func TestSeedanceAccountCapabilitiesKeepDraftFinalInheritedInputs(t *testing.T) {
	for _, tc := range []struct {
		name        string
		resolutions []string
		maximum     int
		blocked     bool
	}{
		{"1080 supported", []string{"480p", "1080p"}, 10, false},
		{"1080 absent cannot choose lower tier", []string{"480p"}, 10, true},
		{"inherited duration cannot clamp", []string{"1080p"}, 6, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, key, account, _, _, _ := seedanceDraftFixture(t)
			account.Platform = PlatformVideo
			account.Credentials["video_protocol"] = VideoProtocolSeedance
			account.Credentials["video_resolutions"] = tc.resolutions
			account.Credentials["video_max_duration_seconds"] = tc.maximum
			svc.accountRepo.(*stubOpenAIAccountRepo).accounts[0] = *account
			ctx, _, _, err := svc.PrepareSeedanceDraftSubmission(context.Background(), key, 10, []byte(seedanceDraftTestBody))
			if tc.name == "inherited duration cannot clamp" {
				require.ErrorIs(t, err, ErrSeedanceDraftTaskUnavailable)
				return
			}
			require.NoError(t, err)
			prepared, info, err := svc.NormalizeSeedanceSubmissionForAccount(ctx, []byte(seedanceDraftTestBody), account)
			if tc.blocked {
				require.ErrorIs(t, err, ErrVideoAccountCapabilityIncompatible)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "1080p", info.Resolution)
			require.Equal(t, 7, info.DurationSeconds)
			require.False(t, gjson.GetBytes(prepared, "duration").Exists())
		})
	}
}

func TestSeedanceAccountCapabilitiesAllowConfiguredLongDraftSourceOnly(t *testing.T) {
	for _, maximum := range []int{0, 45} {
		name := "unconfigured"
		if maximum > 0 {
			name = "configured-45"
		}
		t.Run(name, func(t *testing.T) {
			svc, key, account, repo, _, upstream := seedanceDraftFixture(t)
			account.Platform = PlatformVideo
			account.Credentials["video_protocol"] = VideoProtocolSeedance
			if maximum > 0 {
				account.Credentials["video_max_duration_seconds"] = maximum
			}
			svc.accountRepo.(*stubOpenAIAccountRepo).accounts[0] = *account
			var contract VideoPricingContract
			var snapshot VideoBillingSnapshot
			require.NoError(t, json.Unmarshal(repo.source.Contract, &contract))
			require.NoError(t, json.Unmarshal(repo.source.Snapshot, &snapshot))
			contract.MaxDurationSeconds, snapshot.RequestedDurationSeconds = 45, -1
			var err error
			repo.source.Contract, err = json.Marshal(contract)
			require.NoError(t, err)
			repo.source.Snapshot, err = json.Marshal(snapshot)
			require.NoError(t, err)
			status, err := sjson.Set(seedanceDraftTestStatus, "duration", 40)
			require.NoError(t, err)
			upstream.response = grokMediaContentStatusResponse(status)
			ctx, info, _, err := svc.PrepareSeedanceDraftSubmission(context.Background(), key, 10, []byte(seedanceDraftTestBody))
			if maximum == 0 {
				require.ErrorIs(t, err, ErrSeedanceDraftTaskUnavailable, "unconfigured accounts retain the 30s limit")
				return
			}
			require.NoError(t, err)
			require.Equal(t, 40, info.DurationSeconds)
			prepared, finalInfo, err := svc.NormalizeSeedanceSubmissionForAccount(ctx, []byte(seedanceDraftTestBody), account)
			require.NoError(t, err)
			require.Equal(t, info, finalInfo)
			require.Equal(t, 40, SeedanceReservationDurationForAccount(info, account))
			require.False(t, gjson.GetBytes(prepared, "duration").Exists())
		})
	}
}
