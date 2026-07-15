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

	modelNameIdx := findColumn(header, "model_name")
	effortIdx := findColumn(header, "reasoning_effort")

	if modelNameIdx < 0 {
		return nil, fmt.Errorf("CSV must have 'model_name' column")
	}

	metadataColumns := make(map[int]string)
	for i, col := range header {
		col = strings.TrimSpace(col)
		if i == modelNameIdx || i == effortIdx || col == "" {
			continue
		}
		metadataColumns[i] = col
	}

	if len(metadataColumns) == 0 {
		return nil, fmt.Errorf("CSV must have at least one metadata column")
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
	modelTags := make(map[string]map[string]string) // key: "modelID:effort"

	for {
		row, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("row %d: %v", result.TotalRows+1, err))
			result.TotalRows++
			continue
		}

		modelName := strings.TrimSpace(row[modelNameIdx])

		effort := ""
		if effortIdx >= 0 && effortIdx < len(row) {
			effort = strings.TrimSpace(row[effortIdx])
		}

		modelID, exists := modelMap[strings.ToLower(modelName)]
		if !exists {
			result.Skipped++
			result.TotalRows++
			continue
		}

		tagKey := fmt.Sprintf("%d:%s", modelID, effort)
		if _, ok := modelTags[tagKey]; !ok {
			modelTags[tagKey] = make(map[string]string)
			result.MatchedModels = append(result.MatchedModels, modelName)
		}

		for colIdx, colName := range metadataColumns {
			if colIdx < len(row) {
				value := strings.TrimSpace(row[colIdx])
				if value != "" {
					modelTags[tagKey][colName] = value
				}
			}
		}

		result.TotalRows++
	}

	for tagKey, tags := range modelTags {
		parts := strings.SplitN(tagKey, ":", 2)
		if len(parts) != 2 {
			continue
		}

		var modelID int64
		fmt.Sscanf(parts[0], "%d", &modelID)
		effort := parts[1]

		if mode == ImportModeMerge {
			existing, err := s.tagRepo.GetByModelEffort(ctx, modelID, effort)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("model %d effort %s: %v", modelID, effort, err))
				continue
			}
			for _, t := range existing {
				if _, exists := tags[t.Key]; !exists {
					tags[t.Key] = t.Value
				}
			}
		}

		if err := s.tagRepo.Set(ctx, modelID, effort, tags); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("model %d effort %s: %v", modelID, effort, err))
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
