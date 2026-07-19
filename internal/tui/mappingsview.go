package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/chris/llm-router/internal/repository"
)

type MappingsViewMsg struct {
	Mappings []MappingEntry
}

type MappingEntry struct {
	SourceModelID     int64
	SourceModelName   string
	SourceProviderName string
	TargetModelName   string
	CreatedAt         string
}

type MappingsViewModel struct {
	mappings []MappingEntry
	cursor   int
	width    int
	height   int
	loaded   bool
}

func NewMappingsViewModel() MappingsViewModel {
	return MappingsViewModel{}
}

func (m MappingsViewModel) Update(msg tea.Msg) (MappingsViewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case MappingsViewMsg:
		m.mappings = msg.Mappings
		m.loaded = true
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.mappings)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m MappingsViewModel) View() string {
	if !m.loaded {
		return MutedStyle.Render("  Loading mappings...")
	}

	if len(m.mappings) == 0 {
		return MutedStyle.Render("  No mappings configured. Map models from the Models tab.")
	}

	var b strings.Builder

	// Header
	header := "    "
	header += padRight("source model", 25)
	header += padRight("source provider", 15)
	header += padRight("→", 3)
	header += padRight("target model", 25)
	header += "created"
	b.WriteString(MutedStyle.Render(header))
	b.WriteString("\n")

	// Separator
	sep := "    " + strings.Repeat("-", 25) + "  " +
		strings.Repeat("-", 15) + "  " +
		strings.Repeat("-", 3) + "  " +
		strings.Repeat("-", 25) + "  " +
		strings.Repeat("-", 19)
	b.WriteString(MutedStyle.Render(sep))
	b.WriteString("\n")

	for i, entry := range m.mappings {
		cursor := "  "
		if i == m.cursor {
			cursor = SuccessStyle.Render("▸ ")
		}

		line := "    " + cursor
		line += padRight(trunc(entry.SourceModelName, 25), 25) + "  "
		line += padRight(trunc(entry.SourceProviderName, 15), 15) + "  "
		line += padRight("→", 3) + "  "
		line += padRight(trunc(entry.TargetModelName, 25), 25) + "  "
		line += entry.CreatedAt

		if i == m.cursor {
			b.WriteString(SuccessStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(MutedStyle.Render(fmt.Sprintf("  %d mappings", len(m.mappings))))

	return b.String()
}

func (m MappingsViewModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m MappingsViewModel) Selected() int {
	if len(m.mappings) == 0 {
		return -1
	}
	return m.cursor
}

func (m MappingsViewModel) GetMapping(index int) *MappingEntry {
	if index < 0 || index >= len(m.mappings) {
		return nil
	}
	return &m.mappings[index]
}

func FetchMappingsLocal(mappingRepo repository.ModelMappingRepository) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		joined, err := mappingRepo.GetAllJoined(ctx)
		if err != nil {
			return MappingsViewMsg{}
		}

		var entries []MappingEntry
		for _, j := range joined {
			entries = append(entries, MappingEntry{
				SourceModelID:     j.SourceModelID,
				SourceModelName:   j.SourceModelName,
				SourceProviderName: j.SourceProviderName,
				TargetModelName:   j.TargetModelName,
				CreatedAt:         j.CreatedAt,
			})
		}

		return MappingsViewMsg{Mappings: entries}
	}
}
