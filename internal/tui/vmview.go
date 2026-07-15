package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type VMViewMsg struct {
	VirtualModels []VirtualModelInfo
}

type VirtualModelInfo struct {
	Name          string
	Filter        string
	Sort          string
	ResolvedModels []ResolvedModelInfo
}

type ResolvedModelInfo struct {
	ModelName    string
	ProviderName string
	Tags         map[string]string
	Position     int
}

type VMViewModel struct {
	items  []VirtualModelInfo
	cursor int
	width  int
	height int
	loaded bool
}

func NewVMViewModel() VMViewModel {
	return VMViewModel{}
}

func (m VMViewModel) Update(msg tea.Msg) (VMViewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case VMViewMsg:
		m.items = msg.VirtualModels
		m.loaded = true
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m VMViewModel) View() string {
	if !m.loaded {
		return MutedStyle.Render("  Loading virtual models...")
	}

	if len(m.items) == 0 {
		return MutedStyle.Render("  No virtual models configured. Create one via API.")
	}

	var b strings.Builder

	for i, vm := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = SuccessStyle.Render("▸ ")
		}

		b.WriteString(fmt.Sprintf("%s%s\n", cursor, InfoStyle.Render(vm.Name)))

		if i == m.cursor {
			b.WriteString(fmt.Sprintf("    %s %s\n",
				MutedStyle.Render("Filter:"),
				MutedStyle.Render(vm.Filter),
			))
			b.WriteString(fmt.Sprintf("    %s %s\n",
				MutedStyle.Render("Sort:"),
				MutedStyle.Render(vm.Sort),
			))
			b.WriteString("\n")

			if len(vm.ResolvedModels) == 0 {
				b.WriteString(MutedStyle.Render("    No matching models\n"))
			} else {
				b.WriteString(StatHeaderStyle.Render("    Resolved Models (priority order)"))
				b.WriteString("\n")
				for _, rm := range vm.ResolvedModels {
					tags := formatTagsCompact(rm.Tags)
					b.WriteString(fmt.Sprintf("    %s %s/%s %s\n",
						MutedStyle.Render(fmt.Sprintf("#%d", rm.Position)),
						InfoStyle.Render(rm.ProviderName),
						SuccessStyle.Render(rm.ModelName),
						MutedStyle.Render(tags),
					))
				}
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m *VMViewModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func formatTagsCompact(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}

	parts := []string{}
	if v, ok := tags["intel"]; ok {
		parts = append(parts, fmt.Sprintf("intel=%s", v))
	}
	if v, ok := tags["speed"]; ok {
		parts = append(parts, fmt.Sprintf("speed=%s", v))
	}
	if v, ok := tags["cost-type"]; ok {
		parts = append(parts, fmt.Sprintf("cost=%s", v))
	}
	if v, ok := tags["hallucination"]; ok {
		parts = append(parts, fmt.Sprintf("hallu=%s", v))
	}

	return "[" + strings.Join(parts, ", ") + "]"
}

func FetchVMData(client *APIClient) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return VMViewMsg{}
		}

		vms, err := client.GetVirtualModels()
		if err != nil {
			return VMViewMsg{}
		}

		var items []VirtualModelInfo
		for _, vm := range vms {
			resolved, _ := client.GetResolvedModels(vm.ID)

			var resolvedInfo []ResolvedModelInfo
			for i, r := range resolved {
				resolvedInfo = append(resolvedInfo, ResolvedModelInfo{
					ModelName:    r.ModelName,
					ProviderName: r.ProviderName,
					Tags:         r.Tags,
					Position:     i + 1,
				})
			}

			items = append(items, VirtualModelInfo{
				Name:           vm.Name,
				Filter:         formatFilter(vm.FilterExpr),
				Sort:           formatSort(vm.SortExpr),
				ResolvedModels: resolvedInfo,
			})
		}

		return VMViewMsg{VirtualModels: items}
	}
}

func formatFilter(filter interface{}) string {
	data, err := json.Marshal(filter)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func formatSort(sort interface{}) string {
	data, err := json.Marshal(sort)
	if err != nil {
		return "[]"
	}
	return string(data)
}
