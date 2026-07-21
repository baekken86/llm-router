package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/chris/llm-router/internal/proxy"
	"github.com/chris/llm-router/internal/repository"
)

type StatsData struct {
	TotalRequests      int                     `json:"total_requests"`
	Successes          int                     `json:"successes"`
	Failures           int                     `json:"failures"`
	InputTokens        int                     `json:"input_tokens"`
	OutputTokens       int                     `json:"output_tokens"`
	CachedTokens       int                     `json:"cached_tokens"`
	ReasoningTokens    int                     `json:"reasoning_tokens"`
	RTKIntercepts      int                     `json:"rtk_intercepts"`
	RTKSavedTokens     int                     `json:"rtk_saved_tokens"`
	CavemanIntercepts  int                     `json:"caveman_intercepts"`
	CavemanSavedTokens int                     `json:"caveman_saved_tokens"`
	ByVirtualModel     map[string]*ModelStat   `json:"by_virtual_model"`
	ByProvider         map[string]*ModelStat   `json:"by_provider"`
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

	logRepo  *repository.LogRepository
	logger   *slog.Logger

	clients    map[chan proxy.RequestLog]bool
	clientsMu  sync.Mutex
}

func NewStatsHandler(logRepo *repository.LogRepository, logger *slog.Logger) *StatsHandler {
	h := &StatsHandler{
		stats: StatsData{
			ByVirtualModel: make(map[string]*ModelStat),
			ByProvider:     make(map[string]*ModelStat),
		},
		logs:     make([]proxy.RequestLog, 0, 1000),
		maxLogs:  1000,
		logRepo:  logRepo,
		logger:   logger,
		clients:  make(map[chan proxy.RequestLog]bool),
	}
	h.loadFromDB()
	return h
}

