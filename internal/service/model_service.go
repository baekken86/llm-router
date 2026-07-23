package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	Tags              map[string]string `json:"tags"`
	GlobalMetadata    map[string]string `json:"global_metadata"`
	MappingTargetName *string           `json:"mapping_target_name,omitempty"`
}

type ModelService interface {
	Discover(ctx context.Context, providerID int64) ([]models.Model, error)
	ListByProvider(ctx context.Context, providerID int64) ([]models.Model, error)
	ListAll(ctx context.Context) ([]ModelEffortEntry, error)
	GetByID(ctx context.Context, id int64) (*models.Model, error)
	SetTags(ctx context.Context, modelID int64, tags map[string]string) error
	GetTags(ctx context.Context, modelID int64) ([]models.Tag, error)
	Delete(ctx context.Context, id int64) error
	ToggleDisabled(ctx context.Context, id int64, disabled bool) error
}

type modelService struct {
	modelRepo      repository.ModelRepository
	tagRepo        repository.TagRepository
	providerRepo   repository.ProviderRepository
	provService    ProviderService
	globalMetaRepo repository.GlobalMetadataRepository
	mappingRepo    repository.ModelMappingRepository
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

type openAIModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func (s *modelService) Discover(ctx context.Context, providerID int64) ([]models.Model, error) {
	provider, err := s.providerRepo.GetByID(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, fmt.Errorf("provider not found: %d", providerID)
	}

	var apiKey string
	if provider.APIKeyEncrypted == "oauth" {
		apiKey = "oauth"
	} else {
		apiKey, err = s.provService.DecryptAPIKey(provider.APIKeyEncrypted)
		if err != nil {
			return nil, fmt.Errorf("decrypt api key: %w", err)
		}
	}

	modelNames, err := fetchModels(provider.BaseURL, apiKey, provider.APIType)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}

	var discovered []models.Model
	for _, name := range modelNames {
		m, err := s.modelRepo.Upsert(ctx, providerID, name)
		if err != nil {
			return nil, fmt.Errorf("upsert model %s: %w", name, err)
		}

		if s.globalMetaRepo != nil {
			metadata, err := s.globalMetaRepo.GetByModel(ctx, m.Name)
			if err == nil && len(metadata) > 0 {
				for effort, tags := range metadata {
					s.tagRepo.Set(ctx, m.ID, effort, tags)
				}
			}
		}

		discovered = append(discovered, *m)
	}

	return discovered, nil
}

func (s *modelService) ToggleDisabled(ctx context.Context, id int64, disabled bool) error {
	return s.modelRepo.ToggleDisabled(ctx, id, disabled)
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
					Tags:              targetModelTags,
					GlobalMetadata:    gm,
					MappingTargetName: mappingTarget,
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
					ModelID:         m.ID,
					ModelName:       m.Name,
					ProviderID:      m.ProviderID,
					ProviderName:    providerName,
					ReasoningEffort: effort,
					Disabled:        m.Disabled,
					Tags:            tagMap,
					GlobalMetadata:  gm,
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
