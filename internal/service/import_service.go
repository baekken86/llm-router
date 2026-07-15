package service

import (
	"context"
	"encoding/json"
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
	TotalRows     int      `json:"total_rows"`
	Imported      int      `json:"imported"`
	Skipped       int      `json:"skipped"`
	Errors        []string `json:"errors,omitempty"`
	MatchedModels []string `json:"matched_models,omitempty"`
}

type ImportService interface {
	ImportJSON(ctx context.Context, reader io.Reader, mode ImportMode) (*ImportResult, error)
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

type ModelsFile struct {
	Fields map[string]FieldDef `json:"fields"`
	Models []ModelDef          `json:"models"`
}

type FieldDef struct {
	Description string   `json:"description"`
	Type        string   `json:"type"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	Values      []string `json:"values,omitempty"`
}

type ModelDef struct {
	Name               string                      `json:"name"`
	HasReasoningEffort bool                        `json:"has_reasoning_effort"`
	ContextWindow      *int                        `json:"context_window,omitempty"`
	CostType           *string                     `json:"cost_type,omitempty"`
	CostPer1mInput     *float64                    `json:"cost_per_1m_input,omitempty"`
	CostPer1mOutput    *float64                    `json:"cost_per_1m_output,omitempty"`
	Efforts            map[string]map[string]interface{} `json:"efforts,omitempty"`
	Intel              *float64                    `json:"intelligence,omitempty"`
	Speed              *float64                    `json:"speed,omitempty"`
	Reasoning          *float64                    `json:"reasoning,omitempty"`
	Hallucination      *float64                    `json:"hallucination,omitempty"`
	Coding             *float64                    `json:"coding,omitempty"`
	Latency            *float64                    `json:"latency,omitempty"`
}

func (s *importService) ImportJSON(ctx context.Context, reader io.Reader, mode ImportMode) (*ImportResult, error) {
	var file ModelsFile
	if err := json.NewDecoder(reader).Decode(&file); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
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

	for _, model := range file.Models {
		modelID, exists := modelMap[strings.ToLower(model.Name)]
		if !exists {
			result.Skipped++
			continue
		}

		result.MatchedModels = append(result.MatchedModels, model.Name)

		baseTags := make(map[string]string)
		baseTags["has_reasoning_effort"] = fmt.Sprintf("%v", model.HasReasoningEffort)
		if model.ContextWindow != nil {
			baseTags["context_window"] = fmt.Sprintf("%d", *model.ContextWindow)
		}
		if model.CostType != nil {
			baseTags["cost_type"] = *model.CostType
		}
		if model.CostPer1mInput != nil {
			baseTags["cost_per_1m_input"] = fmt.Sprintf("%.2f", *model.CostPer1mInput)
		}
		if model.CostPer1mOutput != nil {
			baseTags["cost_per_1m_output"] = fmt.Sprintf("%.2f", *model.CostPer1mOutput)
		}

		if model.HasReasoningEffort && len(model.Efforts) > 0 {
			for effort, tags := range model.Efforts {
				effortTags := copyMap(baseTags)
				for k, v := range tags {
					effortTags[k] = fmt.Sprintf("%v", v)
				}

				if mode == ImportModeMerge {
					existing, _ := s.tagRepo.GetByModelEffort(ctx, modelID, effort)
					for _, t := range existing {
						if _, exists := effortTags[t.Key]; !exists {
							effortTags[t.Key] = t.Value
						}
					}
				}

				if err := s.tagRepo.Set(ctx, modelID, effort, effortTags); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("%s effort %s: %v", model.Name, effort, err))
					continue
				}
				result.Imported++
			}
		} else {
			if model.Intel != nil {
				baseTags["intelligence"] = fmt.Sprintf("%g", *model.Intel)
			}
			if model.Speed != nil {
				baseTags["speed"] = fmt.Sprintf("%g", *model.Speed)
			}
			if model.Reasoning != nil {
				baseTags["reasoning"] = fmt.Sprintf("%g", *model.Reasoning)
			}
			if model.Hallucination != nil {
				baseTags["hallucination"] = fmt.Sprintf("%g", *model.Hallucination)
			}
			if model.Coding != nil {
				baseTags["coding"] = fmt.Sprintf("%g", *model.Coding)
			}
			if model.Latency != nil {
				baseTags["latency"] = fmt.Sprintf("%g", *model.Latency)
			}

			if mode == ImportModeMerge {
				existing, _ := s.tagRepo.GetByModelEffort(ctx, modelID, "")
				for _, t := range existing {
					if _, exists := baseTags[t.Key]; !exists {
						baseTags[t.Key] = t.Value
					}
				}
			}

			if err := s.tagRepo.Set(ctx, modelID, "", baseTags); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", model.Name, err))
				continue
			}
			result.Imported++
		}
	}

	return result, nil
}

func copyMap(m map[string]string) map[string]string {
	result := make(map[string]string)
	for k, v := range m {
		result[k] = v
	}
	return result
}
