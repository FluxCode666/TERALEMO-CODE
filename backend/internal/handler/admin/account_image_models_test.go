//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountImageModelCatalogReferenceCapability(t *testing.T) {
	for _, tc := range []struct {
		name, platform, protocol string
		mapping                  map[string]any
		want                     map[string]bool
	}{
		{"mapped aliases", "image", "gemini", map[string]any{"public-render-image": "imagen-4.0-generate-001", "imagen-public": "gemini-2.5-flash-image"}, map[string]bool{"public-render-image": false, "imagen-public": true}},
		{"wildcard only", "image", "gemini", map[string]any{"gemini-*": "gemini-2.5-flash-image"}, map[string]bool{"gemini-2.5-flash-image": true, "gemini-3-pro-image-preview": true}},
		{"openai unchanged", "openai", "", map[string]any{"gpt-image-2": "gpt-image-2"}, nil},
		{"gemini unchanged", "gemini", "", nil, nil},
		{"grok unchanged", "grok", "", nil, nil},
		{"video unchanged", "video", "", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{
				ID: 42, Platform: tc.platform, Type: service.AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "secret", "image_protocol": tc.protocol, "model_mapping": tc.mapping},
			}}
			router := setupAvailableModelsRouter(svc)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/42/models", nil))
			require.Equal(t, http.StatusOK, rec.Code)
			var response struct {
				Data []map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			require.NotEmpty(t, response.Data)
			if tc.want != nil {
				require.Len(t, response.Data, len(tc.want))
			}
			for _, model := range response.Data {
				capability, present := model["supports_reference_image"]
				if tc.want == nil {
					require.False(t, present, "other platforms must retain their original structure")
					continue
				}
				id, ok := model["id"].(string)
				require.True(t, ok)
				want, exists := tc.want[id]
				require.True(t, exists)
				require.True(t, present)
				require.Equal(t, want, capability)
				require.Equal(t, "model", model["object"])
				require.Equal(t, "model", model["type"])
				require.Equal(t, id, model["display_name"])
			}
		})
	}
}
