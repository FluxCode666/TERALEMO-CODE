package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

type imageAccountModel struct {
	openai.Model
	SupportsReferenceImage bool `json:"supports_reference_image"`
}

// The picker and connection tester use this ordering, including the same first
// model when a wildcard-only mapping needs concrete protocol defaults.
func imageAccountModels(account *Account) []imageAccountModel {
	ids := account.ImageModelIDs()
	if len(ids) == 0 {
		for _, id := range DefaultImageModelIDs(account.ImageProtocol()) {
			if account.IsModelSupported(id) {
				ids = append(ids, id)
			}
		}
	}
	models := make([]imageAccountModel, 0, len(ids))
	for _, id := range ids {
		models = append(models, imageAccountModel{
			Model:                  openai.Model{ID: id, Object: "model", Type: "model", DisplayName: id},
			SupportsReferenceImage: imageModelSupportsReference(account, id),
		})
	}
	return models
}

func imageModelSupportsReference(account *Account, publicModel string) bool {
	if account.ImageProtocol() != ImageProtocolGemini {
		return true
	}
	model := strings.TrimPrefix(strings.TrimSpace(account.GetMappedModel(publicModel)), "models/")
	return !strings.HasPrefix(strings.ToLower(model), "imagen-")
}
