package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
)

type VirtualModelService interface {
	Create(ctx context.Context, req models.CreateVirtualModelRequest) (*models.VirtualModel, error)
	GetByID(ctx context.Context, id int64) (*models.VirtualModel, error)
	GetByName(ctx context.Context, name string) (*models.VirtualModel, error)
	List(ctx context.Context) ([]models.VirtualModel, error)
	Update(ctx context.Context, id int64, req models.UpdateVirtualModelRequest) (*models.VirtualModel, error)
	Delete(ctx context.Context, id int64) error
	ResolveModels(ctx context.Context, vm *models.VirtualModel) ([]ResolvedModel, error)
}

type ResolvedModel struct {
	Model    models.Model
	Provider models.Provider
}

type virtualModelService struct {
	vmRepo       repository.VirtualModelRepository
	modelRepo    repository.ModelRepository
	tagRepo      repository.TagRepository
	providerRepo repository.ProviderRepository
}

func NewVirtualModelService(
	vmRepo repository.VirtualModelRepository,
	modelRepo repository.ModelRepository,
	tagRepo repository.TagRepository,
	providerRepo repository.ProviderRepository,
) VirtualModelService {
	return &virtualModelService{
		vmRepo:       vmRepo,
		modelRepo:    modelRepo,
		tagRepo:      tagRepo,
		providerRepo: providerRepo,
	}
}

func (s *virtualModelService) Create(ctx context.Context, req models.CreateVirtualModelRequest) (*models.VirtualModel, error) {
	if err := validateFilterExpr(req.FilterExpr); err != nil {
		return nil, fmt.Errorf("invalid filter_expr: %w", err)
	}
	if err := validateSortExpr(req.SortExpr); err != nil {
		return nil, fmt.Errorf("invalid sort_expr: %w", err)
	}

	vm := &models.VirtualModel{
		Name:       req.Name,
		FilterExpr: req.FilterExpr,
		SortExpr:   req.SortExpr,
	}
	if req.MaxRetries != nil {
		vm.MaxRetries = *req.MaxRetries
	}
	if req.RetryOnStatus != nil {
		vm.RetryOnStatus = req.RetryOnStatus
	}

	if err := s.vmRepo.Create(ctx, vm); err != nil {
		return nil, err
	}
	return vm, nil
}

func (s *virtualModelService) GetByID(ctx context.Context, id int64) (*models.VirtualModel, error) {
	return s.vmRepo.GetByID(ctx, id)
}

func (s *virtualModelService) GetByName(ctx context.Context, name string) (*models.VirtualModel, error) {
	return s.vmRepo.GetByName(ctx, name)
}

func (s *virtualModelService) List(ctx context.Context) ([]models.VirtualModel, error) {
	return s.vmRepo.List(ctx)
}

func (s *virtualModelService) Update(ctx context.Context, id int64, req models.UpdateVirtualModelRequest) (*models.VirtualModel, error) {
	vm, err := s.vmRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if vm == nil {
		return nil, fmt.Errorf("virtual model not found: %d", id)
	}

	if req.Name != nil {
		vm.Name = *req.Name
	}
	if req.FilterExpr != nil {
		if err := validateFilterExpr(*req.FilterExpr); err != nil {
			return nil, fmt.Errorf("invalid filter_expr: %w", err)
		}
		vm.FilterExpr = *req.FilterExpr
	}
	if req.SortExpr != nil {
		if err := validateSortExpr(*req.SortExpr); err != nil {
			return nil, fmt.Errorf("invalid sort_expr: %w", err)
		}
		vm.SortExpr = *req.SortExpr
	}
	if req.MaxRetries != nil {
		vm.MaxRetries = *req.MaxRetries
	}
	if req.RetryOnStatus != nil {
		vm.RetryOnStatus = *req.RetryOnStatus
	}

	if err := s.vmRepo.Update(ctx, vm); err != nil {
		return nil, err
	}
	return vm, nil
}

func (s *virtualModelService) Delete(ctx context.Context, id int64) error {
	return s.vmRepo.Delete(ctx, id)
}