func (h *StatsHandler) loadFromDB() {
	if h.logRepo == nil {
		return
	}
	ctx := context.Background()

	dbLogs, err := h.logRepo.ListRequestLogs(ctx, h.maxLogs)
	if err != nil {
		h.logger.Warn("failed to load logs from db", "error", err)
		return
	}

	h.logs = make([]proxy.RequestLog, 0, len(dbLogs))
	for _, dl := range dbLogs {
		h.logs = append(h.logs, proxy.RequestLog{
			Type:               dl.Type,
			Timestamp:          dl.Timestamp,
			RequestID:          dl.RequestID,
			VirtualModel:       dl.VirtualModel,
			ClientKeyID:        dl.ClientKeyID,
			ProviderName:       dl.ProviderName,
			ModelName:          dl.ModelName,
			StatusCode:         dl.StatusCode,
			Latency:            dl.Latency,
			InputTokens:        dl.InputTokens,
			OutputTokens:       dl.OutputTokens,
			CachedTokens:       dl.CachedTokens,
			ReasoningTokens:    dl.ReasoningTokens,
			ErrorMessage:       dl.ErrorMessage,
			RetryCount:         dl.RetryCount,
			FallbackCount:      dl.FallbackCount,
			RTKIntercepted:     dl.RTKIntercepted,
			RTKSavedTokens:     dl.RTKSavedTokens,
			CavemanIntercepted: dl.CavemanIntercepted,
			CavemanSavedTokens: dl.CavemanSavedTokens,
		})
	}

	dbStats, err := h.logRepo.ComputeStats(ctx)
	if err != nil {
		h.logger.Warn("failed to compute stats from db", "error", err)
		return
	}

	h.stats.TotalRequests = dbStats.TotalRequests
	h.stats.Successes = dbStats.Successes
	h.stats.Failures = dbStats.Failures
	h.stats.InputTokens = dbStats.InputTokens
	h.stats.OutputTokens = dbStats.OutputTokens
	h.stats.CachedTokens = dbStats.CachedTokens
	h.stats.ReasoningTokens = dbStats.ReasoningTokens
	h.stats.RTKIntercepts = dbStats.RTKIntercepts
	h.stats.RTKSavedTokens = dbStats.RTKSavedTokens
	h.stats.CavemanIntercepts = dbStats.CavemanIntercepts
	h.stats.CavemanSavedTokens = dbStats.CavemanSavedTokens

	h.stats.ByVirtualModel = make(map[string]*ModelStat)
	for name, ms := range dbStats.ByVirtualModel {
		h.stats.ByVirtualModel[name] = &ModelStat{
			Requests:     ms.Requests,
			Successes:    ms.Successes,
			Failures:     ms.Failures,
			InputTokens:  ms.InputTokens,
			OutputTokens: ms.OutputTokens,
			CachedTokens: ms.CachedTokens,
		}
	}

	h.stats.ByProvider = make(map[string]*ModelStat)
	for name, ms := range dbStats.ByProvider {
		h.stats.ByProvider[name] = &ModelStat{
			Requests:     ms.Requests,
			Successes:    ms.Successes,
			Failures:     ms.Failures,
			InputTokens:  ms.InputTokens,
			OutputTokens: ms.OutputTokens,
			CachedTokens: ms.CachedTokens,
		}
	}

	h.logger.Info("loaded stats from db", "requests", h.stats.TotalRequests, "logs", len(h.logs))
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

	if log.Type == "proxy" && log.Status == "streaming" {
		for i, existing := range h.logs {
			if existing.Type == "proxy" && existing.RequestID == log.RequestID &&
				existing.ProviderName == log.ProviderName && existing.ModelName == log.ModelName &&
				existing.Status == "streaming" {
				h.logs[i] = log
				h.clientsMu.Lock()
				for ch := range h.clients {
					select {
					case ch <- log:
					default:
					}
				}
				h.clientsMu.Unlock()
				return
			}
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
		return
	}

	if log.Type == "proxy" && log.Status == "completed" {
		for i, existing := range h.logs {
			if existing.Type == "proxy" && existing.RequestID == log.RequestID &&
				existing.ProviderName == log.ProviderName && existing.ModelName == log.ModelName &&
				existing.Status == "streaming" {
				h.logs[i] = log
				break
			}
		}
	}

	if log.Type == "proxy" && log.Status != "streaming" {
		found := false
		for _, existing := range h.logs {
			if existing.Type == "proxy" && existing.RequestID == log.RequestID &&
				existing.ProviderName == log.ProviderName && existing.ModelName == log.ModelName &&
				existing.Status == "completed" {
				found = true
				break
			}
		}
		if !found && log.Status != "streaming" {
			h.logs = append([]proxy.RequestLog{log}, h.logs...)
			if len(h.logs) > h.maxLogs {
				h.logs = h.logs[:h.maxLogs]
			}
		}
	}

	if log.Type != "proxy" {
		h.logs = append([]proxy.RequestLog{log}, h.logs...)
		if len(h.logs) > h.maxLogs {
			h.logs = h.logs[:h.maxLogs]
		}
	}

	if log.Type == "proxy" && log.Status != "streaming" {
		h.stats.TotalRequests++
		h.stats.InputTokens += log.InputTokens
		h.stats.OutputTokens += log.OutputTokens
		h.stats.CachedTokens += log.CachedTokens
		h.stats.ReasoningTokens += log.ReasoningTokens

		if log.RTKIntercepted {
			h.stats.RTKIntercepts++
		}
		h.stats.RTKSavedTokens += log.RTKSavedTokens

		if log.CavemanIntercepted {
			h.stats.CavemanIntercepts++
		}
		h.stats.CavemanSavedTokens += log.CavemanSavedTokens

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
	}

	h.clientsMu.Lock()
	for ch := range h.clients {
		select {
		case ch <- log:
		default:
		}
	}
	h.clientsMu.Unlock()

	if h.logRepo != nil && log.Type == "proxy" && log.Status != "streaming" {
		dbLog := repository.RequestLog{
			Type:               log.Type,
			Timestamp:          log.Timestamp,
			RequestID:          log.RequestID,
			VirtualModel:       log.VirtualModel,
			ClientKeyID:        log.ClientKeyID,
			ProviderName:       log.ProviderName,
			ModelName:          log.ModelName,
			StatusCode:         log.StatusCode,
			Latency:            log.Latency,
			InputTokens:        log.InputTokens,
			OutputTokens:       log.OutputTokens,
			CachedTokens:       log.CachedTokens,
			ReasoningTokens:    log.ReasoningTokens,
			ErrorMessage:       log.ErrorMessage,
			RetryCount:         log.RetryCount,
			FallbackCount:      log.FallbackCount,
			RTKIntercepted:     log.RTKIntercepted,
			RTKSavedTokens:     log.RTKSavedTokens,
			CavemanIntercepted: log.CavemanIntercepted,
			CavemanSavedTokens: log.CavemanSavedTokens,
		}
		if err := h.logRepo.InsertRequestLog(context.Background(), dbLog); err != nil {
			h.logger.Warn("failed to persist request log", "error", err)
		}
	}
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
