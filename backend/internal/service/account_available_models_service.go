package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

const (
	accountModelIntersectionTTL         = 60 * time.Second
	accountModelIntersectionCacheLimit  = 256
	accountModelIntersectionConcurrency = 5
)

// AccountModelCatalogService shares account model selection and short-lived intersections.
type AccountModelCatalogService struct {
	accountRepo AccountRepository
	accountTest *AccountTestService
	mu          sync.Mutex
	cache       map[string]accountModelIntersectionCacheEntry
	fetchSlots  chan struct{}
	now         func() time.Time
	fetchModels func(context.Context, *Account) (any, error)
}

type accountModelIntersectionCacheEntry struct {
	catalogs  map[int64][]AccountCatalogModel
	failed    map[int64]bool
	expiresAt time.Time
}

// AccountCatalogModel is the common picker projection of each platform's catalog.
type AccountCatalogModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type AccountModelIntersection struct {
	Platform         string                `json:"platform"`
	Models           []AccountCatalogModel `json:"models"`
	AccountCount     int                   `json:"account_count"`
	FailedAccountIDs []int64               `json:"failed_account_ids"`
}

func NewAccountModelCatalogService(accountRepo AccountRepository, accountTest *AccountTestService) *AccountModelCatalogService {
	return &AccountModelCatalogService{
		accountRepo: accountRepo, accountTest: accountTest,
		cache:      make(map[string]accountModelIntersectionCacheEntry),
		fetchSlots: make(chan struct{}, accountModelIntersectionConcurrency), now: time.Now,
	}
}

// GetAvailableModels preserves each platform's existing JSON shape and discovery fallback.
func (s *AccountModelCatalogService) GetAvailableModels(ctx context.Context, account *Account) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if account == nil {
		return nil, ErrAccountNotFound
	}
	if account.Platform == PlatformVideo {
		models := make([]openai.Model, 0)
		for _, id := range account.VideoTestModelIDs() {
			models = append(models, openai.Model{ID: id, Object: "model", Type: "model", DisplayName: id})
		}
		return models, nil
	}

	// Handle OpenAI accounts
	if account.IsOpenAI() {
		// Prefer the shared, account-keyed upstream catalog. If discovery fails,
		// retain the legacy local catalog below so the test dialog remains usable.
		if s.accountTest != nil {
			if models, fetchErr := s.accountTest.FetchOpenAIAccountModels(ctx, account); fetchErr == nil {
				return models, nil
			}
		}
		// OpenAI 自动透传会绕过常规模型改写，测试/模型列表也应回落到默认模型集。
		if account.IsOpenAIPassthroughEnabled() {
			return openai.DefaultModels, nil
		}

		mapping := account.GetModelMapping()
		if len(mapping) == 0 {
			return openai.DefaultModels, nil
		}

		// Return mapped models
		var models []openai.Model
		for requestedModel := range mapping {
			var found bool
			for _, dm := range openai.DefaultModels {
				if dm.ID == requestedModel {
					models = append(models, dm)
					found = true
					break
				}
			}
			if !found {
				models = append(models, openai.Model{
					ID:          requestedModel,
					Object:      "model",
					Type:        "model",
					DisplayName: requestedModel,
				})
			}
		}
		return models, nil
	}

	// Handle Gemini accounts
	if account.IsGemini() {
		// Consumer Google One OAuth still uses the legacy Gemini CLI / Code
		// Assist channel. Do not advertise newer 3.x or image models that the
		// channel cannot serve.
		if account.IsOAuth() {
			if account.IsGeminiGoogleOne() {
				return geminicli.GoogleOneModels, nil
			}
			return geminicli.DefaultModels, nil
		}

		// For API Key accounts: return models based on model_mapping
		mapping := account.GetModelMapping()
		if len(mapping) == 0 {
			return geminicli.DefaultModels, nil
		}

		var models []geminicli.Model
		for requestedModel := range mapping {
			var found bool
			for _, dm := range geminicli.DefaultModels {
				if dm.ID == requestedModel {
					models = append(models, dm)
					found = true
					break
				}
			}
			if !found {
				models = append(models, geminicli.Model{
					ID:          requestedModel,
					Type:        "model",
					DisplayName: requestedModel,
					CreatedAt:   "",
				})
			}
		}
		return models, nil
	}

	// Explicit account mappings expose their request-side names to connectivity tests.
	if account.Platform == PlatformAntigravity {
		return AntigravityAccountTestModels(account.Credentials["model_mapping"]), nil
	}

	// Handle Grok accounts
	if account.Platform == PlatformGrok {
		defaultModels := xai.DefaultModels()

		hasExplicitMapping := false
		switch rawMapping := account.Credentials["model_mapping"].(type) {
		case map[string]any:
			hasExplicitMapping = len(rawMapping) > 0
		case map[string]string:
			hasExplicitMapping = len(rawMapping) > 0
		}
		if !hasExplicitMapping {
			return defaultModels, nil
		}

		mapping := account.GetModelMapping()
		if len(mapping) == 0 {
			return defaultModels, nil
		}

		defaultByID := make(map[string]xai.Model, len(defaultModels))
		for _, model := range defaultModels {
			defaultByID[model.ID] = model
		}

		requestedModels := make([]string, 0, len(mapping))
		for requestedModel := range mapping {
			requestedModels = append(requestedModels, requestedModel)
		}
		sort.Strings(requestedModels)

		var models []xai.Model
		for _, requestedModel := range requestedModels {
			if defaultModel, found := defaultByID[requestedModel]; found {
				models = append(models, defaultModel)
				continue
			}
			models = append(models, xai.Model{
				ID:          requestedModel,
				Object:      "model",
				OwnedBy:     "xai",
				DisplayName: requestedModel,
			})
		}
		return models, nil
	}

	// TypeSafe accounts serve only the native System One model.
	if account.IsTypeSafe() {
		return []claude.Model{{ID: typesafe.JevLatestModel, Type: "model", DisplayName: typesafe.JevLatestModel}}, nil
	}

	// Handle Claude/Anthropic accounts
	// For OAuth and Setup-Token accounts: return default models
	if account.IsOAuth() {
		return claude.DefaultModels, nil
	}

	// For API Key accounts: return models based on model_mapping
	mapping := account.GetModelMapping()
	if len(mapping) == 0 {
		// No mapping configured, return default models
		return claude.DefaultModels, nil
	}

	// Return mapped models (keys of the mapping are the available model IDs)
	var models []claude.Model
	for requestedModel := range mapping {
		// Try to find display info from default models
		var found bool
		for _, dm := range claude.DefaultModels {
			if dm.ID == requestedModel {
				models = append(models, dm)
				found = true
				break
			}
		}
		// If not found in defaults, create a basic entry
		if !found {
			models = append(models, claude.Model{
				ID:          requestedModel,
				Type:        "model",
				DisplayName: requestedModel,
				CreatedAt:   "",
			})
		}
	}

	return models, nil
}

