package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/chris/llm-router/internal/config"
	"github.com/chris/llm-router/internal/proxy"
	"github.com/chris/llm-router/internal/repository"
)

type LogMsg struct {
	Log proxy.RequestLog
}

type StatsRefreshMsg struct {
	Stats *StatsResponse
}

type Tab int

const (
	TabLog Tab = iota
	TabStats
	TabVM
	TabSettings
)

type Model struct {
	logView      LogViewModel
	stats        StatsModel
	vmView       VMViewModel
	settingsView SettingsModel
	tab          Tab
	width        int
	height       int
	ready        bool
	logChan      <-chan proxy.RequestLog
	apiClient    *APIClient
	vmRepo       repository.VirtualModelRepository
	modelRepo    repository.ModelRepository
	tagRepo      repository.TagRepository
	providerRepo repository.ProviderRepository
	config       *config.Config
}

func New(logChan <-chan proxy.RequestLog, vmRepo repository.VirtualModelRepository, modelRepo repository.ModelRepository, tagRepo repository.TagRepository, providerRepo repository.ProviderRepository, cfg *config.Config) Model {
	return Model{
		logView:      NewLogViewModel(500),
		stats:        NewStatsModel(),
		vmView:       NewVMViewModel(),
		settingsView: NewSettingsModel(cfg),
		tab:          TabLog,
		logChan:      logChan,
		vmRepo:       vmRepo,
		modelRepo:    modelRepo,
		tagRepo:      tagRepo,
		providerRepo: providerRepo,
		config:       cfg,
	}
}

func NewRemote(apiClient *APIClient, logChan <-chan proxy.RequestLog, cfg *config.Config) Model {
	return Model{
		logView:      NewLogViewModel(500),
		stats:        NewStatsModel(),
		vmView:       NewVMViewModel(),
		settingsView: NewSettingsModel(cfg),
		tab:          TabLog,
		logChan:      logChan,
		apiClient:    apiClient,
		config:       cfg,
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.waitForLog(),
		tea.EnterAltScreen,
	}
	if m.apiClient != nil {
		cmds = append(cmds, m.refreshStats())
		cmds = append(cmds, FetchVMData(m.apiClient))
	} else if m.vmRepo != nil {
		cmds = append(cmds, FetchVMDataLocal(m.vmRepo, m.modelRepo, m.tagRepo, m.providerRepo))
	}
	return tea.Batch(cmds...)
}

func (m Model) waitForLog() tea.Cmd {
	return func() tea.Msg {
		log, ok := <-m.logChan
		if !ok {
			return nil
		}
		return LogMsg{Log: log}
	}
}