func (s *virtualModelService) ResolveModels(ctx context.Context, vm *models.VirtualModel) ([]ResolvedModel, error) {
	allModels, err := s.modelRepo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}

	var filter models.FilterExpr
	if len(vm.FilterExpr) > 0 {
		if err := json.Unmarshal(vm.FilterExpr, &filter); err != nil {
			return nil, fmt.Errorf("parse filter: %w", err)
		}
	}

	var sortExpr models.SortExpr
	if len(vm.SortExpr) > 0 {
		if err := json.Unmarshal(vm.SortExpr, &sortExpr); err != nil {
			return nil, fmt.Errorf("parse sort: %w", err)
		}
	}

	var resolved []ResolvedModel
	for _, m := range allModels {
		tags, err := s.tagRepo.GetByModel(ctx, m.ID)
		if err != nil {
			return nil, err
		}
		m.Tags = tags

		if !matchesFilter(m.Tags, filter) {
			continue
		}

		provider, err := s.providerRepo.GetByID(ctx, m.ProviderID)
		if err != nil {
			return nil, err
		}
		if provider == nil {
			continue
		}

		resolved = append(resolved, ResolvedModel{
			Model:    m,
			Provider: *provider,
		})
	}

	sort.Slice(resolved, func(i, j int) bool {
		return compareModels(resolved[i], resolved[j], sortExpr)
	})

	return resolved, nil
}

func matchesFilter(tags []models.Tag, filter models.FilterExpr) bool {
	if len(filter.And) == 0 {
		return true
	}

	tagMap := make(map[string]string)
	for _, t := range tags {
		tagMap[t.Key] = t.Value
	}

	for _, cond := range filter.And {
		val, exists := tagMap[cond.Key]
		if !exists {
			return false
		}
		if !evaluateCondition(val, cond.Op, cond.Value) {
			return false
		}
	}

	return true
}

func evaluateCondition(actual string, op string, expected interface{}) bool {
	switch op {
	case "eq":
		return fmt.Sprintf("%v", expected) == actual
	case "neq":
		return fmt.Sprintf("%v", expected) != actual
	case "gt":
		return compareNumeric(actual, expected) > 0
	case "gte":
		return compareNumeric(actual, expected) >= 0
	case "lt":
		return compareNumeric(actual, expected) < 0
	case "lte":
		return compareNumeric(actual, expected) <= 0
	case "in":
		arr, ok := expected.([]interface{})
		if !ok {
			return false
		}
		for _, v := range arr {
			if fmt.Sprintf("%v", v) == actual {
				return true
			}
		}
		return false
	case "contains":
		return strings.Contains(actual, fmt.Sprintf("%v", expected))
	default:
		return false
	}
}

func compareNumeric(actual string, expected interface{}) int {
	a, err1 := strconv.ParseFloat(actual, 64)
	b, err2 := toFloat(expected)
	if err1 != nil || err2 != nil {
		return strings.Compare(actual, fmt.Sprintf("%v", expected))
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func toFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case string:
		return strconv.ParseFloat(val, 64)
	default:
		return 0, fmt.Errorf("not a number")
	}
}

func compareModels(a, b ResolvedModel, sortExpr models.SortExpr) bool {
	aMap := tagMap(a.Model.Tags)
	bMap := tagMap(b.Model.Tags)

	for _, s := range sortExpr {
		aVal := aMap[s.Key]
		bVal := bMap[s.Key]

		if s.Direction != "" {
			cmp := strings.Compare(aVal, bVal)
			if cmp == 0 {
				continue
			}
			if s.Direction == "desc" {
				return cmp > 0
			}
			return cmp < 0
		}

		if len(s.Order) > 0 {
			aIdx := indexOf(s.Order, aVal)
			bIdx := indexOf(s.Order, bVal)
			if aIdx == bIdx {
				continue
			}
			return aIdx < bIdx
		}
	}

	return false
}

func tagMap(tags []models.Tag) map[string]string {
	m := make(map[string]string)
	for _, t := range tags {
		m[t.Key] = t.Value
	}
	return m
}

func indexOf(arr []string, val string) int {
	for i, v := range arr {
		if v == val {
			return i
		}
	}
	return len(arr)
}

func validateFilterExpr(data json.RawMessage) error {
	if len(data) == 0 || string(data) == "{}" {
		return nil
	}
	var f models.FilterExpr
	return json.Unmarshal(data, &f)
}

func validateSortExpr(data json.RawMessage) error {
	if len(data) == 0 || string(data) == "[]" {
		return nil
	}
	var s models.SortExpr
	return json.Unmarshal(data, &s)
}
