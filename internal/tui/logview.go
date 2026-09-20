package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/chris/llm-router/internal/proxy"
)

type LogEntry struct {
	LogType         string
	Status          string
	Timestamp       time.Time
	RequestID       string
	VirtualModel    string
	ProviderName    string
	ModelName       string
	StatusCode      int
	Latency         time.Duration
	InputTokens     int
	OutputTokens    int
	CachedTokens    int
	ReasoningEffort string
	ErrorMessage    string
	Fallback        int
	Retry           int
	Children        []*LogEntry
}

type LogViewModel struct {
	entries []*LogEntry
	maxSize int
	width   int
	height  int
	offset  int
	scrollX int
}

func NewLogViewModel(maxSize int) LogViewModel {
	return LogViewModel{
		entries: make([]*LogEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

func logEntryFromProxy(l proxy.RequestLog) LogEntry {
	return LogEntry{
		LogType:         l.Type,
		Status:          l.Status,
		Timestamp:       l.Timestamp,
		RequestID:       l.RequestID,
		VirtualModel:    l.VirtualModel,
		ProviderName:    l.ProviderName,
		ModelName:       l.ModelName,
		StatusCode:      l.StatusCode,
		Latency:         l.Latency,
		InputTokens:     l.InputTokens,
		OutputTokens:    l.OutputTokens,
		CachedTokens:    l.CachedTokens,
		ReasoningEffort: l.ReasoningEffort,
		ErrorMessage:    l.ErrorMessage,
		Fallback:        l.FallbackCount,
		Retry:           l.RetryCount,
	}
}

func (m *LogViewModel) AddEntry(log proxy.RequestLog) {
	if log.Type == "incoming" {
		entry := logEntryFromProxy(log)
		entry.Children = make([]*LogEntry, 0)
		m.entries = append([]*LogEntry{&entry}, m.entries...)
		if len(m.entries) > m.maxSize {
			m.entries = m.entries[:m.maxSize]
		}
		return
	}

	// Incoming rows become parents; proxy/attempt rows become children of
	// their request's parent entry. Child slots are keyed by
	// (provider, model): a result row supersedes a pending "sent" attempt
	// row, and an attempt never overwrites an existing result.
	for _, parent := range m.entries {
		if parent.RequestID == log.RequestID {
			child := logEntryFromProxy(log)
			for i, c := range parent.Children {
				if c.ProviderName == log.ProviderName && c.ModelName == log.ModelName {
					if c.LogType != "attempt" && log.Type == "attempt" {
						return
					}
					parent.Children[i] = &child
					return
				}
			}
			parent.Children = append(parent.Children, &child)
			return
		}
	}

	entry := logEntryFromProxy(log)
	entry.Children = make([]*LogEntry, 0)
	m.entries = append([]*LogEntry{&entry}, m.entries...)
	if len(m.entries) > m.maxSize {
		m.entries = m.entries[:m.maxSize]
	}
}

func (m *LogViewModel) LoadInitial(logs []proxy.RequestLog) {
	byReqID := make(map[string]*LogEntry)
	var order []string

	// Child slots are keyed by (provider, model): history can contain both
	// the "sent" attempt row and the result row for one dispatch, and the
	// result must win the slot (attempt kept only when no result exists).
	findSlot := func(e *LogEntry, l proxy.RequestLog) int {
		for i, c := range e.Children {
			if c.ProviderName == l.ProviderName && c.ModelName == l.ModelName {
				return i
			}
		}
		return -1
	}

	for i := len(logs) - 1; i >= 0; i-- {
		l := logs[i]
		if l.Type == "incoming" {
			entry := logEntryFromProxy(l)
			entry.Children = make([]*LogEntry, 0)
			byReqID[l.RequestID] = &entry
			order = append(order, l.RequestID)
		} else {
			child := logEntryFromProxy(l)
			if parent, ok := byReqID[l.RequestID]; ok {
				idx := findSlot(parent, l)
				if idx >= 0 {
					if parent.Children[idx].LogType != "attempt" && l.Type == "attempt" {
						continue
					}
					parent.Children[idx] = &child
				} else {
					parent.Children = append(parent.Children, &child)
				}
			} else {
				entry := LogEntry{
					LogType:      "proxy",
					Status:       l.Status,
					Timestamp:    l.Timestamp,
					RequestID:    l.RequestID,
					VirtualModel: l.VirtualModel,
				}
				entry.Children = []*LogEntry{&child}
				byReqID[l.RequestID] = &entry
				order = append(order, l.RequestID)
			}
		}
	}

	m.entries = make([]*LogEntry, 0, len(order))
	for _, id := range order {
		m.entries = append(m.entries, byReqID[id])
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

	maxLines := m.height - 2
	if maxLines < 1 {
		maxLines = 10
	}

	linesWritten := 0

	for i := m.offset; i < len(m.entries) && linesWritten < maxLines; i++ {
		entry := m.entries[i]

		line := m.renderParent(entry)
		if m.scrollX > 0 {
			line = trimLeftAnsi(line, m.scrollX)
		}
		b.WriteString(line)
		b.WriteString("\n")
		linesWritten++

		for ci, child := range entry.Children {
			if linesWritten >= maxLines {
				break
			}
			isLast := ci == len(entry.Children)-1
			cLine := m.renderChild(child, isLast)
			if m.scrollX > 0 {
				cLine = trimLeftAnsi(cLine, m.scrollX)
			}
			b.WriteString(cLine)
			b.WriteString("\n")
			linesWritten++
		}
	}

	return b.String()
}

func (m LogViewModel) renderParent(e *LogEntry) string {
	ts := e.Timestamp.Format("15:04:05")

	hasStreaming := false
	allCompleted := len(e.Children) > 0
	hasFailed := false
	for _, c := range e.Children {
		if c.LogType == "attempt" {
			// Attempt rows are progress markers (dispatch / pre-dispatch
			// skips). The final result row drives the parent dot state.
			continue
		}
		if c.Status == "streaming" {
			hasStreaming = true
			allCompleted = false
		} else if c.StatusCode >= 400 || c.Status == "failed" {
			hasFailed = true
		} else if c.Status != "completed" && c.StatusCode == 0 {
			allCompleted = false
		}
	}

	dotStyle := MutedStyle
	dot := "○"
	if hasStreaming {
		dotStyle = InfoStyle
		dot = "●"
	} else if hasFailed {
		dotStyle = ErrorStyle
		dot = "●"
	} else if allCompleted {
		dotStyle = SuccessStyle
		dot = "●"
	}

	vm := truncate(e.VirtualModel, 30)

	return fmt.Sprintf("%s %s %s",
		MutedStyle.Render(ts),
		dotStyle.Render(dot),
		dotStyle.Render(vm),
	)
}

func (m LogViewModel) renderChild(e *LogEntry, isLast bool) string {
	ts := e.Timestamp.Format("15:04:05")
	connector := "├─"
	if isLast {
		connector = "└─"
	}

	// Attempt rows are written at dispatch time, before any response exists.
	// A status-0 attempt renders as a neutral "sent" row; skip attempts
	// (provider rate limited / disabled / no key) carry a real status code
	// and fall through to the normal status rendering below.
	if e.LogType == "attempt" && e.StatusCode == 0 {
		provider := MutedStyle.Render(truncate(e.ProviderName, 12))
		model := truncate(e.ModelName, 20)
		elapsed := MutedStyle.Render(fmt.Sprintf("%6s", e.Latency.Round(time.Second)))
		return fmt.Sprintf("  %s %s %s %s %s %s %s",
			MutedStyle.Render(ts),
			MutedStyle.Render(connector),
			MutedStyle.Render(truncate("sent", 9)),
			provider,
			model,
			elapsed,
			MutedStyle.Render("waiting..."),
		)
	}

	if e.Status == "streaming" {
		provider := MutedStyle.Render(truncate(e.ProviderName, 12))
		model := truncate(e.ModelName, 20)
		effort := ""
		if e.ReasoningEffort != "" {
			effort = MutedStyle.Render(fmt.Sprintf("[%s]", e.ReasoningEffort))
		}
		elapsed := MutedStyle.Render(fmt.Sprintf("%6s", e.Latency.Round(time.Second)))
		tokens := formatStreamingTokens(e)
		return fmt.Sprintf("  %s %s %s %-20s %s %s %s %s",
			MutedStyle.Render(ts),
			MutedStyle.Render(connector),
			InfoStyle.Render(truncate("streaming", 9)),
			provider,
			model,
			effort,
			elapsed,
			MutedStyle.Render(tokens),
		)
	}

	statusStyle := SuccessStyle
	if e.StatusCode >= 400 {
		statusStyle = ErrorStyle
	} else if e.StatusCode >= 300 {
		statusStyle = WarningStyle
	}

	status := statusStyle.Render(fmt.Sprintf("%3d", e.StatusCode))
	provider := MutedStyle.Render(truncate(e.ProviderName, 12))
	model := truncate(e.ModelName, 20)
	effort := ""
	if e.ReasoningEffort != "" {
		effort = MutedStyle.Render(fmt.Sprintf("[%s]", e.ReasoningEffort))
	}
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

	return fmt.Sprintf("  %s %s %s %s %-20s %s %s %s%s",
		MutedStyle.Render(ts),
		MutedStyle.Render(connector),
		status,
		provider,
		model,
		effort,
		latency,
		MutedStyle.Render(tokens),
		extra,
	)
}

func formatStreamingTokens(e *LogEntry) string {
	parts := []string{}
	if e.InputTokens > 0 {
		parts = append(parts, fmt.Sprintf("%s in", formatNumber(e.InputTokens)))
	}
	if e.OutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("~%s out", formatNumber(e.OutputTokens)))
	}
	if e.CachedTokens > 0 {
		parts = append(parts, fmt.Sprintf("%s cached", formatNumber(e.CachedTokens)))
	}
	if len(parts) == 0 {
		return "waiting..."
	}
	return strings.Join(parts, "/")
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
