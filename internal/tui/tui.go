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

type SysLogMsg struct {
	Line string
}

type StatsRefreshMsg struct {
	Stats *StatsResponse
}

type Tab int

const (
	TabLog Tab = iota
	TabSyslog
	TabStats
	TabStatus
	TabVM
	TabSettings
)

type Model struct {
	logView      LogViewModel
	sysLogView   SysLogViewModel
	stats        StatsModel
	status       StatusModel
	vmView       VMViewModel
	settingsView SettingsModel
	tab          Tab
	width        int
	height       int
	ready        bool
	logChan      <-chan proxy.RequestLog
	syslogChan   <-chan string
	apiClient    *APIClient
	vmRepo       repository.VirtualModelRepository
	modelRepo    repository.ModelRepository
	tagRepo      repository.TagRepository
	providerRepo repository.ProviderRepository
	oauthRepo    repository.OAuthRepository
	config       *config.Config
}

func New(logChan <-chan proxy.RequestLog, syslogChan <-chan string, vmRepo repository.VirtualModelRepository, modelRepo repository.ModelRepository, tagRepo repository.TagRepository, providerRepo repository.ProviderRepository, oauthRepo repository.OAuthRepository, cfg *config.Config, initialLogs []proxy.RequestLog, initialSyslogs []SysLogEntry, initialStats *StatsResponse) Model {
	logView := NewLogViewModel(500)
	if len(initialLogs) > 0 {
		logView.LoadInitial(initialLogs)
	}

	sysLogView := NewSysLogViewModel(1000)
	if len(initialSyslogs) > 0 {
		sysLogView.LoadInitial(initialSyslogs)
	}

	stats := NewStatsModel()
	if initialStats != nil {
		stats.LoadFromAPI(initialStats)
	}

	return Model{
		logView:      logView,
		sysLogView:   sysLogView,
		stats:        stats,
		status:       NewStatusModel(),
		vmView:       NewVMViewModel(),
		settingsView: NewSettingsModel(cfg),
		tab:          TabLog,
		logChan:      logChan,
		syslogChan:   syslogChan,
		vmRepo:       vmRepo,
		modelRepo:    modelRepo,
		tagRepo:      tagRepo,
		providerRepo: providerRepo,
		oauthRepo:    oauthRepo,
		config:       cfg,
	}
}

