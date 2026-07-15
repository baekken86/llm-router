package tui

import "github.com/charmbracelet/lipgloss"

var (
	ColorSuccess = lipgloss.Color("46")   // green
	ColorWarning = lipgloss.Color("226")  // yellow
	ColorError   = lipgloss.Color("196")  // red
	ColorInfo    = lipgloss.Color("39")   // blue
	ColorMuted   = lipgloss.Color("245")  // gray
	ColorAccent  = lipgloss.Color("205")  // pink
	ColorWhite   = lipgloss.Color("255")
)

var (
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorAccent).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorAccent).
			Padding(0, 1)

	StatusBarStyle = lipgloss.NewStyle().
			Foreground(ColorWhite).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	TabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorAccent).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(ColorAccent)

	TabInactiveStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	LogLineStyle = lipgloss.NewStyle().
			Padding(0, 1)

	SuccessStyle = lipgloss.NewStyle().Foreground(ColorSuccess)
	WarningStyle = lipgloss.NewStyle().Foreground(ColorWarning)
	ErrorStyle   = lipgloss.NewStyle().Foreground(ColorError)
	InfoStyle    = lipgloss.NewStyle().Foreground(ColorInfo)
	MutedStyle   = lipgloss.NewStyle().Foreground(ColorMuted)

	StatLabelStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Width(25)

	StatValueStyle = lipgloss.NewStyle().
			Foreground(ColorWhite).
			Bold(true)

	StatHeaderStyle = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(ColorMuted).
			MarginBottom(1)

	HelpStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)
)
