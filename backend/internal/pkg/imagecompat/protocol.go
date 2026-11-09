// Package imagecompat converts OpenAI Images API requests (generations and
// edits) into Gemini (generateContent / Imagen predict) and xAI Grok image
// requests, and converts the upstream responses and errors back into the
// OpenAI image response / error shapes.
//
// The package is pure: it performs no network IO and does not depend on the
// service or handler layers. Callers build a Plan with BuildPlan, execute every
// UpstreamCall themselves (adding the base URL and authentication headers), and
// pass the raw results to BuildResponse.
package imagecompat

import (
	"fmt"
	"strings"
)

// Protocol identifies the upstream image API spoken by an image account.
type Protocol string

const (
	// ProtocolOpenAI is a passthrough protocol; imagecompat does not convert it.
	ProtocolOpenAI Protocol = "openai"
	// ProtocolGemini covers Gemini generateContent image models and Imagen
	// (models whose name starts with "imagen-").
	ProtocolGemini Protocol = "gemini"
	// ProtocolGrok is the xAI Images API.
	ProtocolGrok Protocol = "grok"
)

// ParseProtocol trims and lower-cases s. An empty value means ProtocolOpenAI;
// unknown values return an error.
func ParseProtocol(s string) (Protocol, error) {
	switch normalized := strings.ToLower(strings.TrimSpace(s)); normalized {
	case "", string(ProtocolOpenAI):
		return ProtocolOpenAI, nil
	case string(ProtocolGemini):
		return ProtocolGemini, nil
	case string(ProtocolGrok):
		return ProtocolGrok, nil
	default:
		return "", fmt.Errorf("unsupported image protocol %q (supported: openai, gemini, grok)", normalized)
	}
}

// Operation is the OpenAI Images API operation being converted.
type Operation string

const (
	OperationGenerations Operation = "generations"
	OperationEdits       Operation = "edits"
)

// NeedsInlineImageData reports whether the protocol requires reference images
// as inline bytes. When true, callers must download URL reference images into
// InputImage.Data (with SSRF protection) before calling BuildPlan.
func NeedsInlineImageData(p Protocol) bool {
	return p == ProtocolGemini
}
