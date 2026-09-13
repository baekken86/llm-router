package handlers

import (
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strconv"

	"github.com/chris/llm-router/internal/repository"
)

type MetadataHandler struct {
	fieldsJSON      []byte
	providerMetaRepo repository.ProviderMetadataRepository
	globalMetaRepo   repository.GlobalMetadataRepository
}

func NewMetadataHandler(modelsJSON []byte, providerMetaRepo repository.ProviderMetadataRepository, globalMetaRepo repository.GlobalMetadataRepository) *MetadataHandler {
	var raw struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(modelsJSON, &raw); err != nil {
		panic("failed to parse models.json: " + err.Error())
	}

	qualified := make(map[string]json.RawMessage, len(raw.Fields))
	for k, v := range raw.Fields {
		qualified["mc."+k] = v
	}

	fieldsJSON, _ := json.Marshal(qualified)
	return &MetadataHandler{
		fieldsJSON:      fieldsJSON,
		providerMetaRepo: providerMetaRepo,
		globalMetaRepo:   globalMetaRepo,
	}
}

func (h *MetadataHandler) GetFields(w http.ResponseWriter, r *http.Request) {
	// Start with mc.* fields from models.json
	var mcFields map[string]json.RawMessage
	json.Unmarshal(h.fieldsJSON, &mcFields)

	fields := make(map[string]interface{})
	for k, v := range mcFields {
		fields[k] = v
	}

	// Add p.name and m.name as known fields
	fields["p.name"] = map[string]interface{}{
		"description": "Provider name",
		"type":        "string",
	}
	fields["m.name"] = map[string]interface{}{
		"description": "Model name",
		"type":        "string",
	}

	// Add m.* fields (mirror mc.* with m. prefix)
	if h.globalMetaRepo != nil {
		if keys, err := h.globalMetaRepo.ListAllKeys(r.Context()); err == nil {
			for _, key := range keys {
				mKey := "m." + key
				if _, exists := fields[mKey]; !exists {
					// Try to find type definition from mc.* counterpart
					if mcDef, ok := fields["mc."+key]; ok {
						fields[mKey] = mcDef
					} else {
						fields[mKey] = map[string]interface{}{
							"description": "Global model metadata: " + key,
							"type":        inferType(key),
						}
					}
				}
			}
		}
	}

	// Add p.* fields from provider metadata
	if h.providerMetaRepo != nil {
		if allKeys, err := h.providerMetaRepo.ListAllKeys(r.Context()); err == nil {
			for key, values := range allKeys {
				pKey := "p." + key
			if _, exists := fields[pKey]; !exists {
				def := map[string]interface{}{
					"description": "Provider metadata: " + key,
					"type":        inferProviderFieldType(values),
				}
				if len(values) > 0 {
					// sorted copy for stable output
					vals := append([]string(nil), values...)
					sort.Strings(vals)
					def["values"] = vals
				}
				fields[pKey] = def
			}
			}
		}
	}

	data, _ := json.Marshal(fields)
	w.Header().Set("Content-Type", "application/json")
	w.Write(append([]byte(`{"fields":`), append(data, '}')...))
}

func (h *MetadataHandler) ListModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.globalMetaRepo.ListModels(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models)
}

type modelMetadataEntry struct {
	ModelName   string            `json:"model_name"`
	ReasoningEffort string        `json:"reasoning_effort"`
	Metadata    map[string]string `json:"metadata"`
}

func (h *MetadataHandler) ListEntries(w http.ResponseWriter, r *http.Request) {
	all, err := h.globalMetaRepo.GetAll(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var entries []modelMetadataEntry
	for modelName, efforts := range all {
		for effort, metadata := range efforts {
			entries = append(entries, modelMetadataEntry{
				ModelName:       modelName,
				ReasoningEffort: effort,
				Metadata:        metadata,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

func (h *MetadataHandler) SetEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ModelName       string            `json:"model_name"`
		ReasoningEffort string            `json:"reasoning_effort"`
		Metadata        map[string]string `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.ModelName == "" {
		http.Error(w, "model_name is required", http.StatusBadRequest)
		return
	}

	if len(req.Metadata) == 0 {
		http.Error(w, "metadata must not be empty", http.StatusBadRequest)
		return
	}

	if err := h.globalMetaRepo.Set(r.Context(), req.ModelName, req.ReasoningEffort, req.Metadata); err != nil {
		log.Printf("SetEntry error: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(modelMetadataEntry{
		ModelName:       req.ModelName,
		ReasoningEffort: req.ReasoningEffort,
		Metadata:        req.Metadata,
	})
}

func inferType(key string) string {
	switch key {
	case "intelligence", "hallucination", "coding", "speed", "latency",
		"context_window", "cost_per_1m_input", "cost_per_1m_output",
		"cost_per_1m_cache", "cost_per_task", "reasoning":
		return "number"
	case "cost_type":
		return "string"
	case "has_reasoning_effort":
		return "boolean"
	default:
		return "string"
	}
}

func inferProviderFieldType(values []string) string {
	if len(values) == 0 {
		return "string"
	}
	// Check if all values parse as numbers
	allNumeric := true
	for _, v := range values {
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			allNumeric = false
			break
		}
	}
	if allNumeric {
		return "number"
	}
	// Check if it looks like a boolean
	if len(values) <= 2 {
		boolish := true
		for _, v := range values {
			if v != "true" && v != "false" {
				boolish = false
				break
			}
		}
		if boolish {
			return "boolean"
		}
	}
	return "string"
}
