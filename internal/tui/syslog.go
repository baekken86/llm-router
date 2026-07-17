package tui

import (
	"fmt"
	"strings"
	"time"
)

type SysLogEntry struct {
	Timestamp time.Time
	Level     string
	Message   string
}

type SysLogViewModel struct {
	entries []SysLogEntry
	maxSize int
	width   int
	height  int
	offset  int
	scrollX int
}

func NewSysLogViewModel(maxSize int) SysLogViewModel {
	return SysLogViewModel{
		entries: make([]SysLogEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

func (m *SysLogViewModel) AddEntry(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	entry := SysLogEntry{
		Timestamp: time.Now(),
		Level:     parseLevel(line),
		Message:   line,
	}

	m.entries = append([]SysLogEntry{entry}, m.entries...)
	if len(m.entries) > m.maxSize {
		m.entries = m.entries[:m.maxSize]
	}
}

func (m *SysLogViewModel) LoadInitial(entries []SysLogEntry) {
	for i := len(entries) - 1; i >= 0; i-- {
		m.entries = append(m.entries, entries[i])
	}
	if len(m.entries) > m.maxSize {
		m.entries = m.entries[:m.maxSize]
	}
}

func parseLevel(line string) string {
	if strings.Contains(line, "level=DEBUG") || strings.Contains(line, "level=debug") {
		return "DEBUG"
	}
	if strings.Contains(line, "level=WARN") || strings.Contains(line, "level=warn") {
		return "WARN"
	}
	if strings.Contains(line, "level=ERROR") || strings.Contains(line, "level=error") {
		return "ERROR"
	}
	return "INFO"
}

func (m SysLogViewModel) View() string {
	if len(m.entries) == 0 {
		return MutedStyle.Render("  No syslog entries yet. Waiting for application logs...")
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

func (m SysLogViewModel) renderEntry(e SysLogEntry) string {
	ts := e.Timestamp.Format("15:04:05")

	levelStyle := InfoStyle
	switch e.Level {
	case "DEBUG":
		levelStyle = MutedStyle
	case "WARN":
		levelStyle = WarningStyle
	case "ERROR":
		levelStyle = ErrorStyle
	}

	level := levelStyle.Render(fmt.Sprintf("%-5s", e.Level))

	return fmt.Sprintf("%s %s %s",
		MutedStyle.Render(ts),
		level,
		e.Message,
	)
}

func (m *SysLogViewModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *SysLogViewModel) ScrollUp() {
	if m.offset > 0 {
		m.offset--
	}
}

func (m *SysLogViewModel) ScrollDown() {
	if m.offset < len(m.entries)-1 {
		m.offset++
	}
}

func (m *SysLogViewModel) ScrollLeft() {
	m.scrollX -= 10
	if m.scrollX < 0 {
		m.scrollX = 0
	}
}

func (m *SysLogViewModel) ScrollRight() {
	m.scrollX += 10
}

func (m *SysLogViewModel) ScrollToTop() {
	m.offset = 0
	m.scrollX = 0
}

func (m *SysLogViewModel) ScrollToEnd() {
	m.offset = len(m.entries) - (m.height - 2)
	if m.offset < 0 {
		m.offset = 0
	}
	m.scrollX = 0
}