// AntigravityAccountTestModels uses the stored mapping rather than GetModelMapping,
// which supplies defaults and compatibility aliases that the administrator did not configure.
func AntigravityAccountTestModels(rawMapping any) []antigravity.ClaudeModel {
	var mappedIDs []string
	switch mapping := rawMapping.(type) {
	case map[string]any:
		for id := range mapping {
			if strings.TrimSpace(id) != "" && !strings.Contains(id, "*") {
				mappedIDs = append(mappedIDs, id)
			}
		}
	case map[string]string:
		for id := range mapping {
			if strings.TrimSpace(id) != "" && !strings.Contains(id, "*") {
				mappedIDs = append(mappedIDs, id)
			}
		}
	}
	if len(mappedIDs) == 0 {
		return antigravity.DefaultModels()
	}

	sort.Strings(mappedIDs)
	defaultByID := make(map[string]antigravity.ClaudeModel)
	for _, model := range antigravity.DefaultModels() {
		defaultByID[model.ID] = model
	}
	models := make([]antigravity.ClaudeModel, 0, len(mappedIDs))
	for _, id := range mappedIDs {
		if model, ok := defaultByID[id]; ok {
			models = append(models, model)
			continue
		}
		models = append(models, antigravity.ClaudeModel{
			ID:          id,
			Type:        "model",
			DisplayName: id,
		})
	}
	return models
}

