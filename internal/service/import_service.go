package service

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"github.com/chris/llm-router/internal/repository"
)

type ImportMode string

const (
	ImportModeReplace ImportMode = "replace"
	ImportModeMerge   ImportMode = "merge"
)

type ImportResult struct {
	TotalRows   int      `json:"total_rows"`
	Imported    int      `json:"imported"`
	Skipped     int      `json:"skipped"`
	Errors      []string `json:"errors,omitempty"`
	MatchedModels []string `json:"matched_models,omitempty"`
}

type ImportService interface {
	ImportCSV(ctx context.Context, reader io.Reader, mode ImportMode) (*ImportResult, error)
}

type importService struct {
	modelRepo repository.ModelRepository
	tagRepo   repository.TagRepository
}

func NewImportService(modelRepo repository.ModelRepository, tagRepo repository.TagRepository) ImportService {
	return &importService{
		modelRepo: modelRepo,
		tagRepo:   tagRepo,
	}
}

func (s *importService) ImportCSV(ctx context.Context, reader io.Reader, mode ImportMode) (*ImportResult, error) {
	csvReader := csv.NewReader(reader)

	header, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("read CSV header: %w", err)
	}

	if len(header) < 3 {
		return nil, fmt.Errorf("CSV must have at least 3 columns: model_name, key, value")
	}

	modelNameIdx := findColumn(header, "model_name")
	keyIdx := findColumn(header, "key")
	valueIdx := findColumn(header, "value")

	if modelNameIdx < 0 || keyIdx < 0 || valueIdx < 0 {
		return nil, fmt.Errorf("CSV must have columns: model_name, key, value")
	}

	allModels, err := s.modelRepo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}

	modelMap := make(map[string]int64)
	for _, m := range allModels {
		modelMap[strings.ToLower(m.Name)] = m.ID
	}

	result := &ImportResult{}
	modelTags := make(map[int64]map[string]string)

	for {
		row, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("row %d: %v", result.TotalRows+1, err))
			result.TotalRows++
			result.Errors = append(result.Errors, fmt.Sprintf("row %d: parse error: %v", result.TotalRows, err))
			continue
		}

		if len(row) <= max(modelNameIdx, max(keyIdx, valueIdx)) {
			result.Errors = append(result.Errors, fmt.Sprintf("row %d: not enough columns", result.TotalRows+1))
			result.TotalRows++
			continue
		}

		modelName := strings.TrimSpace(row[modelNameIdx])
		key := strings.TrimSpace(row[keyIdx])
		value := strings.TrimSpace(row[valueIdx])

		modelID, exists := modelMap[strings.ToLower(modelName)]
		if !exists {
			result.Skipped++
			result.TotalRows++
			continue
		}

		if _, ok := modelTags[modelID]; !ok {
			modelTags[modelID] = make(map[string]string)
			result.MatchedModels = append(result.MatchedModels, modelName)
		}

		modelTags[modelID][key] = value
		result.TotalRows++
	}

	for modelID, tags := range modelTags {
		if mode == ImportModeMerge {
			existing, err := s.tagRepo.GetByModel(ctx, modelID)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("model %d: %v", modelID, err))
				continue
			}
			for _, t := range existing {
				if _, exists := tags[t.Key]; !exists {
					tags[t.Key] = t.Value
				}
			}
		}

		if err := s.tagRepo.Set(ctx, modelID, tags); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("model %d: %v", modelID, err))
			continue
		}
		result.Imported++
	}

	return result, nil
}

func findColumn(header []string, name string) int {
	for i, h := range header {
		if strings.EqualFold(strings.TrimSpace(h), name) {
			return i
		}
	}
	return -1
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
