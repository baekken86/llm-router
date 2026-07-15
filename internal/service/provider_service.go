package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
)

type ProviderService interface {
	Create(ctx context.Context, req models.CreateProviderRequest) (*models.Provider, error)
	GetByID(ctx context.Context, id int64) (*models.Provider, error)
	List(ctx context.Context) ([]models.Provider, error)
	Update(ctx context.Context, id int64, req models.UpdateProviderRequest) (*models.Provider, error)
	Delete(ctx context.Context, id int64) error
	DecryptAPIKey(encrypted string) (string, error)
}

type providerService struct {
	repo      repository.ProviderRepository
	encryptKey []byte
}

func NewProviderService(repo repository.ProviderRepository, encryptKey []byte) ProviderService {
	return &providerService{
		repo:       repo,
		encryptKey: encryptKey,
	}
}

func (s *providerService) Create(ctx context.Context, req models.CreateProviderRequest) (*models.Provider, error) {
	encrypted, err := s.encrypt(req.APIKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt api key: %w", err)
	}

	p := &models.Provider{
		Name:            req.Name,
		APIType:         req.APIType,
		BaseURL:         req.BaseURL,
		APIKeyEncrypted: encrypted,
	}

	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *providerService) GetByID(ctx context.Context, id int64) (*models.Provider, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *providerService) List(ctx context.Context) ([]models.Provider, error) {
	return s.repo.List(ctx)
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

	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *providerService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
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