func (m Model) refreshStats() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		if m.apiClient == nil {
			return nil
		}
		stats, err := m.apiClient.GetStats()
		if err != nil {
			return nil
		}
		return StatsRefreshMsg{Stats: stats}
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.logView.SetSize(msg.Width, msg.Height-4)
		m.stats.SetSize(msg.Width, msg.Height-4)
		m.vmView.SetSize(msg.Width, msg.Height-4)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			switch m.tab {
			case TabLog:
				m.tab = TabStats
			case TabStats:
				m.tab = TabVM
			case TabVM:
				m.tab = TabSettings
			case TabSettings:
				m.tab = TabLog
			}
		case "shift+tab":
			switch m.tab {
			case TabLog:
				m.tab = TabSettings
			case TabStats:
				m.tab = TabLog
			case TabVM:
				m.tab = TabStats
			case TabSettings:
				m.tab = TabVM
			}
			if m.tab == TabVM {
				if m.apiClient != nil {
					return m, FetchVMData(m.apiClient)
				} else if m.vmRepo != nil {
					return m, FetchVMDataLocal(m.vmRepo, m.modelRepo, m.tagRepo, m.providerRepo)
				}
			}
			return m, nil
		case "r":
			if m.tab == TabStats {
				m.stats, _ = m.stats.Update(StatsResetMsg{})
			} else if m.tab == TabVM {
				if m.apiClient != nil {
					return m, FetchVMData(m.apiClient)
				} else if m.vmRepo != nil {
					return m, FetchVMDataLocal(m.vmRepo, m.modelRepo, m.tagRepo, m.providerRepo)
				}
			}
			return m, nil
		case "up", "k":
			if m.tab == TabLog {
				m.logView.ScrollUp()
			} else if m.tab == TabVM {
				m.vmView, _ = m.vmView.Update(msg)
			} else if m.tab == TabSettings {
				m.settingsView, _ = m.settingsView.Update(msg)
			}
			return m, nil
		case "down", "j":
			if m.tab == TabLog {
				m.logView.ScrollDown()
			} else if m.tab == TabVM {
				m.vmView, _ = m.vmView.Update(msg)
			} else if m.tab == TabSettings {
				m.settingsView, _ = m.settingsView.Update(msg)
			}
			return m, nil
		case "enter", " ":
			if m.tab == TabSettings {
				m.settingsView, _ = m.settingsView.Update(msg)
				return m, nil
			}
		}

	case VMViewMsg:
		m.vmView, _ = m.vmView.Update(msg)
		return m, nil

	case LogMsg:
		if m.tab == TabLog {
			m.logView.AddEntry(msg.Log)
		}
		m.stats, _ = m.stats.Update(StatsUpdateMsg{Log: msg.Log})
		return m, m.waitForLog()

	case StatsRefreshMsg:
		if msg.Stats != nil {
			m.stats.LoadFromAPI(msg.Stats)
			return m, m.refreshStats()
		}
	}

	return m, nil
}

func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	switch m.tab {
	case TabLog:
		b.WriteString(m.logView.View())
	case TabStats:
		b.WriteString(m.stats.View())
	case TabVM:
		b.WriteString(m.vmView.View())
	case TabSettings:
		b.WriteString(m.settingsView.View())
	}

	b.WriteString(m.renderFooter())

	return b.String()
}

func (m Model) renderHeader() string {
	logTab := TabInactiveStyle.Render(" Log ")
	statsTab := TabInactiveStyle.Render(" Stats ")
	vmTab := TabInactiveStyle.Render(" Models ")
	settingsTab := TabInactiveStyle.Render(" Settings ")

	if m.tab == TabLog {
		logTab = TabActiveStyle.Render(" Log ")
	} else if m.tab == TabStats {
		statsTab = TabActiveStyle.Render(" Stats ")
	} else if m.tab == TabVM {
		vmTab = TabActiveStyle.Render(" Models ")
	} else if m.tab == TabSettings {
		settingsTab = TabActiveStyle.Render(" Settings ")
	}

	title := TitleStyle.Render("llm-router")

	mode := ""
	if m.apiClient != nil {
		mode = MutedStyle.Render(" [remote]")
	}

	return fmt.Sprintf("%s%s  %s %s %s %s", title, mode, logTab, statsTab, vmTab, settingsTab)
}

func (m Model) renderFooter() string {
	help := HelpStyle.Render("tab/shift+tab: switch view  ↑/↓: scroll  r: reset stats  q: quit")
	return lipgloss.Place(m.width, 1, lipgloss.Left, lipgloss.Bottom, help)
}

func Run(logChan <-chan proxy.RequestLog, vmRepo repository.VirtualModelRepository, modelRepo repository.ModelRepository, tagRepo repository.TagRepository, providerRepo repository.ProviderRepository, cfg *config.Config) {
	p := tea.NewProgram(New(logChan, vmRepo, modelRepo, tagRepo, providerRepo, cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("TUI error: %v\n", err)
	}
}

func RunRemote(apiClient *APIClient, logChan <-chan proxy.RequestLog, cfg *config.Config) {
	p := tea.NewProgram(NewRemote(apiClient, logChan, cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("TUI error: %v\n", err)
	}
}
