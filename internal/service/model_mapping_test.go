package service

import (
	"context"

	"github.com/chris/llm-router/internal/repository"
)

type mockMappingRepo struct {
	mappings map[int64]*repository.ModelMapping
}

func newMockMappingRepo() *mockMappingRepo {
	return &mockMappingRepo{mappings: make(map[int64]*repository.ModelMapping)}
}

func (r *mockMappingRepo) Set(ctx context.Context, sourceModelID int64, targetModelName string) error {
	r.mappings[sourceModelID] = &repository.ModelMapping{
		SourceModelID:   sourceModelID,
		TargetModelName: targetModelName,
		CreatedAt:       "2026-07-19 00:00:00",
	}
	return nil
}

func (r *mockMappingRepo) Get(ctx context.Context, sourceModelID int64) (*repository.ModelMapping, error) {
	m, ok := r.mappings[sourceModelID]
	if !ok {
		return nil, nil
	}
	return m, nil
}

func (r *mockMappingRepo) GetAll(ctx context.Context) ([]repository.ModelMapping, error) {
	var result []repository.ModelMapping
	for _, m := range r.mappings {
		result = append(result, *m)
	}
	return result, nil
}

func (r *mockMappingRepo) GetAllJoined(ctx context.Context) ([]repository.ModelMappingWithNames, error) {
	return nil, nil
}

func (r *mockMappingRepo) Delete(ctx context.Context, sourceModelID int64) error {
	delete(r.mappings, sourceModelID)
	return nil
}