func NewRemote(apiClient *APIClient, logChan <-chan proxy.RequestLog, syslogChan <-chan string, cfg *config.Config) Model {
	return Model{
		logView:      NewLogViewModel(500),
		sysLogView:   NewSysLogViewModel(1000),
		stats:        NewStatsModel(),
		status:       NewStatusModel(),
		vmView:       NewVMViewModel(),
		settingsView: NewSettingsModel(cfg),
		tab:          TabLog,
		logChan:      logChan,
		syslogChan:   syslogChan,
		apiClient:    apiClient,
		config:       cfg,
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.waitForLog(),
		m.waitForSyslog(),
		tea.EnterAltScreen,
	}
	if m.apiClient != nil {
		cmds = append(cmds, m.refreshStats())
		cmds = append(cmds, FetchStatus(m.apiClient))
		cmds = append(cmds, FetchVMData(m.apiClient))
		cmds = append(cmds, FetchRawModels(m.apiClient))
	} else if m.vmRepo != nil {
		cmds = append(cmds, FetchStatusLocal(m.providerRepo, m.oauthRepo))
		cmds = append(cmds, FetchVMDataLocal(m.vmRepo, m.modelRepo, m.tagRepo, m.providerRepo))
		cmds = append(cmds, FetchRawModelsLocal(m.modelRepo, m.tagRepo, m.providerRepo))
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

func (m Model) waitForSyslog() tea.Cmd {
	return func() tea.Msg {
		line, ok := <-m.syslogChan
		if !ok {
			return nil
		}
		return SysLogMsg{Line: line}
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

func (m Model) refreshStatus() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		if m.apiClient == nil {
			return nil
		}
		status, err := m.apiClient.GetStatus()
		if err != nil {
			return nil
		}
		return StatusRefreshMsg{Status: status}
	})
}

func FetchStatus(apiClient *APIClient) tea.Cmd {
	return func() tea.Msg {
		status, err := apiClient.GetStatus()
		if err != nil {
			return StatusRefreshMsg{}
		}
		return StatusRefreshMsg{Status: status}
	}
}

func ClearRateLimitCmd(apiClient *APIClient, providerID int64) tea.Cmd {
	return func() tea.Msg {
		err := apiClient.ClearRateLimit(providerID)
		return StatusClearMsg{ProviderID: providerID, Err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.logView.SetSize(msg.Width, msg.Height-4)
		m.sysLogView.SetSize(msg.Width, msg.Height-4)
		m.stats.SetSize(msg.Width, msg.Height-4)
		m.status.SetSize(msg.Width, msg.Height-4)
		m.vmView.SetSize(msg.Width, msg.Height-4)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			switch m.tab {
			case TabLog:
				m.tab = TabSyslog
			case TabSyslog:
				m.tab = TabStats
			case TabStats:
				m.tab = TabStatus
			case TabStatus:
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
			case TabSyslog:
				m.tab = TabLog
			case TabStats:
				m.tab = TabSyslog
			case TabStatus:
				m.tab = TabStats
			case TabVM:
				m.tab = TabStatus
			case TabSettings:
				m.tab = TabVM
			}
			if m.tab == TabVM {
				if m.apiClient != nil {
					return m, tea.Batch(FetchVMData(m.apiClient), FetchRawModels(m.apiClient))
				} else if m.vmRepo != nil {
					return m, tea.Batch(FetchVMDataLocal(m.vmRepo, m.modelRepo, m.tagRepo, m.providerRepo), FetchRawModelsLocal(m.modelRepo, m.tagRepo, m.providerRepo))
				}
			}
			return m, nil
		case "left", "h":
			if m.tab == TabLog {
				m.logView.ScrollLeft()
			} else if m.tab == TabSyslog {
				m.sysLogView.ScrollLeft()
			} else if m.tab == TabVM {
				prevTab := m.vmView.modelTab
				m.vmView, _ = m.vmView.Update(msg)
				if prevTab != m.vmView.modelTab {
					if m.vmView.modelTab == ModelTabRaw {
						if m.apiClient != nil {
							return m, FetchRawModels(m.apiClient)
						} else if m.vmRepo != nil {
							return m, FetchRawModelsLocal(m.modelRepo, m.tagRepo, m.providerRepo)
						}
					}
				}
			}
			return m, nil
		case "right", "l":
			if m.tab == TabLog {
				m.logView.ScrollRight()
			} else if m.tab == TabSyslog {
				m.sysLogView.ScrollRight()
			} else if m.tab == TabVM {
				prevTab := m.vmView.modelTab
				m.vmView, _ = m.vmView.Update(msg)
				if prevTab != m.vmView.modelTab {
					if m.vmView.modelTab == ModelTabVirtual {
						if m.apiClient != nil {
							return m, FetchVMData(m.apiClient)
						} else if m.vmRepo != nil {
							return m, FetchVMDataLocal(m.vmRepo, m.modelRepo, m.tagRepo, m.providerRepo)
						}
					}
				}
			}
			return m, nil
		case "r":
			if m.tab == TabStats {
				m.stats, _ = m.stats.Update(StatsResetMsg{})
			} else if m.tab == TabStatus {
				if m.apiClient != nil {
					return m, FetchStatus(m.apiClient)
				} else if m.providerRepo != nil {
					return m, FetchStatusLocal(m.providerRepo, m.oauthRepo)
				}
			} else if m.tab == TabVM {
				if m.vmView.modelTab == ModelTabRaw {
					if m.apiClient != nil {
						return m, FetchRawModels(m.apiClient)
					} else if m.vmRepo != nil {
						return m, FetchRawModelsLocal(m.modelRepo, m.tagRepo, m.providerRepo)
					}
				} else {
					if m.apiClient != nil {
						return m, FetchVMData(m.apiClient)
					} else if m.vmRepo != nil {
						return m, FetchVMDataLocal(m.vmRepo, m.modelRepo, m.tagRepo, m.providerRepo)
					}
				}
			}
			return m, nil
		case "up", "k":
			if m.tab == TabLog {
				m.logView.ScrollUp()
			} else if m.tab == TabSyslog {
				m.sysLogView.ScrollUp()
			} else if m.tab == TabStatus {
				m.status, _ = m.status.Update(msg)
			} else if m.tab == TabVM {
				m.vmView, _ = m.vmView.Update(msg)
			} else if m.tab == TabSettings {
				m.settingsView, _ = m.settingsView.Update(msg)
			}
			return m, nil
		case "down", "j":
			if m.tab == TabLog {
				m.logView.ScrollDown()
			} else if m.tab == TabSyslog {
				m.sysLogView.ScrollDown()
			} else if m.tab == TabStatus {
				m.status, _ = m.status.Update(msg)
			} else if m.tab == TabVM {
				m.vmView, _ = m.vmView.Update(msg)
			} else if m.tab == TabSettings {
				m.settingsView, _ = m.settingsView.Update(msg)
			}
			return m, nil
		case "pgup":
			if m.tab == TabLog {
				m.logView.ScrollToTop()
			} else if m.tab == TabSyslog {
				m.sysLogView.ScrollToTop()
			}
			return m, nil
		case "pgdown":
			if m.tab == TabLog {
				m.logView.ScrollToEnd()
			} else if m.tab == TabSyslog {
				m.sysLogView.ScrollToEnd()
			}
			return m, nil
		case "enter", " ":
			if m.tab == TabSettings {
				m.settingsView, _ = m.settingsView.Update(msg)
				return m, nil
			}
			if m.tab == TabVM {
				m.vmView, _ = m.vmView.Update(msg)
				return m, nil
			}
		case "esc":
			if m.tab == TabVM {
				m.vmView, _ = m.vmView.Update(msg)
				return m, nil
			}
		case "c":
			if m.tab == TabStatus && m.apiClient != nil {
				if p := m.status.SelectedProvider(); p != nil && p.RateLimited {
					return m, ClearRateLimitCmd(m.apiClient, p.ID)
				}
			}
		}

	case VMViewMsg:
		m.vmView, _ = m.vmView.Update(msg)
		return m, nil

	case RawModelListMsg:
		m.vmView, _ = m.vmView.Update(msg)
		return m, nil

	case LogMsg:
		if m.tab == TabLog {
			m.logView.AddEntry(msg.Log)
		}
		m.stats, _ = m.stats.Update(StatsUpdateMsg{Log: msg.Log})
		return m, m.waitForLog()

	case SysLogMsg:
		m.sysLogView.AddEntry(msg.Line)
		return m, m.waitForSyslog()

	case StatsRefreshMsg:
		if msg.Stats != nil {
			m.stats.LoadFromAPI(msg.Stats)
			return m, m.refreshStats()
		}

	case StatusRefreshMsg:
		m.status, _ = m.status.Update(msg)
		return m, m.refreshStatus()

	case StatusClearMsg:
		m.status, _ = m.status.Update(msg)
		return m, nil
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
	case TabSyslog:
		b.WriteString(m.sysLogView.View())
	case TabStats:
		b.WriteString(m.stats.View())
	case TabStatus:
		b.WriteString(m.status.View())
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
	syslogTab := TabInactiveStyle.Render(" Syslog ")
	statsTab := TabInactiveStyle.Render(" Stats ")
	statusTab := TabInactiveStyle.Render(" Status ")
	vmTab := TabInactiveStyle.Render(" Models ")
	settingsTab := TabInactiveStyle.Render(" Settings ")

	if m.tab == TabLog {
		logTab = TabActiveStyle.Render(" Log ")
	} else if m.tab == TabSyslog {
		syslogTab = TabActiveStyle.Render(" Syslog ")
	} else if m.tab == TabStats {
		statsTab = TabActiveStyle.Render(" Stats ")
	} else if m.tab == TabStatus {
		statusTab = TabActiveStyle.Render(" Status ")
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

	header := fmt.Sprintf("%s%s  %s %s %s %s %s %s", title, mode, logTab, syslogTab, statsTab, statusTab, vmTab, settingsTab)

	if m.tab == TabVM {
		subTabSep := MutedStyle.Render("  |  ")
		rawLabel := MutedStyle.Render("Raw")
		virtualLabel := MutedStyle.Render("Virtual")
		if m.vmView.modelTab == ModelTabRaw {
			rawLabel = SuccessStyle.Render("Raw")
		} else {
			virtualLabel = SuccessStyle.Render("Virtual")
		}
		header += subTabSep + rawLabel + MutedStyle.Render(" / ") + virtualLabel
	}

	return header
}

func (m Model) renderFooter() string {
	help := HelpStyle.Render("tab/shift+tab: switch view  ↑/↓: scroll  ←/→: horizontal scroll  pgup/pgdown: jump  r: reset stats  q: quit")
	if m.tab == TabStatus {
		help = HelpStyle.Render("↑/↓: navigate  c: clear rate limit  r: refresh  tab: switch view  q: quit")
	} else if m.tab == TabVM && m.vmView.modelTab == ModelTabRaw {
		help = HelpStyle.Render("↑/↓: navigate  ←/→: switch to Virtual  tab: switch view  r: refresh  q: quit")
	} else if m.tab == TabVM && m.vmView.detailMode {
		help = HelpStyle.Render("↑/↓: navigate  enter: select  esc: back  ←/→: switch tab  tab: switch view  q: quit")
	} else if m.tab == TabVM {
		help = HelpStyle.Render("↑/↓: navigate  enter: detail view  ←/→: switch to Raw  tab: switch view  r: refresh  q: quit")
	}
	return lipgloss.Place(m.width, 1, lipgloss.Left, lipgloss.Bottom, help)
}

func Run(logChan <-chan proxy.RequestLog, syslogChan <-chan string, vmRepo repository.VirtualModelRepository, modelRepo repository.ModelRepository, tagRepo repository.TagRepository, providerRepo repository.ProviderRepository, oauthRepo repository.OAuthRepository, cfg *config.Config, initialLogs []proxy.RequestLog, initialSyslogs []SysLogEntry, initialStats *StatsResponse, quit chan<- struct{}) {
	p := tea.NewProgram(New(logChan, syslogChan, vmRepo, modelRepo, tagRepo, providerRepo, oauthRepo, cfg, initialLogs, initialSyslogs, initialStats), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("TUI error: %v\n", err)
	}
	close(quit)
}

func RunRemote(apiClient *APIClient, logChan <-chan proxy.RequestLog, syslogChan <-chan string, cfg *config.Config, quit chan<- struct{}) {
	p := tea.NewProgram(NewRemote(apiClient, logChan, syslogChan, cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("TUI error: %v\n", err)
	}
	close(quit)
}
