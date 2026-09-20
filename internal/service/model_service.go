package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
)

type ModelEffortEntry struct {
	ModelID           int64             `json:"model_id"`
	ModelName         string            `json:"model_name"`
	ProviderID        int64             `json:"provider_id"`
	ProviderName      string            `json:"provider_name"`
	ReasoningEffort   string            `json:"reasoning_effort"`
	Disabled          bool              `json:"disabled"`
	DisabledUntil     *time.Time        `json:"disabled_until,omitempty"`
	DisabledReason    string            `json:"disabled_reason,omitempty"`
	RateLimitIsolated bool              `json:"rate_limit_isolated"`
	Tags              map[string]string `json:"tags"`
	GlobalMetadata    map[string]string `json:"global_metadata"`
	MappingTargetName *string           `json:"mapping_target_name,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
}

type DiscoverResult struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
}

type ModelService interface {
	Discover(ctx context.Context, providerID int64) (*DiscoverResult, error)
	ListByProvider(ctx context.Context, providerID int64) ([]models.Model, error)
	ListAll(ctx context.Context) ([]ModelEffortEntry, error)
	GetByID(ctx context.Context, id int64) (*models.Model, error)
	SetTags(ctx context.Context, modelID int64, tags map[string]string) error
	GetTags(ctx context.Context, modelID int64) ([]models.Tag, error)
	Delete(ctx context.Context, id int64) error
	ToggleDisabled(ctx context.Context, id int64, disabled bool, duration *time.Duration, reason string) error
	SetRateLimitIsolated(ctx context.Context, id int64, isolated bool) error
	DeleteStaleDisabled(ctx context.Context) (int64, error)
	SetOverrideRepo(repo repository.ModelOverrideRepository)
	// MirrorGlobalMetadata copies global metadata (m.*) for modelName into
	// model_tags (mc.*) on every provider row carrying that model name — the
	// same bridge Discover uses. Filters and sorts evaluate mc.* attributes,
	// so manually entered global metadata only becomes filterable after this
	// sync. Best-effort: errors are logged, not returned.
	MirrorGlobalMetadata(ctx context.Context, modelName string)
	// MirrorOverridesToGlobal rewrites the global metadata layer (m.*) for
	// modelName from the effective override state: for every (model, effort)
	// row carrying that name, per-key override values replace m.* values.
	// clearedKeys lists override keys deleted in the triggering write; they
	// are removed from the m.* layer as well (they'd otherwise resurface as
	// stale values once the override row is gone). Pass nil when nothing was
	// deleted. Best-effort: errors are logged, not returned.
	MirrorOverridesToGlobal(ctx context.Context, modelName string, clearedKeys map[string]bool)
}

type modelService struct {
	modelRepo      repository.ModelRepository
	tagRepo        repository.TagRepository
	providerRepo   repository.ProviderRepository
	provService    ProviderService
	globalMetaRepo repository.GlobalMetadataRepository
	mappingRepo    repository.ModelMappingRepository
	// overrideRepo is optional (nil in tests): needed by
	// MirrorOverridesToGlobal to read the effective override state.
	overrideRepo repository.ModelOverrideRepository

	// Codex discovery collaborators (§4.7), wired post-construction via
	// SetCodexDiscovery because the OAuth service is created after this one.
	// All optional: any missing collaborator downgrades codex discovery to the
	// static seed list (CodexFallbackModels).
	codexTokens oauthTokenSource
	codexRows   oauthRowSource
	codexLister CodexModelLister
}

func NewModelService(
	modelRepo repository.ModelRepository,
	tagRepo repository.TagRepository,
	providerRepo repository.ProviderRepository,
	provService ProviderService,
	globalMetaRepo repository.GlobalMetadataRepository,
	mappingRepo repository.ModelMappingRepository,
) ModelService {
	return &modelService{
		modelRepo:      modelRepo,
		tagRepo:        tagRepo,
		providerRepo:   providerRepo,
		provService:    provService,
		globalMetaRepo: globalMetaRepo,
		mappingRepo:    mappingRepo,
	}
}

// SetOverrideRepo wires the optional override repository used by
// MirrorOverridesToGlobal (avoiding a constructor signature change).
func (s *modelService) SetOverrideRepo(repo repository.ModelOverrideRepository) {
	s.overrideRepo = repo
}

type openAIModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// discoveredModel is one model returned by discovery: its name plus optional
// metadata tags to import alongside it (codex catalog extras; empty for the
// plain OpenAI-compatible/ollama flows).
type discoveredModel struct {
	name string
	tags map[string]string
}

func (s *modelService) Discover(ctx context.Context, providerID int64) (*DiscoverResult, error) {
	provider, err := s.providerRepo.GetByID(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, fmt.Errorf("provider not found: %d", providerID)
	}

	var apiKey string
	if provider.APIType == models.APITypeCodex {
		// Codex authenticates via ChatGPT OAuth (resolved inside the codex
		// discovery branch); no API key is needed or used.
		apiKey = ""
	} else if provider.APIKeyEncrypted == "oauth" {
		apiKey = "oauth"
	} else {
		apiKey, err = s.provService.DecryptAPIKey(provider.APIKeyEncrypted)
		if err != nil {
			return nil, fmt.Errorf("decrypt api key: %w", err)
		}
	}

	existingModels, err := s.modelRepo.ListByProvider(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("list existing models: %w", err)
	}
	existingNames := make(map[string]bool, len(existingModels))
	for _, m := range existingModels {
		existingNames[m.Name] = true
	}

	discovered, err := s.fetchDiscoveredModels(ctx, provider, apiKey)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}

	result := &DiscoverResult{}
	fetchedNames := make(map[string]bool, len(discovered))

	for _, dm := range discovered {
		name := dm.name
		fetchedNames[name] = true
		m, err := s.modelRepo.Upsert(ctx, providerID, name)
		if err != nil {
			return nil, fmt.Errorf("upsert model %s: %w", name, err)
		}

		if !existingNames[name] {
			result.Added = append(result.Added, name)
		}

		// Import discovery metadata (codex catalog) as model-level tags. A tag
		// failure is logged and skipped — the model list is still authoritative.
		if len(dm.tags) > 0 && s.tagRepo != nil {
			if err := s.tagRepo.Set(ctx, m.ID, "", dm.tags); err != nil {
				slog.Warn("failed to import discovery metadata", "provider", provider.Name, "model", name, "error", err)
			}
		}

		if s.globalMetaRepo != nil {
			metadata, err := s.globalMetaRepo.GetByModel(ctx, m.Name)
			if err == nil && len(metadata) > 0 {
				for effort, tags := range metadata {
					s.tagRepo.Set(ctx, m.ID, effort, tags)
				}
			}
		}
	}

	for n := range existingNames {
		if !fetchedNames[n] {
			result.Removed = append(result.Removed, n)
		}
	}

	modelNames := make([]string, 0, len(discovered))
	for _, dm := range discovered {
		modelNames = append(modelNames, dm.name)
	}

	_, err = s.modelRepo.DisableByProviderExcept(ctx, providerID, modelNames)
	if err != nil {
		return nil, fmt.Errorf("disable stale models: %w", err)
	}

	return result, nil
}

// fetchDiscoveredModels dispatches per API type. Codex never fails here: its
// branch falls back to the static seed list internally (§4.7).
func (s *modelService) fetchDiscoveredModels(ctx context.Context, provider *models.Provider, apiKey string) ([]discoveredModel, error) {
	if provider.APIType == models.APITypeCodex {
		return s.fetchCodexModels(ctx, provider), nil
	}

	names, err := fetchModels(provider.BaseURL, apiKey, provider.APIType)
	if err != nil {
		return nil, err
	}

	out := make([]discoveredModel, len(names))
	for i, name := range names {
		out[i] = discoveredModel{name: name}
	}
	return out, nil
}

func (s *modelService) ToggleDisabled(ctx context.Context, id int64, disabled bool, duration *time.Duration, reason string) error {
	return s.modelRepo.ToggleDisabled(ctx, id, disabled, duration, reason)
}

func (s *modelService) SetRateLimitIsolated(ctx context.Context, id int64, isolated bool) error {
	return s.modelRepo.SetRateLimitIsolated(ctx, id, isolated)
}

func (s *modelService) DeleteStaleDisabled(ctx context.Context) (int64, error) {
	return s.modelRepo.DeleteStaleDisabled(ctx)
}

// MirrorGlobalMetadata copies global metadata (m.*) for modelName into
// model_tags (mc.*) for every enabled model row carrying that name — the same
// bridge the Discover flow applies (Discover's globalMetaRepo → tagRepo copy).
// Filters and sorts evaluate mc.* attributes, so metadata written directly to
// model_metadata_global (UI "Add Model", PUT /model-metadata) only becomes
// filterable after this sync.
//
// Efforts are taken from the metadata rows themselves; existing tags for those
// efforts are merged (global metadata wins) so nothing is lost. Best-effort:
// any repo error is logged and skipped.
func (s *modelService) MirrorGlobalMetadata(ctx context.Context, modelName string) {
	if s == nil || s.globalMetaRepo == nil || s.tagRepo == nil || modelName == "" {
		return
	}

	// All per-effort metadata rows for this model name.
	metadata, err := s.globalMetaRepo.GetByModel(ctx, modelName)
	if err != nil || len(metadata) == 0 {
		if err != nil {
			slog.Warn("metadata mirror: load failed", "model", modelName, "error", err)
		}
		return
	}

	// Every model row with this name (a model may exist on several providers).
	allModels, err := s.modelRepo.ListAll(ctx)
	if err != nil {
		slog.Warn("metadata mirror: list models failed", "model", modelName, "error", err)
		return
	}

	for _, m := range allModels {
		if m.Name != modelName {
			continue
		}
		for effort, tags := range metadata {
			if len(tags) == 0 {
				continue
			}
			// Merge over existing tags for this effort, global metadata wins.
			merged := make(map[string]string, len(tags))
			existing, err := s.tagRepo.GetByModelEffort(ctx, m.ID, effort)
			if err != nil {
				slog.Warn("metadata mirror: load tags failed", "model", modelName, "error", err)
				continue
			}
			for _, t := range existing {
				merged[t.Key] = t.Value
			}
			for k, v := range tags {
				merged[k] = v
			}
			if err := s.tagRepo.Set(ctx, m.ID, effort, merged); err != nil {
				slog.Warn("metadata mirror: write tags failed", "model", modelName, "effort", effort, "error", err)
			}
		}
	}
}

// MirrorOverridesToGlobal rewrites the global metadata layer (m.*) for
// modelName from the effective override state (see ModelService docs).
// Layering per (model, effort): base = m.* metadata, then per-key overrides
// applied on top; keys in clearedKeys are dropped from m.* entirely.
// Best-effort: errors are logged, not returned.
func (s *modelService) MirrorOverridesToGlobal(ctx context.Context, modelName string, clearedKeys map[string]bool) {
	if s == nil || s.globalMetaRepo == nil || modelName == "" {
		return
	}
	if s.overrideRepo == nil {
		return
	}

	allModels, err := s.modelRepo.ListAll(ctx)
	if err != nil {
		slog.Warn("override mirror: list models failed", "model", modelName, "error", err)
		return
	}

	for _, m := range allModels {
		if m.Name != modelName {
			continue
		}

		// Efforts known for this model: from m.* rows plus override rows.
		gmByEffort, err := s.globalMetaRepo.GetByModel(ctx, modelName)
		if err != nil {
			slog.Warn("override mirror: load metadata failed", "model", modelName, "error", err)
			return
		}
		efforts := make(map[string]bool, len(gmByEffort)+1)
		for effort := range gmByEffort {
			efforts[effort] = true
		}
		if overrideEfforts, err := s.overrideRepo.GetEffortsByModel(ctx, m.ID); err == nil {
			for _, e := range overrideEfforts {
				efforts[e] = true
			}
		}

		for effort := range efforts {
			overrides, err := s.overrideRepo.GetByModelAndEffort(ctx, m.ID, effort)
			if err != nil {
				slog.Warn("override mirror: load overrides failed", "model", modelName, "error", err)
				continue
			}

			// Effective m.* = current m.* with overrides applied per key.
			merged := make(map[string]string, len(gmByEffort[effort])+len(overrides))
			for k, v := range gmByEffort[effort] {
				merged[k] = v
			}
			// Keys cleared in the triggering write are removed from m.* —
			// their override row is gone, so nothing shadows them anymore.
			for key := range clearedKeys {
				delete(merged, key)
			}
			// Remaining overrides apply on top.
			for _, o := range overrides {
				if o.Value == "" {
					delete(merged, o.Key)
					continue
				}
				merged[o.Key] = o.Value
			}

			if err := s.globalMetaRepo.Set(ctx, modelName, effort, merged); err != nil {
				slog.Warn("override mirror: write metadata failed", "model", modelName, "effort", effort, "error", err)
			}
		}
	}

	// Keep mc.* tags consistent with the new m.* values.
	s.MirrorGlobalMetadata(ctx, modelName)
}

func fetchModels(baseURL, apiKey string, apiType models.APIType) ([]string, error) {
	if apiType == models.APITypeOllama || apiType == models.APITypeOllamaCloud {
		// Ollama (local + cloud): discover via /api/tags
		host := strings.TrimSuffix(baseURL, "/v1")
		host = strings.TrimSuffix(host, "/api/chat")
		host = strings.TrimSuffix(host, "/")
		url := host + "/api/tags"

		client := &http.Client{Timeout: 10 * time.Second}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request ollama tags: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("ollama tags endpoint returned %d", resp.StatusCode)
		}

		var tagsResponse struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tagsResponse); err != nil {
			return nil, fmt.Errorf("decode ollama tags response: %w", err)
		}

		var names []string
		for _, m := range tagsResponse.Models {
			names = append(names, m.Name)
		}
		return names, nil
	}

	// OpenAI-compatible (openai, anthropic, cloudflare, nvidia-nim)
	cleanBase := strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
	url := cleanBase + "/v1/models"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models endpoint returned %d", resp.StatusCode)
	}

	var result openAIModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}

	var names []string
	for _, m := range result.Data {
		names = append(names, m.ID)
	}
	return names, nil
}

func (s *modelService) ListAll(ctx context.Context) ([]ModelEffortEntry, error) {
	allModels, err := s.modelRepo.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	// Batch-fetch all mappings
	var mappingMap map[int64]*repository.ModelMapping
	if s.mappingRepo != nil {
		allMappings, _ := s.mappingRepo.GetAll(ctx)
		mappingMap = make(map[int64]*repository.ModelMapping, len(allMappings))
		for i := range allMappings {
			mappingMap[allMappings[i].SourceModelID] = &allMappings[i]
		}
	}

	var result []ModelEffortEntry
	for _, m := range allModels {
		provider, err := s.providerRepo.GetByID(ctx, m.ProviderID)
		if err != nil {
			return nil, err
		}
		providerName := ""
		if provider != nil {
			providerName = provider.Name
		}

		// Determine mapping
		var mappingTarget *string
		if mappingMap != nil {
			if mapping, ok := mappingMap[m.ID]; ok {
				name := mapping.TargetModelName
				mappingTarget = &name
			}
		}

		if mappingTarget != nil {
			// Mapped path: get target's tags + per-effort global metadata
			targetName := *mappingTarget

			// Find target model by name to load its tags
			var targetModelTags map[string]string
			for i := range allModels {
				if allModels[i].Name == targetName {
					tags, err := s.tagRepo.GetByModel(ctx, allModels[i].ID)
					if err == nil && len(tags) > 0 {
						targetModelTags = make(map[string]string)
						for _, t := range tags {
							targetModelTags[t.Key] = t.Value
						}
					}
					break
				}
			}
			if targetModelTags == nil {
				targetModelTags = map[string]string{}
			}

			targetMeta, err := s.globalMetaRepo.GetByModel(ctx, targetName)
			if err != nil {
				return nil, err
			}

			efforts := make([]string, 0, len(targetMeta))
			for effort := range targetMeta {
				efforts = append(efforts, effort)
			}
			// Deterministic order: map iteration is randomized, and callers
			// (and tests) expect "" (base) before named efforts.
			sort.Strings(efforts)
			if len(efforts) == 0 {
				efforts = []string{""}
			}

			for _, effort := range efforts {
				gm := map[string]string{}
				if targetMeta != nil {
					if data, ok := targetMeta[effort]; ok {
						for k, v := range data {
							gm[k] = v
						}
					}
				}
				result = append(result, ModelEffortEntry{
					ModelID:           m.ID,
					ModelName:         m.Name,
					ProviderID:        m.ProviderID,
					ProviderName:      providerName,
					ReasoningEffort:   effort,
					Disabled:          m.Disabled,
					DisabledUntil:     m.DisabledUntil,
					DisabledReason:    m.DisabledReason,
					RateLimitIsolated: m.RateLimitIsolated,
					Tags:              targetModelTags,
					GlobalMetadata:    gm,
					MappingTargetName: mappingTarget,
					CreatedAt:         m.CreatedAt,
				})
			}
		} else {
			// Not-mapped path: get per-effort tags + global metadata
			efforts, err := s.tagRepo.GetAvailableEfforts(ctx, m.ID)
			if err != nil {
				return nil, err
			}
			if len(efforts) == 0 {
				efforts = []string{""}
			}

			for _, effort := range efforts {
				tagMap := map[string]string{}
				tags, err := s.tagRepo.GetByModelEffort(ctx, m.ID, effort)
				if err != nil {
					return nil, err
				}
				for _, t := range tags {
					tagMap[t.Key] = t.Value
				}

				gm := map[string]string{}
				if s.globalMetaRepo != nil {
					gmData, err := s.globalMetaRepo.GetByModelEffort(ctx, m.Name, effort)
					if err == nil {
						for k, v := range gmData {
							gm[k] = v
						}
					}
				}

				result = append(result, ModelEffortEntry{
					ModelID:           m.ID,
					ModelName:         m.Name,
					ProviderID:        m.ProviderID,
					ProviderName:      providerName,
					ReasoningEffort:   effort,
					Disabled:          m.Disabled,
					DisabledUntil:     m.DisabledUntil,
					DisabledReason:    m.DisabledReason,
					RateLimitIsolated: m.RateLimitIsolated,
					Tags:              tagMap,
					GlobalMetadata:    gm,
					CreatedAt:         m.CreatedAt,
				})
			}
		}
	}

	return result, nil
}

func (s *modelService) ListByProvider(ctx context.Context, providerID int64) ([]models.Model, error) {
	models, err := s.modelRepo.ListByProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}

	for i := range models {
		tags, err := s.tagRepo.GetByModel(ctx, models[i].ID)
		if err != nil {
			return nil, err
		}
		models[i].Tags = tags
	}

	return models, nil
}

func (s *modelService) GetByID(ctx context.Context, id int64) (*models.Model, error) {
	m, err := s.modelRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, nil
	}

	tags, err := s.tagRepo.GetByModel(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	m.Tags = tags

	return m, nil
}

func (s *modelService) SetTags(ctx context.Context, modelID int64, tags map[string]string) error {
	m, err := s.modelRepo.GetByID(ctx, modelID)
	if err != nil {
		return err
	}
	if m == nil {
		return fmt.Errorf("model not found: %d", modelID)
	}

	return s.tagRepo.Set(ctx, modelID, "default", tags)
}

func (s *modelService) GetTags(ctx context.Context, modelID int64) ([]models.Tag, error) {
	return s.tagRepo.GetByModel(ctx, modelID)
}

func (s *modelService) Delete(ctx context.Context, id int64) error {
	return s.modelRepo.Delete(ctx, id)
}
