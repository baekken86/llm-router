package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type StatusRefreshMsg struct {
	Status *StatusResponse
}

type StatusClearMsg struct {
	ProviderID int64
	Err        error
}

type StatusModel struct {
	providers []ProviderStatusResponse
	width     int
	height    int
	cursor    int
}

func NewStatusModel() StatusModel {
	return StatusModel{}
}

func (m StatusModel) Update(msg tea.Msg) (StatusModel, tea.Cmd) {
	switch msg := msg.(type) {
	case StatusRefreshMsg:
		if msg.Status != nil {
			m.providers = msg.Status.Providers
		}
	case StatusClearMsg:
		if msg.Err == nil {
			for i := range m.providers {
				if m.providers[i].ID == msg.ProviderID {
					m.providers[i].RateLimited = false
					m.providers[i].RetryIn = ""
				}
			}
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.providers)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m StatusModel) View() string {
	var b strings.Builder

	b.WriteString(StatHeaderStyle.Render("Provider Status"))
	b.WriteString("\n\n")

	if len(m.providers) == 0 {
		b.WriteString(MutedStyle.Render("  No providers configured"))
		return b.String()
	}

	header := fmt.Sprintf("  %s  %-20s  %-12s  %s",
		MutedStyle.Render(""),
		InfoStyle.Render("Provider"),
		InfoStyle.Render("Status"),
		InfoStyle.Render("Details"),
	)
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(MutedStyle.Render(strings.Repeat("-", 60)))
	b.WriteString("\n")

	for i, p := range m.providers {
		cursor := "  "
		if i == m.cursor {
			cursor = SuccessStyle.Render("▸ ")
		}

		statusStr := ""
		details := ""
		if p.RateLimited {
			statusStr = ErrorStyle.Render("RATE LIMITED")
			details = WarningStyle.Render(fmt.Sprintf("retry in %s", p.RetryIn))
		} else {
			statusStr = SuccessStyle.Render("OK")
		}

		name := p.Name
		if len(name) > 20 {
			name = name[:17] + "..."
		}

		b.WriteString(fmt.Sprintf("%s%-20s  %-12s  %s\n",
			cursor,
			InfoStyle.Render(name),
			statusStr,
			details,
		))
	}

	b.WriteString("\n")
	b.WriteString(MutedStyle.Render("  press 'c' to clear rate limit for selected provider"))

	return b.String()
}

func (m *StatusModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m StatusModel) SelectedProvider() *ProviderStatusResponse {
	if m.cursor >= 0 && m.cursor < len(m.providers) {
		return &m.providers[m.cursor]
	}
	return nil
}
