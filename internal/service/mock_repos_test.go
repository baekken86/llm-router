package service

import (
	"context"
	"time"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
)

// --- Mock VirtualModelRepository ---

type mockVMRepo struct {
	vms  map[string]*models.VirtualModel
	next int64
}

func newMockVMRepo() *mockVMRepo {
	return &mockVMRepo{vms: make(map[string]*models.VirtualModel), next: 1}
}

func (m *mockVMRepo) Create(_ context.Context, vm *models.VirtualModel) error {
	vm.ID = m.next
	m.next++
	m.vms[vm.Name] = vm
	return nil
}

func (m *mockVMRepo) GetByID(_ context.Context, id int64) (*models.VirtualModel, error) {
	for _, vm := range m.vms {
		if vm.ID == id {
			return vm, nil
		}
	}
	return nil, nil
}

func (m *mockVMRepo) GetByName(_ context.Context, name string) (*models.VirtualModel, error) {
	vm, ok := m.vms[name]
	if !ok {
		return nil, nil
	}
	return vm, nil
}

func (m *mockVMRepo) List(_ context.Context) ([]models.VirtualModel, error) {
	var result []models.VirtualModel
	for _, vm := range m.vms {
		result = append(result, *vm)
	}
	return result, nil
}

func (m *mockVMRepo) Update(_ context.Context, vm *models.VirtualModel) error {
	m.vms[vm.Name] = vm
	return nil
}

func (m *mockVMRepo) Delete(_ context.Context, id int64) error {
	for name, vm := range m.vms {
		if vm.ID == id {
			delete(m.vms, name)
			return nil
		}
	}
	return nil
}

// --- Mock ModelRepository ---

type mockModelRepo struct {
	models    []models.Model
	cbStrikes map[int64]int
}

func newMockModelRepo() *mockModelRepo {
	return &mockModelRepo{}
}

func (m *mockModelRepo) Create(_ context.Context, mod *models.Model) error {
	m.models = append(m.models, *mod)
	return nil
}

func (m *mockModelRepo) GetByID(_ context.Context, id int64) (*models.Model, error) {
	for i := range m.models {
		if m.models[i].ID == id {
			return &m.models[i], nil
		}
	}
	return nil, nil
}

func (m *mockModelRepo) GetByProviderAndName(_ context.Context, providerID int64, name string) (*models.Model, error) {
	for i := range m.models {
		if m.models[i].ProviderID == providerID && m.models[i].Name == name {
			return &m.models[i], nil
		}
	}
	return nil, nil
}

func (m *mockModelRepo) ListByProvider(_ context.Context, providerID int64) ([]models.Model, error) {
	var result []models.Model
	for _, mod := range m.models {
		if mod.ProviderID == providerID {
			result = append(result, mod)
		}
	}
	return result, nil
}

func (m *mockModelRepo) ListAll(_ context.Context) ([]models.Model, error) {
	result := make([]models.Model, len(m.models))
	copy(result, m.models)
	return result, nil
}

