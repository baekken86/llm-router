package handlers

import (
	_ "embed"
	"encoding/json"
	"net/http"
)

type MetadataHandler struct {
	fieldsJSON []byte
}

func NewMetadataHandler(modelsJSON []byte) *MetadataHandler {
	var raw struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(modelsJSON, &raw); err != nil {
		panic("failed to parse models.json: " + err.Error())
	}
	fieldsJSON, _ := json.Marshal(raw.Fields)
	return &MetadataHandler{fieldsJSON: fieldsJSON}
}

func (h *MetadataHandler) GetFields(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(append([]byte(`{"fields":`), append(h.fieldsJSON, '}')...))
}
