package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/chris/llm-router/internal/models"
	"github.com/chris/llm-router/internal/repository"
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
	if v, ok := tags["intelligence"]; ok {
		parts = append(parts, fmt.Sprintf("int=%s", v))
	}
	if v, ok := tags["coding"]; ok {
		parts = append(parts, fmt.Sprintf("code=%s", v))
	}
	if v, ok := tags["speed"]; ok {
		parts = append(parts, fmt.Sprintf("spd=%s", v))
	}
	if v, ok := tags["cost_per_1m_input"]; ok {
		parts = append(parts, fmt.Sprintf("$/1M=%s", v))
	}
	if v, ok := tags["hallucination"]; ok {
		parts = append(parts, fmt.Sprintf("hall=%s", v))
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
			resolved, err := client.GetResolvedModels(vm.ID)
			if err != nil {
				resolved = nil
			}

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

func FetchVMDataLocal(vmRepo repository.VirtualModelRepository, modelRepo repository.ModelRepository, tagRepo repository.TagRepository, providerRepo repository.ProviderRepository) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		vms, err := vmRepo.List(ctx)
		if err != nil {
			return VMViewMsg{}
		}

		var items []VirtualModelInfo
		for _, vm := range vms {
			var filterExpr models.FilterExpr
			if len(vm.FilterExpr) > 0 {
				json.Unmarshal(vm.FilterExpr, &filterExpr)
			}

			var sortExpr models.SortExpr
			if len(vm.SortExpr) > 0 {
				json.Unmarshal(vm.SortExpr, &sortExpr)
			}

			allModels, _ := modelRepo.ListAll(ctx)
			var resolved []ResolvedModelInfo

			for _, m := range allModels {
				tags, _ := tagRepo.GetByModel(ctx, m.ID)
				tagMap := make(map[string]string)
				for _, t := range tags {
					if t.ReasoningEffort == "" {
						tagMap[t.Key] = t.Value
					}
				}

				if !matchesFilter(tagMap, filterExpr) {
					continue
				}

				provider, _ := providerRepo.GetByID(ctx, m.ProviderID)
				if provider == nil {
					continue
				}

				resolved = append(resolved, ResolvedModelInfo{
					ModelName:    m.Name,
					ProviderName: provider.Name,
					Tags:         tagMap,
				})
			}

			// Apply sorting
			sortResolved(resolved, sortExpr)

			// Set positions after sorting
			for i := range resolved {
				resolved[i].Position = i + 1
			}

			items = append(items, VirtualModelInfo{
				Name:           vm.Name,
				Filter:         string(vm.FilterExpr),
				Sort:           string(vm.SortExpr),
				ResolvedModels: resolved,
			})
		}

		return VMViewMsg{VirtualModels: items}
	}
}

func sortResolved(resolved []ResolvedModelInfo, sortExpr models.SortExpr) {
	if len(sortExpr) == 0 {
		return
	}

	sort.Slice(resolved, func(i, j int) bool {
		tagsI := resolved[i].Tags
		tagsJ := resolved[j].Tags

		for _, s := range sortExpr {
			valI := tagsI[s.Key]
			valJ := tagsJ[s.Key]

			if s.Direction != "" {
				cmp := strings.Compare(valI, valJ)
				if cmp == 0 {
					continue
				}
				if s.Direction == "desc" {
					return cmp > 0
				}
				return cmp < 0
			}

			if len(s.Order) > 0 {
				idxI := indexOf(s.Order, valI)
				idxJ := indexOf(s.Order, valJ)
				if idxI == idxJ {
					continue
				}
				return idxI < idxJ
			}
		}

		return false
	})
}

func indexOf(arr []string, val string) int {
	for i, v := range arr {
		if v == val {
			return i
		}
	}
	return len(arr)
}

func matchesFilter(tags map[string]string, filter models.FilterExpr) bool {
	if len(filter.And) == 0 {
		return true
	}

	for _, cond := range filter.And {
		val, exists := tags[cond.Key]
		if !exists {
			return false
		}
		if !evaluateCondition(val, cond.Op, cond.Value) {
			return false
		}
	}

	return true
}

func evaluateCondition(actual string, op string, expected interface{}) bool {
	switch op {
	case "eq":
		return fmt.Sprintf("%v", expected) == actual
	case "neq":
		return fmt.Sprintf("%v", expected) != actual
	case "gt":
		return compareNumeric(actual, expected) > 0
	case "gte":
		return compareNumeric(actual, expected) >= 0
	case "lt":
		return compareNumeric(actual, expected) < 0
	case "lte":
		return compareNumeric(actual, expected) <= 0
	default:
		return false
	}
}

func compareNumeric(actual string, expected interface{}) int {
	a := parseFloat(actual)
	b := parseFloat(fmt.Sprintf("%v", expected))
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}
