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

	for i, p := range m.providers {
		cursor := "  "
		if i == m.cursor {
			cursor = SuccessStyle.Render("▸ ")
		}

		name := p.Name
		if len(name) > 20 {
			name = name[:17] + "..."
		}

		b.WriteString(fmt.Sprintf("%s%s\n", cursor, InfoStyle.Render(name)))

		// Rate limit status
		if p.RateLimited {
			b.WriteString(fmt.Sprintf("    %s %s\n",
				ErrorStyle.Render("RATE LIMITED"),
				WarningStyle.Render(fmt.Sprintf("retry in %s", p.RetryIn)),
			))
		} else {
			b.WriteString(fmt.Sprintf("    %s\n", SuccessStyle.Render("OK")))
		}

		// Auth status
		if p.OAuthConfigured {
			authInfo := "OAuth"
			if p.OAuthEmail != "" {
				authInfo = fmt.Sprintf("OAuth (%s)", p.OAuthEmail)
			}
			if p.OAuthExpired {
				b.WriteString(fmt.Sprintf("    %s %s\n",
					ErrorStyle.Render("EXPIRED"),
					MutedStyle.Render(authInfo),
				))
			} else {
				b.WriteString(fmt.Sprintf("    %s %s\n",
					SuccessStyle.Render("configured"),
					MutedStyle.Render(authInfo),
				))
			}
			if p.OAuthExpiresAt != "" {
				b.WriteString(fmt.Sprintf("    %s %s\n",
					MutedStyle.Render("expires:"),
					MutedStyle.Render(p.OAuthExpiresAt),
				))
			}
		} else if p.APIKeyConfigured {
			b.WriteString(fmt.Sprintf("    %s\n", SuccessStyle.Render("API key configured")))
		} else {
			b.WriteString(fmt.Sprintf("    %s\n", ErrorStyle.Render("no credentials")))
		}

		if i < len(m.providers)-1 {
			b.WriteString("\n")
		}
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
