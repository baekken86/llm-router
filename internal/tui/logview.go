package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/chris/llm-router/internal/proxy"
)

type LogEntry struct {
	LogType      string
	Timestamp    time.Time
	RequestID    string
	VirtualModel string
	ProviderName string
	ModelName    string
	StatusCode   int
	Latency      time.Duration
	InputTokens  int
	OutputTokens int
	CachedTokens int
	ErrorMessage string
	Fallback     int
	Retry        int
}

type LogViewModel struct {
	entries []LogEntry
	maxSize int
	width   int
	height  int
	offset  int
	scrollX int
}

func NewLogViewModel(maxSize int) LogViewModel {
	return LogViewModel{
		entries: make([]LogEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

func (m *LogViewModel) AddEntry(log proxy.RequestLog) {
	entry := LogEntry{
		LogType:      log.Type,
		Timestamp:    log.Timestamp,
		RequestID:    log.RequestID,
		VirtualModel: log.VirtualModel,
		ProviderName: log.ProviderName,
		ModelName:    log.ModelName,
		StatusCode:   log.StatusCode,
		Latency:      log.Latency,
		InputTokens:  log.InputTokens,
		OutputTokens: log.OutputTokens,
		CachedTokens: log.CachedTokens,
		ErrorMessage: log.ErrorMessage,
		Fallback:     log.FallbackCount,
		Retry:        log.RetryCount,
	}

	m.entries = append([]LogEntry{entry}, m.entries...)
	if len(m.entries) > m.maxSize {
		m.entries = m.entries[:m.maxSize]
	}
}

func (m *LogViewModel) LoadInitial(logs []proxy.RequestLog) {
	for i := len(logs) - 1; i >= 0; i-- {
		entry := LogEntry{
			LogType:      logs[i].Type,
			Timestamp:    logs[i].Timestamp,
			RequestID:    logs[i].RequestID,
			VirtualModel: logs[i].VirtualModel,
			ProviderName: logs[i].ProviderName,
			ModelName:    logs[i].ModelName,
			StatusCode:   logs[i].StatusCode,
			Latency:      logs[i].Latency,
			InputTokens:  logs[i].InputTokens,
			OutputTokens: logs[i].OutputTokens,
			CachedTokens: logs[i].CachedTokens,
			ErrorMessage: logs[i].ErrorMessage,
			Fallback:     logs[i].FallbackCount,
			Retry:        logs[i].RetryCount,
		}
		m.entries = append(m.entries, entry)
	}
	if len(m.entries) > m.maxSize {
		m.entries = m.entries[:m.maxSize]
	}
}

func (m LogViewModel) View() string {
	if len(m.entries) == 0 {
		return MutedStyle.Render("  No requests yet. Waiting for traffic...")
	}

	var b strings.Builder

	visible := m.height - 2
	if visible < 1 {
		visible = 10
	}

	end := m.offset + visible
	if end > len(m.entries) {
		end = len(m.entries)
	}

	for _, entry := range m.entries[m.offset:end] {
		line := m.renderEntry(entry)
		if m.scrollX > 0 {
			line = trimLeftAnsi(line, m.scrollX)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

func (m LogViewModel) renderEntry(e LogEntry) string {
	ts := e.Timestamp.Format("15:04:05")

	if e.LogType == "incoming" {
		prefix := InfoStyle.Render("▸")
		vm := InfoStyle.Render(truncate(e.VirtualModel, 20))
		return fmt.Sprintf("%s %s %s incoming %s",
			MutedStyle.Render(ts),
			prefix,
			vm,
			MutedStyle.Render(truncate(e.RequestID, 20)),
		)
	}

	statusStyle := SuccessStyle
	if e.StatusCode >= 400 {
		statusStyle = ErrorStyle
	} else if e.StatusCode >= 300 {
		statusStyle = WarningStyle
	}

	status := statusStyle.Render(fmt.Sprintf("%3d", e.StatusCode))

	prefix := MutedStyle.Render("↪")
	vm := InfoStyle.Render(truncate(e.VirtualModel, 20))
	provider := MutedStyle.Render(truncate(e.ProviderName, 12))
	model := truncate(e.ModelName, 20)

	latency := MutedStyle.Render(fmt.Sprintf("%6s", e.Latency.Round(time.Millisecond)))

	tokens := fmt.Sprintf("%s in/%s out/%s cached",
		formatNumber(e.InputTokens),
		formatNumber(e.OutputTokens),
		formatNumber(e.CachedTokens),
	)

	extra := ""
	if e.Fallback > 0 {
		extra += WarningStyle.Render(fmt.Sprintf(" fb=%d", e.Fallback))
	}
	if e.Retry > 0 {
		extra += WarningStyle.Render(fmt.Sprintf(" retry=%d", e.Retry))
	}
	if e.ErrorMessage != "" {
		extra += " " + ErrorStyle.Render(truncate(e.ErrorMessage, 40))
	}

	return fmt.Sprintf("%s %s %s %s/%s %-20s %s %s%s",
		MutedStyle.Render(ts),
		prefix,
		status,
		provider,
		vm,
		model,
		latency,
		MutedStyle.Render(tokens),
		extra,
	)
}

func (m *LogViewModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *LogViewModel) ScrollUp() {
	if m.offset > 0 {
		m.offset--
	}
}

func (m *LogViewModel) ScrollDown() {
	if m.offset < len(m.entries)-1 {
		m.offset++
	}
}

func (m *LogViewModel) ScrollLeft() {
	m.scrollX -= 10
	if m.scrollX < 0 {
		m.scrollX = 0
	}
}

func (m *LogViewModel) ScrollRight() {
	m.scrollX += 10
}

func (m *LogViewModel) ScrollToTop() {
	m.offset = 0
	m.scrollX = 0
}

func (m *LogViewModel) ScrollToEnd() {
	m.offset = len(m.entries) - (m.height - 2)
	if m.offset < 0 {
		m.offset = 0
	}
	m.scrollX = 0
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s + strings.Repeat(" ", max-len(s))
	}
	return s[:max-1] + "~"
}

func trimLeftAnsi(s string, n int) string {
	if n <= 0 {
		return s
	}
	i := 0
	vis := 0
	for i < len(s) && vis < n {
		if s[i] == '\x1b' {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			if i < len(s) {
				i++
			}
		} else {
			i++
			vis++
		}
	}
	return s[i:]
}
