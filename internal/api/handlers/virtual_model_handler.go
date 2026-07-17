package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/service"
)

type VirtualModelHandler struct {
	vmService service.VirtualModelService
}

type resolvedEntry struct {
	Position        int               `json:"position"`
	ModelID         int64             `json:"model_id"`
	ModelName       string            `json:"model_name"`
	ReasoningEffort string            `json:"reasoning_effort"`
	ProviderID      int64             `json:"provider_id"`
	ProviderName    string            `json:"provider_name"`
	APIType         string            `json:"api_type"`
	Tags            map[string]string `json:"tags"`
}

func NewVirtualModelHandler(vms service.VirtualModelService) *VirtualModelHandler {
	return &VirtualModelHandler{vmService: vms}
}

func (h *VirtualModelHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Get("/dependencies", h.GetDependencies)
	r.Post("/preview", h.Preview)
	r.Get("/{id}", h.GetByID)
	r.Get("/{id}/resolved", h.GetResolved)
	r.Put("/{id}", h.Update)
	r.Delete("/{id}", h.Delete)
	return r
}

func (h *VirtualModelHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateVirtualModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	vm, err := h.vmService.Create(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, vm)
}

func (h *VirtualModelHandler) Preview(w http.ResponseWriter, r *http.Request) {
	var req models.PreviewVirtualModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resolved, err := h.vmService.PreviewResolve(r.Context(), req.FilterExpr, req.SortExpr, req.IncludeModels, req.Composition)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var result []resolvedEntry
	for i, rm := range resolved {
		tags := make(map[string]string)
		for _, t := range rm.Model.Tags {
			tags["mc."+t.Key] = t.Value
		}
		for k, v := range rm.GlobalMetadata {
			tags["m."+k] = v
		}
		for k, v := range rm.ProviderMetadata {
			tags[k] = v
		}

		result = append(result, resolvedEntry{
			Position:        i + 1,
			ModelID:         rm.Model.ID,
			ModelName:       rm.Model.Name,
			ReasoningEffort: rm.ReasoningEffort,
			ProviderID:      rm.Provider.ID,
			ProviderName:    rm.Provider.Name,
			APIType:         string(rm.Provider.APIType),
			Tags:            tags,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"models": result,
	})
}

func (h *VirtualModelHandler) List(w http.ResponseWriter, r *http.Request) {
	vms, err := h.vmService.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, vms)
}

func (h *VirtualModelHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	vm, err := h.vmService.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if vm == nil {
		writeError(w, http.StatusNotFound, "virtual model not found")
		return
	}

	writeJSON(w, http.StatusOK, vm)
}

func (h *VirtualModelHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req models.UpdateVirtualModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	vm, err := h.vmService.Update(r.Context(), id, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, vm)
}

func (h *VirtualModelHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	// Check for dependents before deleting
	vm, err := h.vmService.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if vm == nil {
		writeError(w, http.StatusNotFound, "virtual model not found")
		return
	}

	deps, err := h.vmService.GetDependencies(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var dependents []string
	for name, sources := range deps {
		for _, src := range sources {
			if src == vm.Name {
				dependents = append(dependents, name)
				break
			}
		}
	}

	if len(dependents) > 0 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":       "virtual model has dependents",
			"dependents":  dependents,
		})
		return
	}

	if err := h.vmService.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *VirtualModelHandler) GetResolved(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	vm, err := h.vmService.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if vm == nil {
		writeError(w, http.StatusNotFound, "virtual model not found")
		return
	}

	resolved, err := h.vmService.ResolveModels(r.Context(), vm)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var result []resolvedEntry
	for i, rm := range resolved {
		tags := make(map[string]string)
		for _, t := range rm.Model.Tags {
			tags["mc."+t.Key] = t.Value
		}
		for k, v := range rm.GlobalMetadata {
			tags["m."+k] = v
		}
		for k, v := range rm.ProviderMetadata {
			tags[k] = v
		}

		result = append(result, resolvedEntry{
			Position:        i + 1,
			ModelID:         rm.Model.ID,
			ModelName:       rm.Model.Name,
			ReasoningEffort: rm.ReasoningEffort,
			ProviderID:      rm.Provider.ID,
			ProviderName:    rm.Provider.Name,
			APIType:         string(rm.Provider.APIType),
			Tags:            tags,
		})
	}

	var filterObj interface{}
	if len(vm.FilterExpr) > 0 {
		json.Unmarshal(vm.FilterExpr, &filterObj)
	}
	var sortObj interface{}
	if len(vm.SortExpr) > 0 {
		json.Unmarshal(vm.SortExpr, &sortObj)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"virtual_model": vm.Name,
		"description":   vm.Description,
		"filter":        filterObj,
		"sort":          sortObj,
		"composition":   vm.Composition,
		"models":        result,
	})
}

func (h *VirtualModelHandler) GetDependencies(w http.ResponseWriter, r *http.Request) {
	deps, err := h.vmService.GetDependencies(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, deps)
}
