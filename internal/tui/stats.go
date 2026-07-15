package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/chris/llm-router/internal/proxy"
)

type StatsUpdateMsg struct {
	Log proxy.RequestLog
}

type StatsResetMsg struct{}

type ModelStats struct {
	Requests      int
	Successes     int
	Failures      int
	TotalLatency  time.Duration
	InputTokens   int
	OutputTokens  int
	CachedTokens  int
	ReasoningTokens int
	ErrorByStatus map[int]int
}

type StatsModel struct {
	global      ModelStats
	byVM        map[string]*ModelStats
	byModel     map[string]*ModelStats
	width       int
	height      int
}

func NewStatsModel() StatsModel {
	return StatsModel{
		global:      ModelStats{ErrorByStatus: make(map[int]int)},
		byVM:        make(map[string]*ModelStats),
		byModel:     make(map[string]*ModelStats),
	}
}

func (m StatsModel) Update(msg tea.Msg) (StatsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case StatsUpdateMsg:
		m.updateStats(msg.Log)
	case StatsResetMsg:
		m = NewStatsModel()
		m.width = m.width
		m.height = m.height
	}
	return m, nil
}

func (m *StatsModel) updateStats(log proxy.RequestLog) {
	m.global.Requests++
	m.global.InputTokens += log.InputTokens
	m.global.OutputTokens += log.OutputTokens
	m.global.CachedTokens += log.CachedTokens
	m.global.ReasoningTokens += log.ReasoningTokens
	m.global.TotalLatency += log.Latency

	if log.StatusCode >= 200 && log.StatusCode < 300 {
		m.global.Successes++
	} else {
		m.global.Failures++
		m.global.ErrorByStatus[log.StatusCode]++
	}

	vmStats := m.getOrCreateVM(log.VirtualModel)
	vmStats.Requests++
	vmStats.InputTokens += log.InputTokens
	vmStats.OutputTokens += log.OutputTokens
	vmStats.CachedTokens += log.CachedTokens
	vmStats.TotalLatency += log.Latency
	if log.StatusCode >= 200 && log.StatusCode < 300 {
		vmStats.Successes++
	} else {
		vmStats.Failures++
		vmStats.ErrorByStatus[log.StatusCode]++
	}

	modelKey := fmt.Sprintf("%s/%s", log.ProviderName, log.ModelName)
	modelStats := m.getOrCreateModel(modelKey)
	modelStats.Requests++
	modelStats.InputTokens += log.InputTokens
	modelStats.OutputTokens += log.OutputTokens
	modelStats.CachedTokens += log.CachedTokens
	modelStats.TotalLatency += log.Latency
	if log.StatusCode >= 200 && log.StatusCode < 300 {
		modelStats.Successes++
	} else {
		modelStats.Failures++
		modelStats.ErrorByStatus[log.StatusCode]++
	}
}

func (m *StatsModel) getOrCreateVM(name string) *ModelStats {
	if m.byVM[name] == nil {
		m.byVM[name] = &ModelStats{ErrorByStatus: make(map[int]int)}
	}
	return m.byVM[name]
}

func (m *StatsModel) getOrCreateModel(key string) *ModelStats {
	if m.byModel[key] == nil {
		m.byModel[key] = &ModelStats{ErrorByStatus: make(map[int]int)}
	}
	return m.byModel[key]
}

func (m StatsModel) View() string {
	var b strings.Builder

	b.WriteString(StatHeaderStyle.Render("Global Statistics"))
	b.WriteString("\n\n")

	b.WriteString(m.renderGlobalStats())
	b.WriteString("\n")

	b.WriteString(StatHeaderStyle.Render("By Virtual Model"))
	b.WriteString("\n\n")
	b.WriteString(m.renderVMStats())
	b.WriteString("\n")

	b.WriteString(StatHeaderStyle.Render("Top Models"))
	b.WriteString("\n\n")
	b.WriteString(m.renderModelStats())

	return b.String()
}

