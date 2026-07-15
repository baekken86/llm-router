package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/chris/llm-router/internal/proxy"
)

type StatsData struct {
	TotalRequests int            `json:"total_requests"`
	Successes     int            `json:"successes"`
	Failures      int            `json:"failures"`
	InputTokens   int            `json:"input_tokens"`
	OutputTokens  int            `json:"output_tokens"`
	CachedTokens  int            `json:"cached_tokens"`
	ByVirtualModel map[string]*ModelStat `json:"by_virtual_model"`
	ByProvider    map[string]*ModelStat  `json:"by_provider"`
}

type ModelStat struct {
	Requests     int `json:"requests"`
	Successes    int `json:"successes"`
	Failures     int `json:"failures"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	CachedTokens int `json:"cached_tokens"`
}

type StatsHandler struct {
	mu    sync.RWMutex
	stats StatsData
	logs  []proxy.RequestLog
	maxLogs int

	clients    map[chan proxy.RequestLog]bool
	clientsMu  sync.Mutex
}

func NewStatsHandler() *StatsHandler {
	return &StatsHandler{
		stats: StatsData{
			ByVirtualModel: make(map[string]*ModelStat),
			ByProvider:     make(map[string]*ModelStat),
		},
		logs:     make([]proxy.RequestLog, 0, 1000),
		maxLogs:  1000,
		clients:  make(map[chan proxy.RequestLog]bool),
	}
}

func (h *StatsHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.GetStats)
	r.Get("/logs", h.GetLogs)
	r.Get("/logs/stream", h.StreamLogs)
	return r
}

func (h *StatsHandler) RecordLog(log proxy.RequestLog) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.stats.TotalRequests++
	h.stats.InputTokens += log.InputTokens
	h.stats.OutputTokens += log.OutputTokens
	h.stats.CachedTokens += log.CachedTokens

	if log.StatusCode >= 200 && log.StatusCode < 300 {
		h.stats.Successes++
	} else {
		h.stats.Failures++
	}

	vm := h.getOrCreateVM(log.VirtualModel)
	vm.Requests++
	vm.InputTokens += log.InputTokens
	vm.OutputTokens += log.OutputTokens
	vm.CachedTokens += log.CachedTokens
	if log.StatusCode >= 200 && log.StatusCode < 300 {
		vm.Successes++
	} else {
		vm.Failures++
	}

	providerKey := log.ProviderName + "/" + log.ModelName
	prov := h.getOrCreateProvider(providerKey)
	prov.Requests++
	prov.InputTokens += log.InputTokens
	prov.OutputTokens += log.OutputTokens
	prov.CachedTokens += log.CachedTokens
	if log.StatusCode >= 200 && log.StatusCode < 300 {
		prov.Successes++
	} else {
		prov.Failures++
	}

	h.logs = append([]proxy.RequestLog{log}, h.logs...)
	if len(h.logs) > h.maxLogs {
		h.logs = h.logs[:h.maxLogs]
	}

	h.clientsMu.Lock()
	for ch := range h.clients {
		select {
		case ch <- log:
		default:
		}
	}
	h.clientsMu.Unlock()
}

func (h *StatsHandler) getOrCreateVM(name string) *ModelStat {
	if h.stats.ByVirtualModel[name] == nil {
		h.stats.ByVirtualModel[name] = &ModelStat{}
	}
	return h.stats.ByVirtualModel[name]
}

func (h *StatsHandler) getOrCreateProvider(key string) *ModelStat {
	if h.stats.ByProvider[key] == nil {
		h.stats.ByProvider[key] = &ModelStat{}
	}
	return h.stats.ByProvider[key]
}

func (h *StatsHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.stats)
}

func (h *StatsHandler) GetLogs(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.logs)
}

func (h *StatsHandler) StreamLogs(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan proxy.RequestLog, 50)
	h.clientsMu.Lock()
	h.clients[ch] = true
	h.clientsMu.Unlock()

	defer func() {
		h.clientsMu.Lock()
		delete(h.clients, ch)
		h.clientsMu.Unlock()
	}()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case log := <-ch:
			data, _ := json.Marshal(log)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
