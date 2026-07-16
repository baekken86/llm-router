package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/chris/llm-router/internal/config"
)

type SettingsModel struct {
	settings    config.Settings
	config      *config.Config
	selected    int
	width       int
	height      int
}

type SettingsSavedMsg struct{}

func NewSettingsModel(cfg *config.Config) SettingsModel {
	return SettingsModel{
		settings: cfg.Get(),
		config:   cfg,
		selected: 0,
	}
}

func (m SettingsModel) Init() tea.Cmd {
	return nil
}

func (m SettingsModel) Update(msg tea.Msg) (SettingsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < 5 {
				m.selected++
			}
		case "enter", " ":
			switch m.selected {
			case 0:
				m.settings.RTKEnabled = !m.settings.RTKEnabled
				m.config.SetRTK(m.settings.RTKEnabled)
			case 1:
				m.settings.CavemanEnabled = !m.settings.CavemanEnabled
				m.config.SetCaveman(m.settings.CavemanEnabled)
			case 2:
				m.settings.LogLevel = cycleLogLevel(m.settings.LogLevel)
				m.config.SetLogLevel(m.settings.LogLevel)
			case 3:
				m.settings.MaxRetries = cycleRetries(m.settings.MaxRetries)
				m.config.SetMaxRetries(m.settings.MaxRetries)
			case 4:
				m.settings.TimeoutSeconds = cycleTimeout(m.settings.TimeoutSeconds)
				m.config.SetTimeout(m.settings.TimeoutSeconds)
			case 5:
				m.settings.MaxTokens = cycleMaxTokens(m.settings.MaxTokens)
				m.config.SetMaxTokens(m.settings.MaxTokens)
			}
			return m, nil
		case "left", "-":
			switch m.selected {
			case 3:
				if m.settings.MaxRetries > 0 {
					m.settings.MaxRetries--
					m.config.SetMaxRetries(m.settings.MaxRetries)
				}
			case 4:
				if m.settings.TimeoutSeconds > 30 {
					m.settings.TimeoutSeconds -= 30
					m.config.SetTimeout(m.settings.TimeoutSeconds)
				}
			case 5:
				if m.settings.MaxTokens > 1024 {
					m.settings.MaxTokens -= 1024
					m.config.SetMaxTokens(m.settings.MaxTokens)
				}
			}
			return m, nil
		case "right", "+":
			switch m.selected {
			case 3:
				if m.settings.MaxRetries < 10 {
					m.settings.MaxRetries++
					m.config.SetMaxRetries(m.settings.MaxRetries)
				}
			case 4:
				if m.settings.TimeoutSeconds < 600 {
					m.settings.TimeoutSeconds += 30
					m.config.SetTimeout(m.settings.TimeoutSeconds)
				}
			case 5:
				if m.settings.MaxTokens < 32768 {
					m.settings.MaxTokens += 1024
					m.config.SetMaxTokens(m.settings.MaxTokens)
				}
			}
			return m, nil
		}
	}
	return m, nil
}

func (m SettingsModel) View() string {
	var b strings.Builder

	b.WriteString(StatHeaderStyle.Render("Settings"))
	b.WriteString("\n\n")

	toggleItems := []struct {
		label   string
		enabled bool
	}{
		{"RTK Token Compression (bash tool output)", m.settings.RTKEnabled},
		{"Caveman Output Compression (model response)", m.settings.CavemanEnabled},
	}

	for i, item := range toggleItems {
		status := ErrorStyle.Render("OFF")
		if item.enabled {
			status = InfoStyle.Render("ON ")
		}

		cursor := "  "
		if i == m.selected {
			cursor = "> "
		}

		row := fmt.Sprintf("%s%s: %s", cursor, item.label, status)
		b.WriteString(row)
		b.WriteString("\n")
	}

	b.WriteString("\n")

	b.WriteString(HelpStyle.Render("  ────────────────────────────────────"))
	b.WriteString("\n\n")

	valueItems := []struct {
		label string
		value string
	}{
		{"Log Level", m.settings.LogLevel},
		{"Max Retries", strconv.Itoa(m.settings.MaxRetries)},
		{"Timeout", fmt.Sprintf("%ds", m.settings.TimeoutSeconds)},
		{"Max Tokens", strconv.Itoa(m.settings.MaxTokens)},
	}

	for i, item := range valueItems {
		cursor := "  "
		if i+2 == m.selected {
			cursor = "> "
		}

		row := fmt.Sprintf("%s%s: %s", cursor, item.label, InfoStyle.Render(item.value))
		b.WriteString(row)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(HelpStyle.Render("↑/↓: select  enter/space: toggle  ←/→: adjust value"))
	b.WriteString("\n")
	b.WriteString(HelpStyle.Render(fmt.Sprintf("Config: %s", m.configPath())))

	return b.String()
}

func cycleLogLevel(current string) string {
	levels := []string{"debug", "info", "warn", "error"}
	for i, l := range levels {
		if l == current {
			return levels[(i+1)%len(levels)]
		}
	}
	return "info"
}

func cycleRetries(current int) int {
	switch current {
	case 0:
		return 1
	case 1:
		return 2
	case 2:
		return 3
	default:
		return 0
	}
}

func cycleTimeout(current int) int {
	switch {
	case current <= 60:
		return 120
	case current <= 120:
		return 300
	case current <= 300:
		return 600
	default:
		return 60
	}
}

func cycleMaxTokens(current int) int {
	switch {
	case current <= 2048:
		return 4096
	case current <= 4096:
		return 8192
	case current <= 8192:
		return 16384
	case current <= 16384:
		return 32768
	default:
		return 2048
	}
}

func (m SettingsModel) configPath() string {
	return m.config.GetPath()
}

func (m *SettingsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m SettingsModel) GetSettings() config.Settings {
	return m.settings
}
