package imagecompat

import (
	"math"
	"strconv"
	"strings"
)

// Resolution tiers derived from the OpenAI size parameter. The tiering matches
// the repository's image billing tiers (longest edge <=1024 → 1K, <=2048 → 2K,
// otherwise 4K) so the tier sent upstream is the tier the request is billed at.
const (
	tier1K = "1K"
	tier2K = "2K"
	tier4K = "4K"
)

// aspectRatio is an upstream aspect-ratio label together with its reduced
// integer form, e.g. "19.5:9" → 13:6, "21:9" → 7:3.
type aspectRatio struct {
	label string
	w, h  int
}

func newAspectRatio(label string) aspectRatio {
	w, h, ok := parseRatio(label)
	if !ok {
		panic("imagecompat: invalid built-in aspect ratio " + label)
	}
	return aspectRatio{label: label, w: w, h: h}
}

func ratioLabels(ratios []aspectRatio) string {
	labels := make([]string, 0, len(ratios))
	for _, r := range ratios {
		labels = append(labels, r.label)
	}
	return strings.Join(labels, ", ")
}

func matchAspectRatio(ratios []aspectRatio, w, h int) (string, bool) {
	for _, r := range ratios {
		if r.w == w && r.h == h {
			return r.label, true
		}
	}
	return "", false
}

// sizeSpec is the parsed OpenAI size parameter.
type sizeSpec struct {
	raw          string
	hasRatio     bool
	ratioW       int // reduced
	ratioH       int // reduced
	tier         string
	explicitTier bool // tier came from a literal "1K"/"2K"/"4K", not from WIDTHxHEIGHT
}

func (s sizeSpec) ratioString() string {
	return strconv.Itoa(s.ratioW) + ":" + strconv.Itoa(s.ratioH)
}

// parseSize accepts:
//   - "" / "auto": no geometry, upstream default
//   - "1K" / "2K" / "4K" (case-insensitive): resolution tier only
//   - "WIDTHxHEIGHT": exact aspect ratio + tier from the longest edge
//   - "W:H" (e.g. "16:9", "19.5:9"): aspect ratio only
func parseSize(raw string) (sizeSpec, bool) {
	trimmed := strings.TrimSpace(raw)
	spec := sizeSpec{raw: trimmed}
	normalized := strings.ToLower(trimmed)
	switch normalized {
	case "", "auto":
		return spec, true
	case "1k":
		spec.tier = tier1K
		spec.explicitTier = true
		return spec, true
	case "2k":
		spec.tier = tier2K
		spec.explicitTier = true
		return spec, true
	case "4k":
		spec.tier = tier4K
		spec.explicitTier = true
		return spec, true
	}
	if strings.Contains(normalized, "x") {
		parts := strings.Split(normalized, "x")
		if len(parts) != 2 {
			return spec, false
		}
		width, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
		height, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
		if errW != nil || errH != nil || width <= 0 || height <= 0 {
			return spec, false
		}
		div := gcd(width, height)
		spec.hasRatio = true
		spec.ratioW, spec.ratioH = width/div, height/div
		spec.tier = tierFromLongestEdge(max(width, height))
		return spec, true
	}
	if strings.Contains(normalized, ":") {
		w, h, ok := parseRatio(normalized)
		if !ok {
			return spec, false
		}
		spec.hasRatio = true
		spec.ratioW, spec.ratioH = w, h
		return spec, true
	}
	return spec, false
}

var tierRank = map[string]int{tier1K: 1, tier2K: 2, tier4K: 3}

// resolveOutputTier returns the resolution tier the upstream will actually
// produce for spec on a model supporting the tiers in supported (nil means the
// model only produces its 1K default):
//   - "" / "auto": "" — the upstream decides; callers keep their default billing
//   - aspect ratio only ("16:9"): "1K", the documented upstream default
//   - explicit "1K"/"2K"/"4K": must be supported, otherwise 400 — the client
//     asked for that exact tier
//   - WIDTHxHEIGHT: the tier derived from the longest edge, downgraded to the
//     highest supported tier when the model cannot produce it (the aspect
//     ratio itself is still matched exactly by the caller)
func resolveOutputTier(label string, spec sizeSpec, supported []string) (string, *Error) {
	if len(supported) == 0 {
		supported = []string{tier1K}
	}
	switch {
	case spec.tier == "" && !spec.hasRatio:
		return "", nil
	case spec.tier == "":
		return tier1K, nil
	case containsString(supported, spec.tier):
		return spec.tier, nil
	case spec.explicitTier:
		return "", unsupportedTier(label, spec, supported)
	}
	best := ""
	for _, candidate := range supported {
		if rank := tierRank[candidate]; rank <= tierRank[spec.tier] && rank > tierRank[best] {
			best = candidate
		}
	}
	if best == "" {
		return "", unsupportedTier(label, spec, supported)
	}
	return best, nil
}

func tierFromLongestEdge(edge int) string {
	switch {
	case edge <= 1024:
		return tier1K
	case edge <= 2048:
		return tier2K
	default:
		return tier4K
	}
}

// parseRatio parses "W:H" where W and H may carry up to two decimals and
// returns the reduced integer ratio.
func parseRatio(value string) (int, int, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, okW := scaledRatioPart(parts[0])
	h, okH := scaledRatioPart(parts[1])
	if !okW || !okH {
		return 0, 0, false
	}
	div := gcd(w, h)
	return w / div, h / div, true
}

func scaledRatioPart(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 || f > 10000 {
		return 0, false
	}
	scaled := f * 100
	rounded := math.Round(scaled)
	if math.Abs(scaled-rounded) > 1e-6 || rounded < 1 {
		return 0, false
	}
	return int(rounded), true
}

func gcd(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}
