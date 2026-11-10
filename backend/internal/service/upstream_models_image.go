package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

const (
	imageUpstreamModelsMaxPages  = 20
	ImageModelsNotIdentifiedCode = "image_models_not_identified"
)

func (s *AccountTestService) buildImageUpstreamModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	if err := ValidateImageAccount(account); err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid image account credentials or protocol", err)
	}
	// Reuse each protocol's request builder without changing the real account
	// used by the transport for proxy selection and concurrency limits.
	protocolAccount := *account
	protocolAccount.Credentials = make(map[string]any, len(account.Credentials)+1)
	for key, value := range account.Credentials {
		protocolAccount.Credentials[key] = value
	}
	protocolAccount.Credentials["base_url"] = account.ImageBaseURL()
	switch account.ImageProtocol() {
	case ImageProtocolGemini:
		protocolAccount.Platform = PlatformGemini
		return s.buildGeminiUpstreamModelsRequest(ctx, &protocolAccount)
	case ImageProtocolGrok:
		protocolAccount.Platform = PlatformGrok
		req, err := s.buildGrokUpstreamModelsRequest(ctx, &protocolAccount)
		if err != nil {
			return nil, err
		}
		req.URL.Path = strings.TrimSuffix(req.URL.Path, "/models") + "/image-generation-models"
		req.URL.RawPath = ""
		return req, nil
	default:
		protocolAccount.Platform = PlatformOpenAI
		return s.buildOpenAIUpstreamModelsRequest(ctx, &protocolAccount)
	}
}

// Image catalogs describe generators, so neither Codex capability enrichment
// nor the configured-model fallback applies to this live discovery path.
func (s *AccountTestService) fetchImageUpstreamModelCatalog(ctx context.Context, account *Account) (*UpstreamModelCatalog, error) {
	if s == nil || s.httpUpstream == nil {
		return nil, newUpstreamModelSyncConfigError("Upstream HTTP client is not configured", nil)
	}
	req, err := s.buildUpstreamModelsRequest(ctx, account)
	if err != nil {
		return nil, err
	}
	protocol := account.ImageProtocol()
	pageToken := ""
	models := make([]string, 0)
	grokImageEndpoint := protocol == ImageProtocolGrok
	for page := 0; page < imageUpstreamModelsMaxPages; page++ {
		pageReq := req.Clone(ctx)
		if pageToken != "" {
			query := pageReq.URL.Query()
			query.Set("pageToken", pageToken)
			pageReq.URL.RawQuery = query.Encode()
		}
		body, err := s.readUpstreamModelsResponse(pageReq, account)
		if err != nil && grokImageEndpoint && upstreamModelListEndpointUnsupported(err) {
			grokImageEndpoint = false
			fallback := req.Clone(ctx)
			fallback.URL.Path = strings.TrimSuffix(fallback.URL.Path, "/image-generation-models") + "/models"
			fallback.URL.RawPath = ""
			body, err = s.readUpstreamModelsResponse(fallback, account)
		}
		if err != nil {
			return nil, err
		}
		extractModels := extractUpstreamModelIDs
		if protocol == ImageProtocolGrok {
			extractModels = extractGrokUpstreamModelIDs
		}
		pageModels, err := extractModels(body)
		if err != nil {
			return nil, newUpstreamModelSyncUpstreamError("Upstream model list response was not valid JSON", err)
		}
		models = append(models, pageModels...)
		pageToken = ""
		if protocol == ImageProtocolGemini {
			var response struct {
				NextPageToken string `json:"nextPageToken"`
			}
			if err := json.Unmarshal(body, &response); err != nil {
				return nil, newUpstreamModelSyncUpstreamError("Upstream model list response was not valid JSON", err)
			}
			pageToken = strings.TrimSpace(response.NextPageToken)
		}
		if pageToken == "" {
			return imageUpstreamModelCatalog(models, protocol, grokImageEndpoint)
		}
	}
	return nil, newUpstreamModelSyncUpstreamError("Upstream model list pagination did not finish", nil)
}

func imageUpstreamModelCatalog(models []string, protocol string, grokImageEndpoint bool) (*UpstreamModelCatalog, error) {
	models = dedupeAndSortModelIDs(models)
	if len(models) == 0 {
		return nil, newUpstreamModelSyncUpstreamError("Upstream returned no supported models", nil)
	}
	filtered := make([]string, 0, len(models))
	for _, model := range models {
		name := strings.ToLower(model)
		keep := strings.Contains(name, "image")
		switch protocol {
		case ImageProtocolGemini:
			keep = keep || strings.HasPrefix(name, "imagen-")
		case ImageProtocolGrok:
			keep = keep || grokImageEndpoint || strings.Contains(name, "imagine")
		default:
			keep = keep || strings.HasPrefix(name, "dall-e-")
		}
		if keep {
			filtered = append(filtered, model)
		}
	}
	catalog := &UpstreamModelCatalog{Models: filtered}
	if len(filtered) == 0 {
		catalog.Models = models
		catalog.Warnings = []UpstreamModelSyncWarning{{
			Code:    ImageModelsNotIdentifiedCode,
			Message: "No image models were identified; all upstream models are returned. Please select image models manually.",
		}}
	}
	return catalog, nil
}