func (m *mockModelRepo) Delete(_ context.Context, id int64) error {
	for i, mod := range m.models {
		if mod.ID == id {
			m.models = append(m.models[:i], m.models[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *mockModelRepo) Upsert(_ context.Context, providerID int64, name string) (*models.Model, error) {
	for i := range m.models {
		if m.models[i].ProviderID == providerID && m.models[i].Name == name {
			return &m.models[i], nil
		}
	}
	mod := models.Model{ID: int64(len(m.models) + 1), ProviderID: providerID, Name: name}
	m.models = append(m.models, mod)
	return &mod, nil
}

func (m *mockModelRepo) ToggleDisabled(_ context.Context, id int64, disabled bool, _ *time.Duration) error {
	for i := range m.models {
		if m.models[i].ID == id {
			m.models[i].Disabled = disabled
			return nil
		}
	}
	return nil
}

func (m *mockModelRepo) ListEnabled(_ context.Context) ([]models.Model, error) {
	var result []models.Model
	for _, mod := range m.models {
		if !mod.Disabled {
			result = append(result, mod)
		}
	}
	return result, nil
}

func (m *mockModelRepo) DisableByProviderExcept(_ context.Context, providerID int64, names []string) (int64, error) {
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}
	var count int64
	for i := range m.models {
		if m.models[i].ProviderID == providerID && !nameSet[m.models[i].Name] && !m.models[i].Disabled {
			m.models[i].Disabled = true
			count++
		}
	}
	return count, nil
}

func (m *mockModelRepo) ListExpiredDisabled(_ context.Context, _ time.Time) ([]int64, error) {
	return nil, nil
}

func (m *mockModelRepo) GetCBStrikes(_ context.Context, modelID int64) (int, error) {
	if m.cbStrikes == nil {
		return 0, nil
	}
	return m.cbStrikes[modelID], nil
}

func (m *mockModelRepo) SetCBStrikes(_ context.Context, modelID int64, strikes int) error {
	if m.cbStrikes == nil {
		m.cbStrikes = map[int64]int{}
	}
	m.cbStrikes[modelID] = strikes
	return nil
}

// --- Mock TagRepository ---

type mockTagRepo struct {
	tags    map[int64][]models.Tag
	efforts map[int64][]string
}

func newMockTagRepo() *mockTagRepo {
	return &mockTagRepo{
		tags:    make(map[int64][]models.Tag),
		efforts: make(map[int64][]string),
	}
}

func (m *mockTagRepo) Set(_ context.Context, modelID int64, effort string, tags map[string]string) error {
	var tagList []models.Tag
	for k, v := range tags {
		tagList = append(tagList, models.Tag{ModelID: modelID, ReasoningEffort: effort, Key: k, Value: v})
	}
	m.tags[modelID] = tagList
	return nil
}

func (m *mockTagRepo) GetByModel(_ context.Context, modelID int64) ([]models.Tag, error) {
	return m.tags[modelID], nil
}

func (m *mockTagRepo) GetByModelEffort(_ context.Context, modelID int64, effort string) ([]models.Tag, error) {
	all := m.tags[modelID]
	var filtered []models.Tag
	for _, t := range all {
		if t.ReasoningEffort == effort {
			filtered = append(filtered, t)
		}
	}
	return filtered, nil
}

func (m *mockTagRepo) GetAvailableEfforts(_ context.Context, modelID int64) ([]string, error) {
	efforts := m.efforts[modelID]
	if len(efforts) == 0 {
		return []string{""}, nil
	}
	return efforts, nil
}

func (m *mockTagRepo) DeleteByModel(_ context.Context, modelID int64) error {
	delete(m.tags, modelID)
	return nil
}

// --- Mock ProviderRepository ---

type mockProviderRepo struct {
	providers map[int64]*models.Provider
	byName    map[string]*models.Provider
	byKey     map[string]*models.Provider
	nextID    int64
}

func newMockProviderRepo() *mockProviderRepo {
	return &mockProviderRepo{
		providers: make(map[int64]*models.Provider),
		byName:    make(map[string]*models.Provider),
		byKey:     make(map[string]*models.Provider),
		nextID:    1,
	}
}

func (m *mockProviderRepo) add(p *models.Provider) {
	m.providers[p.ID] = p
	m.byName[p.Name] = p
	key := p.ProviderKey
	if key == "" {
		key = p.Name
	}
	if m.byKey == nil {
		m.byKey = make(map[string]*models.Provider)
	}
	m.byKey[key] = p
}

func (m *mockProviderRepo) Create(_ context.Context, p *models.Provider) error {
	if p.ID == 0 {
		p.ID = m.nextID
		m.nextID++
	}
	m.add(p)
	return nil
}

func (m *mockProviderRepo) GetByID(_ context.Context, id int64) (*models.Provider, error) {
	return m.providers[id], nil
}

func (m *mockProviderRepo) GetByName(_ context.Context, name string) (*models.Provider, error) {
	return m.byName[name], nil
}

func (m *mockProviderRepo) GetByKey(_ context.Context, key string) (*models.Provider, error) {
	if m.byKey == nil {
		return nil, nil
	}
	return m.byKey[key], nil
}

func (m *mockProviderRepo) List(_ context.Context) ([]models.Provider, error) {
	var result []models.Provider
	for _, p := range m.providers {
		result = append(result, *p)
	}
	return result, nil
}

func (m *mockProviderRepo) ListByMetadata(_ context.Context, filters map[string]string) ([]models.Provider, error) {
	return nil, nil
}

func (m *mockProviderRepo) Update(_ context.Context, p *models.Provider) error {
	m.add(p)
	return nil
}

func (m *mockProviderRepo) Delete(_ context.Context, id int64) error {
	delete(m.providers, id)
	return nil
}

func (m *mockProviderRepo) ListExpiredDisabled(_ context.Context, _ time.Time) ([]int64, error) {
	return nil, nil
}

// --- Mock ProviderMetadataRepository ---

type mockProviderMetaRepo struct {
	meta map[int64][]models.ProviderMetadata
}

func newMockProviderMetaRepo() *mockProviderMetaRepo {
	return &mockProviderMetaRepo{meta: make(map[int64][]models.ProviderMetadata)}
}

func (m *mockProviderMetaRepo) Set(_ context.Context, providerID int64, tags map[string]string) error {
	var list []models.ProviderMetadata
	for k, v := range tags {
		list = append(list, models.ProviderMetadata{ProviderID: providerID, Key: k, Value: v})
	}
	m.meta[providerID] = list
	return nil
}

func (m *mockProviderMetaRepo) GetByProvider(_ context.Context, providerID int64) ([]models.ProviderMetadata, error) {
	return m.meta[providerID], nil
}

func (m *mockProviderMetaRepo) GetByProviders(_ context.Context, providerIDs []int64) (map[int64][]models.ProviderMetadata, error) {
	result := make(map[int64][]models.ProviderMetadata)
	for _, id := range providerIDs {
		result[id] = m.meta[id]
	}
	return result, nil
}

func (m *mockProviderMetaRepo) ListAll(_ context.Context) (map[int64]map[string]string, error) {
	return nil, nil
}

func (m *mockProviderMetaRepo) ListAllKeys(_ context.Context) (map[string][]string, error) {
	return nil, nil
}

func (m *mockProviderMetaRepo) DeleteByProvider(_ context.Context, providerID int64) error {
	delete(m.meta, providerID)
	return nil
}

func (m *mockProviderMetaRepo) UpsertKey(_ context.Context, providerID int64, key, value string) error {
	return nil
}

// --- Mock GlobalMetadataRepository ---

type mockGlobalMetaRepo struct {
	data map[string]map[string]map[string]string // modelName → effort → tags
}

func newMockGlobalMetaRepo() *mockGlobalMetaRepo {
	return &mockGlobalMetaRepo{data: make(map[string]map[string]map[string]string)}
}

func (m *mockGlobalMetaRepo) Set(_ context.Context, modelName, effort string, tags map[string]string) error {
	if m.data[modelName] == nil {
		m.data[modelName] = make(map[string]map[string]string)
	}
	m.data[modelName][effort] = tags
	return nil
}

func (m *mockGlobalMetaRepo) GetByModel(_ context.Context, modelName string) (map[string]map[string]string, error) {
	d, ok := m.data[modelName]
	if !ok {
		return nil, nil
	}
	return d, nil
}

func (m *mockGlobalMetaRepo) GetByModelEffort(_ context.Context, modelName, effort string) (map[string]string, error) {
	d, ok := m.data[modelName]
	if !ok {
		return nil, nil
	}
	tags, ok := d[effort]
	if !ok {
		return nil, nil
	}
	return tags, nil
}

func (m *mockGlobalMetaRepo) ListModels(_ context.Context) ([]string, error) {
	var result []string
	for k := range m.data {
		result = append(result, k)
	}
	return result, nil
}

func (m *mockGlobalMetaRepo) ListAllKeys(_ context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockGlobalMetaRepo) GetAll(_ context.Context) (map[string]map[string]map[string]string, error) {
	return m.data, nil
}

// Compile-time interface checks
var _ repository.VirtualModelRepository = (*mockVMRepo)(nil)
var _ repository.ModelRepository = (*mockModelRepo)(nil)
var _ repository.TagRepository = (*mockTagRepo)(nil)
var _ repository.ProviderRepository = (*mockProviderRepo)(nil)
var _ repository.ProviderMetadataRepository = (*mockProviderMetaRepo)(nil)
var _ repository.GlobalMetadataRepository = (*mockGlobalMetaRepo)(nil)

// --- Mock GlobalSortConditionRepository ---

type mockGlobalSortRepo struct {
	conditions []models.GlobalSortCondition
}

func (m *mockGlobalSortRepo) List(_ context.Context) ([]models.GlobalSortCondition, error) {
	// Stable ordering: by position then id (mirrors the SQL ORDER BY).
	result := make([]models.GlobalSortCondition, len(m.conditions))
	copy(result, m.conditions)
	for i := 1; i < len(result); i++ {
		for j := len(result) - 1; j > 0; j-- {
			if result[j].Position < result[j-1].Position || (result[j].Position == result[j-1].Position && result[j].ID < result[j-1].ID) {
				result[j], result[j-1] = result[j-1], result[j]
			}
		}
	}
	return result, nil
}

func (m *mockGlobalSortRepo) Get(_ context.Context, id int64) (*models.GlobalSortCondition, error) {
	for i := range m.conditions {
		if m.conditions[i].ID == id {
			return &m.conditions[i], nil
		}
	}
	return nil, nil
}

func (m *mockGlobalSortRepo) Create(_ context.Context, cond *models.GlobalSortCondition) error {
	m.conditions = append(m.conditions, *cond)
	return nil
}

func (m *mockGlobalSortRepo) Update(_ context.Context, cond *models.GlobalSortCondition) error {
	for i := range m.conditions {
		if m.conditions[i].ID == cond.ID {
			m.conditions[i] = *cond
			return nil
		}
	}
	return nil
}

func (m *mockGlobalSortRepo) Delete(_ context.Context, id int64) error {
	return nil
}

func (m *mockGlobalSortRepo) ListDisabledIDs(_ context.Context, virtualModelID int64) ([]int64, error) {
	return nil, nil
}

func (m *mockGlobalSortRepo) SetDisabled(_ context.Context, virtualModelID int64, disabledIDs []int64) error {
	return nil
}

func (m *mockGlobalSortRepo) ListDisabledIDsByModel(_ context.Context, virtualModelIDs []int64) (map[int64][]int64, error) {
	return nil, nil
}

var _ repository.GlobalSortConditionRepository = (*mockGlobalSortRepo)(nil)

// --- Mock GlobalFilterConditionRepository ---

type mockGlobalFilterRepo struct {
	conditions []models.GlobalFilterCondition
}

func (m *mockGlobalFilterRepo) List(_ context.Context) ([]models.GlobalFilterCondition, error) {
	// Stable ordering: by position then id (mirrors the SQL ORDER BY).
	result := make([]models.GlobalFilterCondition, len(m.conditions))
	copy(result, m.conditions)
	for i := 1; i < len(result); i++ {
		for j := len(result) - 1; j > 0; j-- {
			if result[j].Position < result[j-1].Position || (result[j].Position == result[j-1].Position && result[j].ID < result[j-1].ID) {
				result[j], result[j-1] = result[j-1], result[j]
			}
		}
	}
	return result, nil
}

func (m *mockGlobalFilterRepo) Get(_ context.Context, id int64) (*models.GlobalFilterCondition, error) {
	for i := range m.conditions {
		if m.conditions[i].ID == id {
			return &m.conditions[i], nil
		}
	}
	return nil, nil
}

func (m *mockGlobalFilterRepo) Create(_ context.Context, cond *models.GlobalFilterCondition) error {
	m.conditions = append(m.conditions, *cond)
	return nil
}

func (m *mockGlobalFilterRepo) Update(_ context.Context, cond *models.GlobalFilterCondition) error {
	for i := range m.conditions {
		if m.conditions[i].ID == cond.ID {
			m.conditions[i] = *cond
			return nil
		}
	}
	return nil
}

func (m *mockGlobalFilterRepo) Delete(_ context.Context, id int64) error {
	return nil
}

func (m *mockGlobalFilterRepo) ListDisabledIDs(_ context.Context, virtualModelID int64) ([]int64, error) {
	return nil, nil
}

func (m *mockGlobalFilterRepo) SetDisabled(_ context.Context, virtualModelID int64, disabledIDs []int64) error {
	return nil
}

func (m *mockGlobalFilterRepo) ListDisabledIDsByModel(_ context.Context, virtualModelIDs []int64) (map[int64][]int64, error) {
	return nil, nil
}

var _ repository.GlobalFilterConditionRepository = (*mockGlobalFilterRepo)(nil)

func (m *mockGlobalSortRepo) Reorder(_ context.Context, _ []int64) error { return nil }

func (m *mockGlobalFilterRepo) Reorder(_ context.Context, _ []int64) error { return nil }
