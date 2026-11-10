//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageAccountModelsCapabilitiesAndDefault(t *testing.T) {
	for _, tc := range []struct {
		name, protocol string
		mapping        map[string]any
		ids            []string
		references     []bool
	}{
		{"gemini defaults", "gemini", nil, DefaultImageModelIDs("gemini"), []bool{true, true, false}},
		{"openai defaults", "openai", nil, DefaultImageModelIDs("openai"), []bool{true, true, true}},
		{"grok defaults", "grok", nil, DefaultImageModelIDs("grok"), []bool{true}},
		{"concrete", "gemini", map[string]any{"imagen-4.0-generate-001": "imagen-4.0-generate-001", "gemini-2.5-flash-image": "gemini-2.5-flash-image"}, []string{"gemini-2.5-flash-image", "imagen-4.0-generate-001"}, []bool{true, false}},
		{"reverse aliases", "gemini", map[string]any{"public-render-image": "imagen-4.0-generate-001", "imagen-public": "gemini-2.5-flash-image"}, []string{"imagen-public", "public-render-image"}, []bool{true, false}},
		{"normalized imagen target", "gemini", map[string]any{"render": " models/IMAGEN-4.0-generate-001 "}, []string{"render"}, []bool{false}},
		{"wildcard only", "gemini", map[string]any{"gemini-*": "gemini-2.5-flash-image"}, []string{"gemini-2.5-flash-image", "gemini-3-pro-image-preview"}, []bool{true, true}},
		{"wildcard to imagen", "gemini", map[string]any{"gemini-*": "imagen-4.0-generate-001"}, []string{"gemini-2.5-flash-image", "gemini-3-pro-image-preview"}, []bool{false, false}},
		{"specific keys take precedence", "gemini", map[string]any{"public-render-image": "imagen-4.0-generate-001", "gemini-*": "gemini-2.5-flash-image"}, []string{"public-render-image"}, []bool{false}},
		{"openai protocol", "openai", map[string]any{"imagen-public": "imagen-custom"}, []string{"imagen-public"}, []bool{true}},
		{"grok protocol", "grok", map[string]any{"public-render": "imagen-custom"}, []string{"public-render"}, []bool{true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, account, upstream, _, _ := imageConnectionFixture(t, tc.protocol, "", func(w http.ResponseWriter, _ *http.Request) {
				if tc.protocol == ImageProtocolGemini {
					_, _ = io.WriteString(w, `{"predictions":[{"bytesBase64Encoded":"aW1hZ2U=","mimeType":"image/png"}],"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`)
				} else {
					_, _ = io.WriteString(w, `{"data":[{"b64_json":"aW1hZ2U="}]}`)
				}
			})
			if tc.mapping != nil {
				account.Credentials["model_mapping"] = tc.mapping
			}
			catalog, err := NewAccountModelCatalogService(nil, nil).GetAvailableModels(context.Background(), account)
			require.NoError(t, err)
			encoded, err := json.Marshal(catalog)
			require.NoError(t, err)
			var models []struct {
				ID                     string `json:"id"`
				SupportsReferenceImage *bool  `json:"supports_reference_image"`
			}
			require.NoError(t, json.Unmarshal(encoded, &models))
			var ids []string
			for i, model := range models {
				ids = append(ids, model.ID)
				require.NotNil(t, model.SupportsReferenceImage, "false capability must be present in JSON")
				require.Equal(t, tc.references[i], *model.SupportsReferenceImage)
			}
			require.Equal(t, tc.ids, ids)
			c, rec := newTestContext()
			require.NoError(t, svc.TestAccountConnection(c, account.ID, "", "", ""))
			events := imageConnectionEvents(t, rec.Body.String())
			require.Equal(t, ids[0], events[0].Model, "catalog first model and connection default must agree")
			calls := upstream.calls.Load()
			c, _ = newTestContext()
			require.ErrorContains(t, svc.TestAccountConnection(c, account.ID, "unavailable-model", "", ""), "not available")
			require.Equal(t, calls, upstream.calls.Load())
		})
	}
}

func TestImageAccountModelsReferenceValidationUsesMappedModel(t *testing.T) {
	for _, tc := range []struct {
		public, target string
		supported      bool
	}{
		{"public-render-image", "imagen-4.0-generate-001", false},
		{"imagen-public", "gemini-2.5-flash-image", true},
		{"normalized-render", "models/imagen-4.0-generate-001", false},
	} {
		t.Run(tc.public, func(t *testing.T) {
			svc, account, upstream, _, _ := imageConnectionFixture(t, "gemini", "", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`)
			})
			account.Credentials["model_mapping"] = map[string]any{tc.public: tc.target}
			c, _ := newTestContext()
			err := svc.TestAccountConnection(c, account.ID, tc.public, "", "", AccountTestOptions{ImageDataURL: minimalAccountTestPNGDataURL(8, 8)})
			if tc.supported {
				require.NoError(t, err)
				require.EqualValues(t, 1, upstream.calls.Load())
			} else {
				require.ErrorContains(t, err, "Imagen models do not support")
				require.Zero(t, upstream.calls.Load())
			}
		})
	}
}
