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
	PreviewResolve(ctx context.Context, filterExpr json.RawMessage, sortExpr json.RawMessage, includeModels json.RawMessage, composition *models.CompositionNode) ([]ResolvedModel, error)
	GetDependencies(ctx context.Context) (map[string][]string, error)
}

type ResolvedModel struct {
	Model            models.Model
	Provider         models.Provider
	ReasoningEffort  string
	GlobalMetadata   map[string]string
	ProviderMetadata map[string]string
}

type virtualModelService struct {
	vmRepo           repository.VirtualModelRepository
	modelRepo        repository.ModelRepository
	tagRepo          repository.TagRepository
	providerRepo     repository.ProviderRepository
	providerMetaRepo repository.ProviderMetadataRepository
	globalMetaRepo   repository.GlobalMetadataRepository
	mappingRepo      repository.ModelMappingRepository
}

func NewVirtualModelService(
	vmRepo repository.VirtualModelRepository,
	modelRepo repository.ModelRepository,
	tagRepo repository.TagRepository,
	providerRepo repository.ProviderRepository,
	providerMetaRepo repository.ProviderMetadataRepository,
	globalMetaRepo repository.GlobalMetadataRepository,
	mappingRepo repository.ModelMappingRepository,
) VirtualModelService {
	return &virtualModelService{
		vmRepo:           vmRepo,
		modelRepo:        modelRepo,
		tagRepo:          tagRepo,
		providerRepo:     providerRepo,
		providerMetaRepo: providerMetaRepo,
		globalMetaRepo:   globalMetaRepo,
		mappingRepo:      mappingRepo,
	}
}

