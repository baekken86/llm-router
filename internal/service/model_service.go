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

type ModelWithProvider struct {
	models.Model
	ProviderName      string  `json:"provider_name"`
	MappingTargetID   *int64  `json:"mapping_target_id,omitempty"`
	MappingTargetName *string `json:"mapping_target_name,omitempty"`
}

type ModelService interface {
	Discover(ctx context.Context, providerID int64) ([]models.Model, error)
	ListByProvider(ctx context.Context, providerID int64) ([]models.Model, error)
	ListAll(ctx context.Context) ([]ModelWithProvider, error)
	GetByID(ctx context.Context, id int64) (*models.Model, error)
	SetTags(ctx context.Context, modelID int64, tags map[string]string) error
	GetTags(ctx context.Context, modelID int64) ([]models.Tag, error)
	Delete(ctx context.Context, id int64) error
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

func fetchModels(baseURL, apiKey string, apiType models.APIType) ([]string, error) {
	if apiType == models.APITypeOllama {
		// Ollama: discover via /api/tags (not /v1/models)
		ollamaHost := strings.TrimSuffix(baseURL, "/v1")
		ollamaHost = strings.TrimSuffix(ollamaHost, "/")
		url := ollamaHost + "/api/tags"

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get(url)
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

	// OpenAI-compatible (openai, anthropic, cloudflare)
	url := baseURL + "/v1/models"

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

func (s *modelService) ListAll(ctx context.Context) ([]ModelWithProvider, error) {
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

	var result []ModelWithProvider
	for _, m := range allModels {
		tags, err := s.tagRepo.GetByModel(ctx, m.ID)
		if err != nil {
			return nil, err
		}
		m.Tags = tags

		provider, err := s.providerRepo.GetByID(ctx, m.ProviderID)
		if err != nil {
			return nil, err
		}
		providerName := ""
		if provider != nil {
			providerName = provider.Name
		}

		mp := ModelWithProvider{
			Model:        m,
			ProviderName: providerName,
		}

		// Populate mapping info
		if mappingMap != nil {
			if mapping, ok := mappingMap[m.ID]; ok {
				mp.MappingTargetID = &mapping.TargetModelID
				// Fetch target model name
				targetModel, _ := s.modelRepo.GetByID(ctx, mapping.TargetModelID)
				if targetModel != nil {
					name := targetModel.Name
					mp.MappingTargetName = &name
				}
			}
		}

		result = append(result, mp)
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
