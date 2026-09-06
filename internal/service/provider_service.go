package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
)

type ProviderService interface {
	Create(ctx context.Context, req models.CreateProviderRequest) (*models.Provider, error)
	GetByID(ctx context.Context, id int64) (*models.Provider, error)
	List(ctx context.Context) ([]models.Provider, error)
	ListByMetadata(ctx context.Context, filters map[string]string) ([]models.Provider, error)
	Update(ctx context.Context, id int64, req models.UpdateProviderRequest) (*models.Provider, error)
	Delete(ctx context.Context, id int64) error
	DecryptAPIKey(encrypted string) (string, error)
}

type providerService struct {
	repo       repository.ProviderRepository
	metadataRepo repository.ProviderMetadataRepository
	encryptKey []byte
}

func NewProviderService(repo repository.ProviderRepository, metadataRepo repository.ProviderMetadataRepository, encryptKey []byte) ProviderService {
	return &providerService{
		repo:         repo,
		metadataRepo: metadataRepo,
		encryptKey:   encryptKey,
	}
}

func (s *providerService) Create(ctx context.Context, req models.CreateProviderRequest) (*models.Provider, error) {
	encrypted, err := s.encrypt(req.APIKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt api key: %w", err)
	}

	// Provider key defaults to the provider name and must not collide with
	// the "virtual/" VM namespace or contain "/" (model addressing separator).
	providerKey := req.ProviderKey
	if providerKey == "" {
		providerKey = req.Name
	}
	if err := validateProviderKey(providerKey); err != nil {
		return nil, err
	}
	existing, err := s.repo.GetByKey(ctx, providerKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("provider_key %q already in use", providerKey)
	}

	p := &models.Provider{
		Name:            req.Name,
		APIType:         req.APIType,
		BaseURL:         req.BaseURL,
		APIKeyEncrypted: encrypted,
		AccountID:       req.AccountID,
		ProviderKey:     providerKey,
	}

	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}

	if len(req.Metadata) > 0 {
		if err := s.metadataRepo.Set(ctx, p.ID, req.Metadata); err != nil {
			return nil, fmt.Errorf("set metadata: %w", err)
		}
		p.Metadata, _ = s.metadataRepo.GetByProvider(ctx, p.ID)
	}

	return p, nil
}

func (s *providerService) GetByID(ctx context.Context, id int64) (*models.Provider, error) {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil || p == nil {
		return p, err
	}
	p.Metadata, _ = s.metadataRepo.GetByProvider(ctx, p.ID)
	return p, nil
}

func (s *providerService) List(ctx context.Context) ([]models.Provider, error) {
	providers, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	return s.attachMetadata(ctx, providers)
}

func (s *providerService) ListByMetadata(ctx context.Context, filters map[string]string) ([]models.Provider, error) {
	providers, err := s.repo.ListByMetadata(ctx, filters)
	if err != nil {
		return nil, err
	}
	return s.attachMetadata(ctx, providers)
}

func (s *providerService) attachMetadata(ctx context.Context, providers []models.Provider) ([]models.Provider, error) {
	if len(providers) == 0 {
		return providers, nil
	}
	ids := make([]int64, len(providers))
	for i, p := range providers {
		ids[i] = p.ID
	}
	allMeta, err := s.metadataRepo.GetByProviders(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range providers {
		providers[i].Metadata = allMeta[providers[i].ID]
	}
	return providers, nil
}

func (s *providerService) Update(ctx context.Context, id int64, req models.UpdateProviderRequest) (*models.Provider, error) {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("provider not found: %d", id)
	}

	if req.Name != nil {
		p.Name = *req.Name
	}
	if req.ProviderKey != nil {
		// Empty string means "reset to the provider name".
		newKey := *req.ProviderKey
		if newKey == "" {
			newKey = p.Name
		}
		if err := validateProviderKey(newKey); err != nil {
			return nil, err
		}
		existing, err := s.repo.GetByKey(ctx, newKey)
		if err != nil {
			return nil, err
		}
		if existing != nil && existing.ID != id {
			return nil, fmt.Errorf("provider_key %q already in use", newKey)
		}
		p.ProviderKey = newKey
	}
	if req.APIType != nil {
		p.APIType = *req.APIType
	}
	if req.BaseURL != nil {
		p.BaseURL = *req.BaseURL
	}
	if req.APIKey != nil {
		encrypted, err := s.encrypt(*req.APIKey)
		if err != nil {
			return nil, fmt.Errorf("encrypt api key: %w", err)
		}
		p.APIKeyEncrypted = encrypted
	}
	if req.Disabled != nil {
		p.Disabled = *req.Disabled
		if *req.Disabled && req.Duration != nil {
			dur, err := models.ParseDuration(*req.Duration)
			if err != nil {
				return nil, fmt.Errorf("invalid duration: %w", err)
			}
			if dur > 0 {
				t := time.Now().Add(dur)
				p.DisabledUntil = &t
			} else {
				p.DisabledUntil = nil
			}
		} else if !*req.Disabled {
			p.DisabledUntil = nil
		}
	}

	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}

	if req.Metadata != nil {
		if err := s.metadataRepo.Set(ctx, p.ID, *req.Metadata); err != nil {
			return nil, fmt.Errorf("set metadata: %w", err)
		}
	}

	p.Metadata, _ = s.metadataRepo.GetByProvider(ctx, p.ID)
	return p, nil
}

func (s *providerService) Delete(ctx context.Context, id int64) error {
	if err := s.metadataRepo.DeleteByProvider(ctx, id); err != nil {
		return fmt.Errorf("delete metadata: %w", err)
	}
	return s.repo.Delete(ctx, id)
}

// validateProviderKey enforces the model-addressing namespace rules: a key
// must be non-empty, must not contain "/" (the provider/model separator), and
// must not be "virtual" (reserved for the virtual-model namespace).
func validateProviderKey(key string) error {
	if key == "" {
		return fmt.Errorf("provider_key must not be empty")
	}
	if strings.Contains(key, "/") {
		return fmt.Errorf("provider_key %q must not contain '/'", key)
	}
	if key == "virtual" {
		return fmt.Errorf("provider_key %q is reserved", key)
	}
	return nil
}

func (s *providerService) DecryptAPIKey(encrypted string) (string, error) {
	return s.decrypt(encrypted)
}

func (s *providerService) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.encryptKey)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (s *providerService) decrypt(encoded string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}

	block, err := aes.NewCipher(s.encryptKey)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := aesGCM.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}
