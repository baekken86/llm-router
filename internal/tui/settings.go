package tui

import (
	"fmt"
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
			if m.selected < 1 {
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

	items := []struct {
		label   string
		enabled bool
	}{
		{"RTK Token Compression (bash tool output)", m.settings.RTKEnabled},
		{"Caveman Output Compression (model response)", m.settings.CavemanEnabled},
	}

	for i, item := range items {
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
	b.WriteString(HelpStyle.Render("↑/↓: select  enter/space: toggle"))
	b.WriteString("\n")
	b.WriteString(HelpStyle.Render(fmt.Sprintf("Config: %s", m.configPath())))

	return b.String()
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

func renderSettingRow(label string, enabled bool, selected bool, width int) string {
	status := ErrorStyle.Render("OFF")
	if enabled {
		status = InfoStyle.Render("ON ")
	}

	cursor := "  "
	if selected {
		cursor = "> "
	}

	return fmt.Sprintf("%s%s: %s", cursor, label, status)
}