func (s *virtualModelService) Create(ctx context.Context, req models.CreateVirtualModelRequest) (*models.VirtualModel, error) {
	if req.Composition != nil {
		if len(req.FilterExpr) > 0 && string(req.FilterExpr) != "{}" {
			return nil, fmt.Errorf("cannot specify both composition and filter_expr at top level")
		}
		if err := validateCompositionFilterSources(req.Composition); err != nil {
			return nil, fmt.Errorf("invalid composition: %w", err)
		}
		if err := models.ValidateCompositionNode(req.Composition, 0); err != nil {
			return nil, fmt.Errorf("invalid composition: %w", err)
		}
	} else {
		if err := validateFilterExpr(req.FilterExpr); err != nil {
			return nil, fmt.Errorf("invalid filter_expr: %w", err)
		}
	}
	if err := validateSortExpr(req.SortExpr); err != nil {
		return nil, fmt.Errorf("invalid sort_expr: %w", err)
	}
	if err := validateIncludeModels(req.IncludeModels); err != nil {
		return nil, fmt.Errorf("invalid include_models: %w", err)
	}

	vm := &models.VirtualModel{
		Name:          req.Name,
		Description:   req.Description,
		FilterExpr:    req.FilterExpr,
		SortExpr:      req.SortExpr,
		IncludeModels: req.IncludeModels,
		Composition:   req.Composition,
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
	if req.Description != nil {
		vm.Description = *req.Description
	}
	if req.Composition != nil {
		if err := validateCompositionFilterSources(req.Composition); err != nil {
			return nil, fmt.Errorf("invalid composition: %w", err)
		}
		if err := models.ValidateCompositionNode(req.Composition, 0); err != nil {
			return nil, fmt.Errorf("invalid composition: %w", err)
		}
		vm.Composition = req.Composition
		vm.FilterExpr = nil
		vm.SortExpr = nil
	} else if req.FilterExpr != nil || req.SortExpr != nil {
		// Sending filter/sort means switching to leaf mode — clear any existing composition
		vm.Composition = nil
	}
	if req.FilterExpr != nil {
		if vm.Composition != nil {
			return nil, fmt.Errorf("cannot specify both composition and filter_expr")
		}
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
	if req.IncludeModels != nil {
		if err := validateIncludeModels(*req.IncludeModels); err != nil {
			return nil, fmt.Errorf("invalid include_models: %w", err)
		}
		vm.IncludeModels = *req.IncludeModels
	}

	if err := s.vmRepo.Update(ctx, vm); err != nil {
		return nil, err
	}
	return vm, nil
}

func (s *virtualModelService) Delete(ctx context.Context, id int64) error {
	return s.vmRepo.Delete(ctx, id)
}

func (s *virtualModelService) PreviewResolve(ctx context.Context, filterExpr json.RawMessage, sortExpr json.RawMessage, includeModels json.RawMessage, composition *models.CompositionNode) ([]ResolvedModel, error) {
	if composition != nil {
		if len(filterExpr) > 0 && string(filterExpr) != "{}" {
			return nil, fmt.Errorf("cannot specify both composition and filter_expr")
		}
		if err := validateCompositionFilterSources(composition); err != nil {
			return nil, fmt.Errorf("invalid composition: %w", err)
		}
		if err := models.ValidateCompositionNode(composition, 0); err != nil {
			return nil, fmt.Errorf("invalid composition: %w", err)
		}
	} else {
		if err := validateFilterExpr(filterExpr); err != nil {
			return nil, fmt.Errorf("invalid filter_expr: %w", err)
		}
	}
	if err := validateSortExpr(sortExpr); err != nil {
		return nil, fmt.Errorf("invalid sort_expr: %w", err)
	}
	if err := validateIncludeModels(includeModels); err != nil {
		return nil, fmt.Errorf("invalid include_models: %w", err)
	}

	vm := &models.VirtualModel{
		FilterExpr:    filterExpr,
		SortExpr:      sortExpr,
		IncludeModels: includeModels,
		Composition:   composition,
	}
	return s.ResolveModels(ctx, vm)
}

func (s *virtualModelService) ResolveModels(ctx context.Context, vm *models.VirtualModel) ([]ResolvedModel, error) {
	// Composite VM — evaluate the composition tree
	if vm.Composition != nil {
		stack := make(map[string]bool)
		result, err := s.evaluateCompositionNode(ctx, vm.Composition, stack)
		if err != nil {
			return nil, err
		}
		// Apply top-level include_models
		result = s.applyIncludeModels(ctx, result, vm.IncludeModels)
		return result, nil
	}

	// Leaf VM — existing logic
	return s.resolveLeafModels(ctx, vm)
}

// resolveLeafModels contains the original leaf VM resolution logic.
func (s *virtualModelService) resolveLeafModels(ctx context.Context, vm *models.VirtualModel) ([]ResolvedModel, error) {
	var filter models.FilterNode
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

	return s.resolveModelsFiltered(ctx, filter, sortExpr, vm.IncludeModels)
}

// resolveModelsFiltered resolves all models matching filter, applies sort and include_models.
// Shared by resolveLeafModels and resolveFilterSource.
func (s *virtualModelService) resolveModelsFiltered(ctx context.Context, filter models.FilterNode, sortExpr models.SortExpr, includeModels json.RawMessage) ([]ResolvedModel, error) {
	allModels, err := s.modelRepo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}

	// Batch-fetch all mappings for O(1) lookup per model
	var mappingMap map[int64]*repository.ModelMapping
	if s.mappingRepo != nil {
		allMappings, _ := s.mappingRepo.GetAll(ctx)
		mappingMap = make(map[int64]*repository.ModelMapping, len(allMappings))
		for i := range allMappings {
			mappingMap[allMappings[i].SourceModelID] = &allMappings[i]
		}
	}

	type includeRef struct {
		model    models.Model
		provider models.Provider
	}
	var includes []includeRef

	if len(includeModels) > 0 && string(includeModels) != "[]" {
		var refs []models.IncludeModelRef
		if err := json.Unmarshal(includeModels, &refs); err != nil {
			return nil, fmt.Errorf("parse include_models: %w", err)
		}
		for _, ref := range refs {
			provider, err := s.providerRepo.GetByName(ctx, ref.Provider)
			if err != nil || provider == nil {
				continue
			}
			m, err := s.modelRepo.GetByProviderAndName(ctx, provider.ID, ref.Model)
			if err != nil || m == nil {
				continue
			}
			includes = append(includes, includeRef{model: *m, provider: *provider})
		}
	}

	var resolved []ResolvedModel
	for _, m := range allModels {
		efforts, err := s.tagRepo.GetAvailableEfforts(ctx, m.ID)
		if err != nil {
			return nil, err
		}
		if len(efforts) == 0 {
			efforts = []string{""}
		}

		// Check for mapping: if mapped, use target's tags + global metadata
		var mapping *repository.ModelMapping
		if mappingMap != nil {
			mapping = mappingMap[m.ID]
		}

		for _, effort := range efforts {
			var tags []models.Tag
			var globalMeta map[string]string

			if mapping != nil {
				// Mapped: hide source instance tags, use target's global metadata by name
				tags = nil
				globalMeta, _ = s.globalMetaRepo.GetByModelEffort(ctx, mapping.TargetModelName, effort)
				if len(globalMeta) == 0 {
					globalMeta, _ = s.globalMetaRepo.GetByModelEffort(ctx, mapping.TargetModelName, "")
				}
			} else {
				// Unmapped: use source model's own tags and global metadata
				tags, err = s.tagRepo.GetByModelEffort(ctx, m.ID, effort)
				if err != nil {
					return nil, err
				}
				globalMeta, _ = s.globalMetaRepo.GetByModelEffort(ctx, m.Name, effort)
				if len(globalMeta) == 0 {
					globalMeta, _ = s.globalMetaRepo.GetByModelEffort(ctx, m.Name, "")
				}
			}

			m.Tags = tags

			provider, err := s.providerRepo.GetByID(ctx, m.ProviderID)
			if err != nil {
				return nil, err
			}
			if provider == nil {
				continue
			}

			providerMeta, _ := s.providerMetaRepo.GetByProvider(ctx, provider.ID)

			if !matchesFilter(m.Tags, m.Name, filter, provider.Name, providerMeta, globalMeta) {
				continue
			}

			pMetaMap := make(map[string]string)
			pMetaMap["p.name"] = provider.Name
			for _, pm := range providerMeta {
				pMetaMap["p."+pm.Key] = pm.Value
			}

			resolved = append(resolved, ResolvedModel{
				Model:            m,
				Provider:         *provider,
				ReasoningEffort:  effort,
				GlobalMetadata:   globalMeta,
				ProviderMetadata: pMetaMap,
			})
		}
	}

	if len(includes) > 0 {
		existing := make(map[string]bool, len(resolved))
		for _, r := range resolved {
			existing[r.Provider.Name+"/"+r.Model.Name] = true
		}

		var includeResolved []ResolvedModel
		for _, inc := range includes {
			key := inc.provider.Name + "/" + inc.model.Name
			if existing[key] {
				continue
			}

			providerMeta, _ := s.providerMetaRepo.GetByProvider(ctx, inc.provider.ID)

			globalMeta, _ := s.globalMetaRepo.GetByModelEffort(ctx, inc.model.Name, "")

			pMetaMap := make(map[string]string)
			pMetaMap["p.name"] = inc.provider.Name
			for _, pm := range providerMeta {
				pMetaMap["p."+pm.Key] = pm.Value
			}

			includeResolved = append(includeResolved, ResolvedModel{
				Model:            inc.model,
				Provider:         inc.provider,
				ReasoningEffort:  "",
				GlobalMetadata:   globalMeta,
				ProviderMetadata: pMetaMap,
			})
		}

		resolved = append(includeResolved, resolved...)
	}

	sort.Slice(resolved, func(i, j int) bool {
		return compareModels(resolved[i], resolved[j], sortExpr)
	})

	return resolved, nil
}

// evaluateCompositionNode recursively resolves a composition tree node.
func (s *virtualModelService) evaluateCompositionNode(ctx context.Context, node *models.CompositionNode, stack map[string]bool) ([]ResolvedModel, error) {
	if node.IsOperation() {
		return s.resolveOperation(ctx, node, stack)
	}
	return s.resolveSource(ctx, node, stack)
}

// resolveSource resolves a source node. If Vm is set, it resolves that VM's result set
// (with circular detection). If Vm is empty, it queries all raw models.
// Then applies per-node filter and sort.
func (s *virtualModelService) resolveSource(ctx context.Context, node *models.CompositionNode, stack map[string]bool) ([]ResolvedModel, error) {
	var result []ResolvedModel
	var err error

	if node.Vm != "" {
		// Source from a specific VM's result set
		if stack[node.Vm] {
			return nil, fmt.Errorf("circular reference detected: %s", node.Vm)
		}

		sourceVM, err := s.vmRepo.GetByName(ctx, node.Vm)
		if err != nil {
			return nil, fmt.Errorf("resolve VM %q: %w", node.Vm, err)
		}
		if sourceVM == nil {
			return nil, fmt.Errorf("virtual model not found: %s", node.Vm)
		}

		stack[node.Vm] = true
		result, err = s.ResolveModels(ctx, sourceVM)
		delete(stack, node.Vm)
		if err != nil {
			return nil, fmt.Errorf("resolve VM %q: %w", node.Vm, err)
		}
	} else {
		// Source from all raw models
		result, err = s.resolveModelsFiltered(ctx, models.FilterNode{}, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("list models: %w", err)
		}
	}

	// Apply per-node filter
	if node.FilterExpr != nil {
		result = s.filterResolvedModels(result, *node.FilterExpr)
	}

	// Apply per-node sort
	if len(node.SortExpr) > 0 {
		sort.Slice(result, func(i, j int) bool {
			return compareModels(result[i], result[j], node.SortExpr)
		})
	}

	return result, nil
}

// resolveOperation resolves an operation node by resolving all children, applying the set operation, then filter/sort.
func (s *virtualModelService) resolveOperation(ctx context.Context, node *models.CompositionNode, stack map[string]bool) ([]ResolvedModel, error) {
	if len(node.Sources) < 2 {
		return nil, fmt.Errorf("operation %s requires at least 2 sources", node.Operation)
	}

	var sourceResults [][]ResolvedModel
	for i := range node.Sources {
		result, err := s.evaluateCompositionNode(ctx, &node.Sources[i], stack)
		if err != nil {
			return nil, fmt.Errorf("source[%d]: %w", i, err)
		}
		sourceResults = append(sourceResults, result)
	}

	combined := applySetOperation(node.Operation, sourceResults)

	// Apply per-node filter
	if node.FilterExpr != nil {
		combined = s.filterResolvedModels(combined, *node.FilterExpr)
	}

	// Apply per-node sort
	if len(node.SortExpr) > 0 {
		sort.Slice(combined, func(i, j int) bool {
			return compareModels(combined[i], combined[j], node.SortExpr)
		})
	}

	return combined, nil
}

// filterResolvedModels filters an already-resolved model list using a FilterNode.
func (s *virtualModelService) filterResolvedModels(models []ResolvedModel, filter models.FilterNode) []ResolvedModel {
	var result []ResolvedModel
	for _, rm := range models {
		tagMap := make(map[string]string)
		for _, t := range rm.Model.Tags {
			tagMap["mc."+t.Key] = t.Value
		}

		providerMap := make(map[string]string)
		providerMap["p.name"] = rm.Provider.Name
		for k, v := range rm.ProviderMetadata {
			if k != "p.name" {
				providerMap[k] = v
			}
		}

		modelMap := make(map[string]string)
		modelMap["m.name"] = rm.Model.Name
		for k, v := range rm.GlobalMetadata {
			modelMap["m."+k] = v
		}

		allMaps := []map[string]string{tagMap, providerMap, modelMap}
		if evalFilterNode(filter, allMaps) {
			result = append(result, rm)
		}
	}
	return result
}

// applyIncludeModels prepends explicitly included models to the resolved list.
func (s *virtualModelService) applyIncludeModels(ctx context.Context, resolved []ResolvedModel, includeModels json.RawMessage) []ResolvedModel {
	if len(includeModels) == 0 || string(includeModels) == "[]" {
		return resolved
	}

	var refs []models.IncludeModelRef
	if err := json.Unmarshal(includeModels, &refs); err != nil {
		return resolved
	}

	existing := make(map[string]bool, len(resolved))
	for _, r := range resolved {
		existing[r.Provider.Name+"/"+r.Model.Name] = true
	}

	// Batch-fetch mappings for include models
	var mappingMap map[int64]*repository.ModelMapping
	if s.mappingRepo != nil {
		allMappings, _ := s.mappingRepo.GetAll(ctx)
		mappingMap = make(map[int64]*repository.ModelMapping, len(allMappings))
		for i := range allMappings {
			mappingMap[allMappings[i].SourceModelID] = &allMappings[i]
		}
	}

	var includeResolved []ResolvedModel
	for _, ref := range refs {
		key := ref.Provider + "/" + ref.Model
		if existing[key] {
			continue
		}

		provider, err := s.providerRepo.GetByName(ctx, ref.Provider)
		if err != nil || provider == nil {
			continue
		}
		m, err := s.modelRepo.GetByProviderAndName(ctx, provider.ID, ref.Model)
		if err != nil || m == nil {
			continue
		}

		providerMeta, _ := s.providerMetaRepo.GetByProvider(ctx, provider.ID)

		// Check for mapping on included model
		var globalMeta map[string]string
		if mappingMap != nil && mappingMap[m.ID] != nil {
			mapping := mappingMap[m.ID]
			globalMeta, _ = s.globalMetaRepo.GetByModelEffort(ctx, mapping.TargetModelName, "")
		} else {
			globalMeta, _ = s.globalMetaRepo.GetByModelEffort(ctx, m.Name, "")
		}

		pMetaMap := make(map[string]string)
		pMetaMap["p.name"] = provider.Name
		for _, pm := range providerMeta {
			pMetaMap["p."+pm.Key] = pm.Value
		}

		includeResolved = append(includeResolved, ResolvedModel{
			Model:            *m,
			Provider:         *provider,
			ReasoningEffort:  "",
			GlobalMetadata:   globalMeta,
			ProviderMetadata: pMetaMap,
		})
	}

	return append(includeResolved, resolved...)
}

// applySetOperation applies a set operation to multiple resolved model lists.
func applySetOperation(op string, sourceResults [][]ResolvedModel) []ResolvedModel {
	switch op {
	case "union":
		return unionResults(sourceResults)
	case "intersection":
		return intersectionResults(sourceResults)
	case "difference":
		return differenceResults(sourceResults)
	default:
		return nil
	}
}

func modelKey(rm ResolvedModel) string {
	effort := rm.ReasoningEffort
	return rm.Provider.Name + "/" + rm.Model.Name + "/" + effort
}

func unionResults(sourceResults [][]ResolvedModel) []ResolvedModel {
	seen := make(map[string]bool)
	var result []ResolvedModel
	for _, source := range sourceResults {
		for _, rm := range source {
			key := modelKey(rm)
			if !seen[key] {
				seen[key] = true
				result = append(result, rm)
			}
		}
	}
	return result
}

func intersectionResults(sourceResults [][]ResolvedModel) []ResolvedModel {
	if len(sourceResults) < 2 {
		return nil
	}

	sets := make([]map[string]bool, len(sourceResults))
	for i, source := range sourceResults {
		sets[i] = make(map[string]bool, len(source))
		for _, rm := range source {
			sets[i][modelKey(rm)] = true
		}
	}

	common := make(map[string]bool)
	for key := range sets[0] {
		inAll := true
		for i := 1; i < len(sets); i++ {
			if !sets[i][key] {
				inAll = false
				break
			}
		}
		if inAll {
			common[key] = true
		}
	}

	var result []ResolvedModel
	for _, rm := range sourceResults[0] {
		if common[modelKey(rm)] {
			result = append(result, rm)
		}
	}
	return result
}

func differenceResults(sourceResults [][]ResolvedModel) []ResolvedModel {
	if len(sourceResults) < 2 {
		if len(sourceResults) == 1 {
			return sourceResults[0]
		}
		return nil
	}

	exclude := make(map[string]bool)
	for i := 1; i < len(sourceResults); i++ {
		for _, rm := range sourceResults[i] {
			exclude[modelKey(rm)] = true
		}
	}

	var result []ResolvedModel
	for _, rm := range sourceResults[0] {
		if !exclude[modelKey(rm)] {
			result = append(result, rm)
		}
	}
	return result
}

// GetDependencies returns a map of VM name -> list of VM names it depends on.
func (s *virtualModelService) GetDependencies(ctx context.Context) (map[string][]string, error) {
	vms, err := s.vmRepo.List(ctx)
	if err != nil {
		return nil, err
	}

	deps := make(map[string][]string)
	for _, vm := range vms {
		if vm.Composition != nil {
			deps[vm.Name] = vm.Composition.CollectVMNames()
		} else {
			deps[vm.Name] = []string{}
		}
	}
	return deps, nil
}

func matchesFilter(tags []models.Tag, modelName string, filter models.FilterNode, providerName string, providerMeta []models.ProviderMetadata, globalMeta map[string]string) bool {
	if !filter.IsLeaf() && len(filter.And) == 0 && len(filter.Or) == 0 && filter.Not == nil {
		return true
	}

	tagMap := make(map[string]string)
	for _, t := range tags {
		tagMap["mc."+t.Key] = t.Value
	}

	providerMap := make(map[string]string)
	providerMap["p.name"] = providerName
	for _, pm := range providerMeta {
		providerMap["p."+pm.Key] = pm.Value
	}

	modelMap := make(map[string]string)
	modelMap["m.name"] = modelName
	for k, v := range globalMeta {
		modelMap["m."+k] = v
	}

	allMaps := []map[string]string{tagMap, providerMap, modelMap}
	return evalFilterNode(filter, allMaps)
}

func evalFilterNode(node models.FilterNode, allMaps []map[string]string) bool {
	if node.IsLeaf() {
		var val string
		var exists bool
		for _, m := range allMaps {
			if v, ok := m[node.Key]; ok {
				val = v
				exists = true
				break
			}
		}
		if !exists {
			return false
		}
		return evaluateCondition(val, node.Op, node.Value)
	}

	if len(node.And) > 0 {
		for _, child := range node.And {
			if !evalFilterNode(child, allMaps) {
				return false
			}
		}
		return true
	}

	if len(node.Or) > 0 {
		for _, child := range node.Or {
			if evalFilterNode(child, allMaps) {
				return true
			}
		}
		return false
	}

	if node.Not != nil {
		return !evalFilterNode(*node.Not, allMaps)
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
	aTagMap := make(map[string]string)
	for _, t := range a.Model.Tags {
		aTagMap["mc."+t.Key] = t.Value
	}
	bTagMap := make(map[string]string)
	for _, t := range b.Model.Tags {
		bTagMap["mc."+t.Key] = t.Value
	}

	for k, v := range a.GlobalMetadata {
		aTagMap["m."+k] = v
	}
	for k, v := range b.GlobalMetadata {
		bTagMap["m."+k] = v
	}

	aTagMap["m.name"] = a.Model.Name
	bTagMap["m.name"] = b.Model.Name

	for k, v := range a.ProviderMetadata {
		aTagMap[k] = v
	}
	for k, v := range b.ProviderMetadata {
		bTagMap[k] = v
	}

	aMaps := []map[string]string{aTagMap}
	bMaps := []map[string]string{bTagMap}

	for _, s := range sortExpr {
		if s.IsCondition() {
			aMatch := evalFilterNode(*s.Condition, aMaps)
			bMatch := evalFilterNode(*s.Condition, bMaps)
			if aMatch == bMatch {
				continue
			}
			if s.Direction == "desc" {
				return !aMatch
			}
			return aMatch
		}

		aVal := aTagMap[s.Key]
		bVal := bTagMap[s.Key]

		aMissing := aVal == ""
		bMissing := bVal == ""
		if aMissing && bMissing {
			continue
		}
		if aMissing {
			return false
		}
		if bMissing {
			return true
		}

		if s.Direction != "" {
			cmp := compareNumeric(aVal, bVal)
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
	var f models.FilterNode
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	return validateFilterNode(&f)
}

func validateFilterNode(node *models.FilterNode) error {
	if node == nil {
		return fmt.Errorf("nil filter node")
	}

	hasLeaf := node.Key != ""
	hasAnd := len(node.And) > 0
	hasOr := len(node.Or) > 0
	hasNot := node.Not != nil

	if hasLeaf && (hasAnd || hasOr || hasNot) {
		return fmt.Errorf("filter node cannot have both leaf fields (key/op/value) and logical operators (and/or/not)")
	}

	if !hasLeaf && !hasAnd && !hasOr && !hasNot {
		return fmt.Errorf("filter node must have either leaf fields or logical operators")
	}

	if hasLeaf {
		if node.Op == "" {
			return fmt.Errorf("leaf node missing operator")
		}
		validOps := map[string]bool{"eq": true, "neq": true, "gt": true, "gte": true, "lt": true, "lte": true, "in": true, "contains": true}
		if !validOps[node.Op] {
			return fmt.Errorf("unknown operator: %s", node.Op)
		}
		return nil
	}

	if hasAnd {
		for i := range node.And {
			if err := validateFilterNode(&node.And[i]); err != nil {
				return fmt.Errorf("and[%d]: %w", i, err)
			}
		}
	}
	if hasOr {
		for i := range node.Or {
			if err := validateFilterNode(&node.Or[i]); err != nil {
				return fmt.Errorf("or[%d]: %w", i, err)
			}
		}
	}
	if hasNot {
		if err := validateFilterNode(node.Not); err != nil {
			return fmt.Errorf("not: %w", err)
		}
	}
	return nil
}

func validateSortExpr(data json.RawMessage) error {
	if len(data) == 0 || string(data) == "[]" {
		return nil
	}
	var s models.SortExpr
	return json.Unmarshal(data, &s)
}

// validateCompositionFilterSources walks the composition tree and validates filter_expr
// on every filter source node using the existing validateFilterNode helper.
func validateCompositionFilterSources(node *models.CompositionNode) error {
	if node == nil {
		return nil
	}
	if node.FilterExpr != nil {
		if err := validateFilterNode(node.FilterExpr); err != nil {
			return fmt.Errorf("filter_expr: %w", err)
		}
	}
	for i := range node.Sources {
		if err := validateCompositionFilterSources(&node.Sources[i]); err != nil {
			return fmt.Errorf("source[%d]: %w", i, err)
		}
	}
	return nil
}

func validateIncludeModels(data json.RawMessage) error {
	if len(data) == 0 || string(data) == "[]" {
		return nil
	}
	var refs []models.IncludeModelRef
	if err := json.Unmarshal(data, &refs); err != nil {
		return err
	}
	for i, ref := range refs {
		if ref.Provider == "" {
			return fmt.Errorf("include_models[%d]: provider is required", i)
		}
		if ref.Model == "" {
			return fmt.Errorf("include_models[%d]: model is required", i)
		}
	}
	return nil
}
