package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
)

type KeyService interface {
	Create(ctx context.Context, req models.CreateKeyRequest) (*models.CreateKeyResponse, error)
	GetByID(ctx context.Context, id int64) (*models.ProxyKey, error)
	List(ctx context.Context) ([]models.ProxyKey, error)
	Delete(ctx context.Context, id int64) error
	ValidateKey(ctx context.Context, key string) (*models.ProxyKey, error)
}

type keyService struct {
	repo repository.ProxyKeyRepository
}

func NewKeyService(repo repository.ProxyKeyRepository) KeyService {
	return &keyService{repo: repo}
}

func (s *keyService) Create(ctx context.Context, req models.CreateKeyRequest) (*models.CreateKeyResponse, error) {
	key, err := generateKey()
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	hash := hashKey(key)

	pk := &models.ProxyKey{
		KeyHash:     hash,
		Description: req.Description,
	}

	if err := s.repo.Create(ctx, pk); err != nil {
		return nil, err
	}

	return &models.CreateKeyResponse{
		ID:          pk.ID,
		Key:         key,
		Description: pk.Description,
		CreatedAt:   pk.CreatedAt,
	}, nil
}

func (s *keyService) GetByID(ctx context.Context, id int64) (*models.ProxyKey, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *keyService) List(ctx context.Context) ([]models.ProxyKey, error) {
	return s.repo.List(ctx)
}

func (s *keyService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

func (s *keyService) ValidateKey(ctx context.Context, key string) (*models.ProxyKey, error) {
	hash := hashKey(key)
	return s.repo.GetByHash(ctx, hash)
}

func generateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "lmr_" + hex.EncodeToString(b), nil
}

func hashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
