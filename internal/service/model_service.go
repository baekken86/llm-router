package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
)

type ModelWithProvider struct {
	models.Model
	ProviderName string `json:"provider_name"`
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
}

func NewModelService(
	modelRepo repository.ModelRepository,
	tagRepo repository.TagRepository,
	providerRepo repository.ProviderRepository,
	provService ProviderService,
	globalMetaRepo repository.GlobalMetadataRepository,
) ModelService {
	return &modelService{
		modelRepo:      modelRepo,
		tagRepo:        tagRepo,
		providerRepo:   providerRepo,
		provService:    provService,
		globalMetaRepo: globalMetaRepo,
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

		result = append(result, ModelWithProvider{
			Model:        m,
			ProviderName: providerName,
		})
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