// ModelIntersection validates current accounts even on a cache hit, so deletion
// and a platform change cannot leave a stale group eligible for testing.
func (s *AccountModelCatalogService) ModelIntersection(ctx context.Context, requestedIDs []int64) (*AccountModelIntersection, error) {
	ids, err := normalizeAccountBatchTestIDs(requestedIDs, "account_ids", 100)
	if err != nil {
		return nil, infraerrors.BadRequest("INVALID_MODEL_INTERSECTION_ACCOUNTS", err.Error())
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.accountRepo == nil {
		return nil, infraerrors.InternalServer("MODEL_CATALOG_UNAVAILABLE", "Account model catalog is unavailable")
	}
	accounts, err := s.accountRepo.GetByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("load model intersection accounts: %w", err)
	}
	byID := make(map[int64]*Account, len(accounts))
	for _, account := range accounts {
		if account != nil {
			byID[account.ID] = account
		}
	}
	ordered := make([]*Account, 0, len(ids))
	platform := ""
	for i, id := range ids {
		account := byID[id]
		if account == nil {
			return nil, infraerrors.BadRequest("MODEL_INTERSECTION_ACCOUNT_NOT_FOUND", fmt.Sprintf("Account %d not found", id))
		}
		if i == 0 {
			platform = account.Platform
		} else if account.Platform != platform {
			return nil, ErrAccountBatchTestMixedPlatforms
		}
		ordered = append(ordered, account)
	}
	keyIDs := append([]int64(nil), ids...)
	sort.Slice(keyIDs, func(i, j int) bool { return keyIDs[i] < keyIDs[j] })
	var key strings.Builder
	_, _ = key.WriteString(platform)
	for _, id := range keyIDs {
		_ = key.WriteByte(':')
		_, _ = key.WriteString(strconv.FormatInt(id, 10))
	}
	entry, hit := s.cachedIntersection(key.String())
	if !hit {
		entry, err = s.fetchIntersectionCatalogs(ctx, ordered)
		if err != nil {
			return nil, err
		}
		if len(entry.catalogs) == 0 {
			return nil, infraerrors.InternalServer("MODEL_INTERSECTION_FETCH_FAILED", "Failed to load models for all selected accounts")
		}
		s.storeIntersection(key.String(), entry)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return projectModelIntersection(platform, ids, entry), nil
}

func (s *AccountModelCatalogService) cachedIntersection(key string) (accountModelIntersectionCacheEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[key]
	if ok && !s.now().Before(entry.expiresAt) {
		delete(s.cache, key)
		return accountModelIntersectionCacheEntry{}, false
	}
	return entry, ok
}

func (s *AccountModelCatalogService) storeIntersection(key string, entry accountModelIntersectionCacheEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for key, cached := range s.cache {
		if !now.Before(cached.expiresAt) {
			delete(s.cache, key)
		}
	}
	if len(s.cache) >= accountModelIntersectionCacheLimit {
		var oldestKey string
		var oldest time.Time
		for key, cached := range s.cache {
			if oldest.IsZero() || cached.expiresAt.Before(oldest) {
				oldestKey, oldest = key, cached.expiresAt
			}
		}
		delete(s.cache, oldestKey)
	}
	entry.expiresAt = now.Add(accountModelIntersectionTTL)
	s.cache[key] = entry
}

func (s *AccountModelCatalogService) fetchIntersectionCatalogs(ctx context.Context, accounts []*Account) (accountModelIntersectionCacheEntry, error) {
	entry := accountModelIntersectionCacheEntry{
		catalogs: make(map[int64][]AccountCatalogModel), failed: make(map[int64]bool),
	}
	jobs := make(chan *Account, len(accounts))
	for _, account := range accounts {
		jobs <- account
	}
	close(jobs)
	var mu sync.Mutex
	var workers sync.WaitGroup
	for range min(accountModelIntersectionConcurrency, len(accounts)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for account := range jobs {
				if ctx.Err() != nil {
					return
				}
				select {
				case s.fetchSlots <- struct{}{}:
				case <-ctx.Done():
					return
				}
				models, err := s.fetchAccountCatalog(ctx, account)
				<-s.fetchSlots
				mu.Lock()
				if err != nil {
					entry.failed[account.ID] = true
				} else {
					entry.catalogs[account.ID] = models
				}
				mu.Unlock()
			}
		}()
	}
	workers.Wait()
	return entry, ctx.Err()
}

func (s *AccountModelCatalogService) fetchAccountCatalog(ctx context.Context, account *Account) (models []AccountCatalogModel, err error) {
	defer func() {
		if recover() != nil {
			slog.Warn("account model catalog fetch panicked", "account_id", account.ID)
			err = fmt.Errorf("fetch account %d models failed", account.ID)
		}
	}()
	fetch := s.fetchModels
	if fetch == nil {
		fetch = s.GetAvailableModels
	}
	catalog, err := fetch(ctx, account)
	if err != nil {
		return nil, err
	}
	// Project the existing platform-specific JSON once; the single-account API
	// still exposes its full original fields and the exact same fallback catalog.
	payload, err := json.Marshal(catalog)
	if err != nil {
		return nil, fmt.Errorf("encode account catalog: %w", err)
	}
	if err := json.Unmarshal(payload, &models); err != nil {
		return nil, fmt.Errorf("decode account catalog: %w", err)
	}
	return models, nil
}

func projectModelIntersection(platform string, ids []int64, entry accountModelIntersectionCacheEntry) *AccountModelIntersection {
	result := &AccountModelIntersection{
		Platform: platform, AccountCount: len(ids), Models: make([]AccountCatalogModel, 0), FailedAccountIDs: make([]int64, 0),
	}
	counts := make(map[string]int)
	names := make(map[string]string)
	for _, id := range ids {
		if entry.failed[id] {
			result.FailedAccountIDs = append(result.FailedAccountIDs, id)
			continue
		}
		seen := make(map[string]bool)
		for _, model := range entry.catalogs[id] {
			if model.ID == "" || seen[model.ID] {
				continue
			}
			seen[model.ID] = true
			counts[model.ID]++
			if _, exists := names[model.ID]; !exists {
				name := model.DisplayName
				if strings.TrimSpace(name) == "" {
					name = model.ID
				}
				names[model.ID] = name
			}
		}
	}
	for id, count := range counts {
		if count == len(entry.catalogs) {
			result.Models = append(result.Models, AccountCatalogModel{ID: id, DisplayName: names[id]})
		}
	}
	sort.Slice(result.Models, func(i, j int) bool { return result.Models[i].ID < result.Models[j].ID })
	return result
}