func (m StatsModel) renderGlobalStats() string {
	var b strings.Builder

	successRate := float64(0)
	if m.global.Requests > 0 {
		successRate = float64(m.global.Successes) / float64(m.global.Requests) * 100
	}

	avgLatency := time.Duration(0)
	if m.global.Requests > 0 {
		avgLatency = m.global.TotalLatency / time.Duration(m.global.Requests)
	}

	cacheRate := float64(0)
	if m.global.InputTokens > 0 {
		cacheRate = float64(m.global.CachedTokens) / float64(m.global.InputTokens) * 100
	}

	rows := []struct {
		label string
		value string
	}{
		{"Total Requests", fmt.Sprintf("%d", m.global.Requests)},
		{"Success Rate", fmt.Sprintf("%.1f%%", successRate)},
		{"Avg Latency", avgLatency.Round(time.Millisecond).String()},
		{"Input Tokens", formatNumber(m.global.InputTokens)},
		{"Output Tokens", formatNumber(m.global.OutputTokens)},
		{"Cached Tokens", formatNumber(m.global.CachedTokens)},
		{"Cache Hit Rate", fmt.Sprintf("%.1f%%", cacheRate)},
		{"Reasoning Tokens", formatNumber(m.global.ReasoningTokens)},
	}

	for _, r := range rows {
		b.WriteString(StatLabelStyle.Render(r.label))
		b.WriteString(StatValueStyle.Render(r.value))
		b.WriteString("\n")
	}

	if len(m.global.ErrorByStatus) > 0 {
		b.WriteString("\n")
		b.WriteString(ErrorStyle.Render("Errors by Status:"))
		b.WriteString("\n")
		for code, count := range m.global.ErrorByStatus {
			b.WriteString(fmt.Sprintf("  %s: %s\n",
				WarningStyle.Render(fmt.Sprintf("%d", code)),
				ErrorStyle.Render(fmt.Sprintf("%d", count)),
			))
		}
	}

	return b.String()
}

func (m StatsModel) renderVMStats() string {
	if len(m.byVM) == 0 {
		return MutedStyle.Render("  No requests yet")
	}

	var b strings.Builder

	type vmEntry struct {
		name  string
		stats *ModelStats
	}
	var entries []vmEntry
	for name, stats := range m.byVM {
		entries = append(entries, vmEntry{name, stats})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].stats.Requests > entries[j].stats.Requests
	})

	for _, e := range entries {
		successRate := float64(0)
		if e.stats.Requests > 0 {
			successRate = float64(e.stats.Successes) / float64(e.stats.Requests) * 100
		}

		b.WriteString(fmt.Sprintf("  %s %s\n",
			InfoStyle.Render(e.name),
			MutedStyle.Render(fmt.Sprintf("(%d reqs, %.0f%% success)", e.stats.Requests, successRate)),
		))
	}

	return b.String()
}

func (m StatsModel) renderModelStats() string {
	if len(m.byModel) == 0 {
		return MutedStyle.Render("  No requests yet")
	}

	type modelEntry struct {
		key   string
		stats *ModelStats
	}
	var entries []modelEntry
	for key, stats := range m.byModel {
		entries = append(entries, modelEntry{key, stats})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].stats.Requests > entries[j].stats.Requests
	})

	limit := 5
	if len(entries) < limit {
		limit = len(entries)
	}

	var b strings.Builder
	for _, e := range entries[:limit] {
		avgLatency := time.Duration(0)
		if e.stats.Requests > 0 {
			avgLatency = e.stats.TotalLatency / time.Duration(e.stats.Requests)
		}

		b.WriteString(fmt.Sprintf("  %s %s\n",
			InfoStyle.Render(e.key),
			MutedStyle.Render(fmt.Sprintf("(%d reqs, avg %s, %s in/%s out/%s cached)",
				e.stats.Requests,
				avgLatency.Round(time.Millisecond),
				formatNumber(e.stats.InputTokens),
				formatNumber(e.stats.OutputTokens),
				formatNumber(e.stats.CachedTokens),
			)),
		))
	}

	return b.String()
}

func (m *StatsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *StatsModel) LoadFromAPI(resp *StatsResponse) {
	m.global.Requests = resp.TotalRequests
	m.global.Successes = resp.Successes
	m.global.Failures = resp.Failures
	m.global.InputTokens = resp.InputTokens
	m.global.OutputTokens = resp.OutputTokens
	m.global.CachedTokens = resp.CachedTokens

	m.byVM = make(map[string]*ModelStats)
	for name, stat := range resp.ByVirtualModel {
		m.byVM[name] = &ModelStats{
			Requests:     stat.Requests,
			Successes:    stat.Successes,
			Failures:     stat.Failures,
			InputTokens:  stat.InputTokens,
			OutputTokens: stat.OutputTokens,
			CachedTokens: stat.CachedTokens,
			ErrorByStatus: make(map[int]int),
		}
	}

	m.byModel = make(map[string]*ModelStats)
	for key, stat := range resp.ByProvider {
		m.byModel[key] = &ModelStats{
			Requests:     stat.Requests,
			Successes:    stat.Successes,
			Failures:     stat.Failures,
			InputTokens:  stat.InputTokens,
			OutputTokens: stat.OutputTokens,
			CachedTokens: stat.CachedTokens,
			ErrorByStatus: make(map[int]int),
		}
	}
}

func formatNumber(n int) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func renderStatRow(label, value string, style lipgloss.Style) string {
	return fmt.Sprintf("%s %s", StatLabelStyle.Render(label), style.Render(value))
}
