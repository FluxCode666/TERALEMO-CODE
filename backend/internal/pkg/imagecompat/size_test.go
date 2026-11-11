//go:build unit

package imagecompat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSize(t *testing.T) {
	cases := []struct {
		in       string
		ok       bool
		hasRatio bool
		ratioW   int
		ratioH   int
		wantTier string
	}{
		{"", true, false, 0, 0, ""},
		{"auto", true, false, 0, 0, ""},
		{" AUTO ", true, false, 0, 0, ""},
		{"1k", true, false, 0, 0, "1K"},
		{"2K", true, false, 0, 0, "2K"},
		{"4k", true, false, 0, 0, "4K"},
		{"1024x1024", true, true, 1, 1, "1K"},
		{"256x256", true, true, 1, 1, "1K"},
		{"1536x1024", true, true, 3, 2, "2K"},
		{"1024x1536", true, true, 2, 3, "2K"},
		{"1792x1024", true, true, 7, 4, "2K"},
		{"2048x2048", true, true, 1, 1, "2K"},
		{"2049x1000", true, true, 2049, 1000, "4K"},
		{"3840x2160", true, true, 16, 9, "4K"},
		{" 1024 x 768 ", true, true, 4, 3, "1K"},
		{"16:9", true, true, 16, 9, ""},
		{"19.5:9", true, true, 13, 6, ""},
		{"9:19.5", true, true, 6, 13, ""},
		{"21:9", true, true, 7, 3, ""},
		{"1.5:1", true, true, 3, 2, ""},
		{"0x1024", false, false, 0, 0, ""},
		{"-1x1024", false, false, 0, 0, ""},
		{"1024x", false, false, 0, 0, ""},
		{"1024x1024x2", false, false, 0, 0, ""},
		{"axb", false, false, 0, 0, ""},
		{"16:0", false, false, 0, 0, ""},
		{"16:9:1", false, false, 0, 0, ""},
		{"1.333:1", false, false, 0, 0, ""}, // more than two decimals
		{"NaN:1", false, false, 0, 0, ""},
		{"large", false, false, 0, 0, ""},
		{"8k", false, false, 0, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			spec, ok := parseSize(tc.in)
			require.Equal(t, tc.ok, ok)
			if !ok {
				return
			}
			require.Equal(t, tc.hasRatio, spec.hasRatio)
			require.Equal(t, tc.ratioW, spec.ratioW)
			require.Equal(t, tc.ratioH, spec.ratioH)
			require.Equal(t, tc.wantTier, spec.tier)
		})
	}
}

func TestAspectRatioTables(t *testing.T) {
	// Built-in labels must reduce to distinct ratios so matching is unambiguous.
	for name, table := range map[string][]aspectRatio{
		"gemini":          geminiAspectRatios,
		"gemini extended": geminiExtendedAspectRatios,
		"imagen":          imagenAspectRatios,
		"grok":            grokAspectRatios,
	} {
		seen := map[[2]int]string{}
		for _, r := range table {
			key := [2]int{r.w, r.h}
			prev, dup := seen[key]
			require.False(t, dup, "%s: %s duplicates %s", name, r.label, prev)
			seen[key] = r.label
		}
	}
	require.Len(t, geminiAspectRatios, 10)
	require.Len(t, geminiExtendedAspectRatios, 14)
	require.Len(t, imagenAspectRatios, 5)
	require.Len(t, grokAspectRatios, 13)

	label, ok := matchAspectRatio(grokAspectRatios, 13, 6)
	require.True(t, ok)
	require.Equal(t, "19.5:9", label)
	label, ok = matchAspectRatio(geminiAspectRatios, 7, 3)
	require.True(t, ok)
	require.Equal(t, "21:9", label)
	_, ok = matchAspectRatio(geminiAspectRatios, 7, 4)
	require.False(t, ok)
}

func TestNewAspectRatioPanicsOnInvalidLabel(t *testing.T) {
	require.Panics(t, func() { newAspectRatio("bogus") })
}

func TestResolveOutputTier(t *testing.T) {
	all := []string{tier1K, tier2K, tier4K}
	upTo2K := []string{tier1K, tier2K}
	cases := []struct {
		name      string
		size      string
		supported []string
		want      string
		wantErr   bool
	}{
		{"empty", "", all, "", false},
		{"auto", "auto", nil, "", false},
		{"ratio only defaults to 1K", "16:9", all, "1K", false},
		{"ratio only on 1K-only model", "16:9", nil, "1K", false},
		{"dims supported", "2048x2048", all, "2K", false},
		{"dims 4K supported", "3840x2160", all, "4K", false},
		{"dims 4K downgraded to 2K", "3840x2160", upTo2K, "2K", false},
		{"dims 2K downgraded to 1K", "1536x1024", nil, "1K", false},
		{"dims 4K downgraded to 1K", "4096x4096", []string{tier1K}, "1K", false},
		{"dims 1K", "1024x1024", nil, "1K", false},
		{"explicit tier supported", "2K", upTo2K, "2K", false},
		{"explicit 1K on 1K-only", "1k", nil, "1K", false},
		{"explicit 4K rejected", "4K", upTo2K, "", true},
		{"explicit 2K rejected on 1K-only", "2k", nil, "", true},
		// Defensive: a table without any tier at or below the derived one.
		{"no lower tier available", "1024x1024", []string{tier2K}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec, ok := parseSize(tc.size)
			require.True(t, ok)
			got, err := resolveOutputTier("test protocol", spec, tc.supported)
			if tc.wantErr {
				require.NotNil(t, err)
				require.Equal(t, "size", err.Param)
				require.Equal(t, 400, err.HTTPStatus)
				require.Empty(t, got)
				return
			}
			require.Nil(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
