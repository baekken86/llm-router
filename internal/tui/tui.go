package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/chris/llm-router/internal/proxy"
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
)

type Model struct {
	logView    LogViewModel
	stats      StatsModel
	tab        Tab
	width      int
	height     int
	ready      bool
	logChan    <-chan proxy.RequestLog
	apiClient  *APIClient
}

func New(logChan <-chan proxy.RequestLog) Model {
	return Model{
		logView: NewLogViewModel(500),
		stats:   NewStatsModel(),
		tab:     TabLog,
		logChan: logChan,
	}
}

func NewRemote(apiClient *APIClient, logChan <-chan proxy.RequestLog) Model {
	return Model{
		logView:   NewLogViewModel(500),
		stats:     NewStatsModel(),
		tab:       TabLog,
		logChan:   logChan,
		apiClient: apiClient,
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.waitForLog(),
		tea.EnterAltScreen,
	}
	if m.apiClient != nil {
		cmds = append(cmds, m.refreshStats())
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
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			if m.tab == TabLog {
				m.tab = TabStats
			} else {
				m.tab = TabLog
			}
			return m, nil
		case "r":
			if m.tab == TabStats {
				m.stats, _ = m.stats.Update(StatsResetMsg{})
			}
			return m, nil
		case "up", "k":
			if m.tab == TabLog {
				m.logView.ScrollUp()
			}
			return m, nil
		case "down", "j":
			if m.tab == TabLog {
				m.logView.ScrollDown()
			}
			return m, nil
		}

	case LogMsg:
		m.logView.AddEntry(msg.Log)
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
	}

	b.WriteString(m.renderFooter())

	return b.String()
}

func (m Model) renderHeader() string {
	logTab := TabInactiveStyle.Render(" Log ")
	statsTab := TabInactiveStyle.Render(" Stats ")

	if m.tab == TabLog {
		logTab = TabActiveStyle.Render(" Log ")
	} else {
		statsTab = TabActiveStyle.Render(" Stats ")
	}

	title := TitleStyle.Render("llm-router")

	mode := ""
	if m.apiClient != nil {
		mode = MutedStyle.Render(" [remote]")
	}

	return fmt.Sprintf("%s%s  %s %s", title, mode, logTab, statsTab)
}

func (m Model) renderFooter() string {
	help := HelpStyle.Render("tab: switch view  ↑/↓: scroll  r: reset stats  q: quit")
	return lipgloss.Place(m.width, 1, lipgloss.Left, lipgloss.Bottom, help)
}

func Run(logChan <-chan proxy.RequestLog) {
	p := tea.NewProgram(New(logChan), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("TUI error: %v\n", err)
	}
}

func RunRemote(apiClient *APIClient, logChan <-chan proxy.RequestLog) {
	p := tea.NewProgram(NewRemote(apiClient, logChan), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("TUI error: %v\n", err)
	}
}
