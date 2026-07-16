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

type RawModelListMsg struct {
	RawModels map[string][]RawModelInfo
}

type RawModelInfo struct {
	Name string
	Tags map[string]string
}

type VirtualModelInfo struct {
	Name           string
	Filter         string
	Sort           string
	FilterExpr     json.RawMessage
	SortExpr       json.RawMessage
	ResolvedModels []ResolvedModelInfo
}

type ResolvedModelInfo struct {
	ModelName       string
	ProviderName    string
	ReasoningEffort string
	Tags            map[string]string
	Position        int
}

type ModelTab int

const (
	ModelTabRaw ModelTab = iota
	ModelTabVirtual
)

type VMViewModel struct {
	items          []VirtualModelInfo
	cursor         int
	resolvedCursor int
	detailMode     bool
	modelTab       ModelTab
	rawModels      map[string][]RawModelInfo
	rawProviders   []string
	rawCursor      int
	width          int
	height         int
	loaded         bool
}

func NewVMViewModel() VMViewModel {
	return VMViewModel{}
}

func (m VMViewModel) Update(msg tea.Msg) (VMViewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case VMViewMsg:
		m.items = msg.VirtualModels
		m.loaded = true
	case RawModelListMsg:
		m.rawModels = msg.RawModels
		m.rawProviders = make([]string, 0, len(msg.RawModels))
		for p := range msg.RawModels {
			m.rawProviders = append(m.rawProviders, p)
		}
		sort.Strings(m.rawProviders)
		m.loaded = true
	case tea.KeyMsg:
		switch msg.String() {
		case "left", "h":
			if m.detailMode {
				break
			}
			if m.modelTab == ModelTabVirtual {
				m.modelTab = ModelTabRaw
				m.cursor = 0
			}
		case "right", "l":
			if m.detailMode {
				break
			}
			if m.modelTab == ModelTabRaw {
				m.modelTab = ModelTabVirtual
				m.cursor = 0
			}
		case "up", "k":
			if m.modelTab == ModelTabRaw {
				if m.rawCursor > 0 {
					m.rawCursor--
				}
			} else if m.detailMode {
				if m.resolvedCursor > 0 {
					m.resolvedCursor--
				}
			} else {
				if m.cursor > 0 {
					m.cursor--
				}
			}
		case "down", "j":
			if m.modelTab == ModelTabRaw {
				total := 0
				for _, models := range m.rawModels {
					total += len(models)
				}
				if m.rawCursor < total-1 {
					m.rawCursor++
				}
			} else if m.detailMode {
				if m.cursor < len(m.items) {
					vm := m.items[m.cursor]
					if m.resolvedCursor < len(vm.ResolvedModels)-1 {
						m.resolvedCursor++
					}
				}
			} else {
				if m.cursor < len(m.items)-1 {
					m.cursor++
				}
			}
		case "enter":
			if m.modelTab == ModelTabVirtual && !m.detailMode && len(m.items) > 0 {
				m.detailMode = true
				m.resolvedCursor = 0
			}
		case "esc":
			if m.detailMode {
				m.detailMode = false
				m.resolvedCursor = 0
			}
		}
	}
	return m, nil
}

func (m VMViewModel) View() string {
	if !m.loaded {
		if m.modelTab == ModelTabRaw {
			return MutedStyle.Render("  Loading raw models...")
		}
		return MutedStyle.Render("  Loading virtual models...")
	}

	if m.modelTab == ModelTabRaw {
		return m.viewRawModels()
	}

	if len(m.items) == 0 {
		return MutedStyle.Render("  No virtual models configured. Create one via API.")
	}

	if m.detailMode {
		return m.viewDetail()
	}

	return m.viewOverview()
}

func (m VMViewModel) viewRawModels() string {
	if len(m.rawModels) == 0 {
		return MutedStyle.Render("  No raw models found. Discover models from providers first.")
	}

	// Collect all tag keys across all raw models for column headers
	seen := make(map[string]bool)
	var tagKeys []string
	for _, models := range m.rawModels {
		for _, rm := range models {
			for k := range rm.Tags {
				if !seen[k] {
					seen[k] = true
					tagKeys = append(tagKeys, k)
				}
			}
		}
	}

	// Build a flat list with provider info for cursor tracking
	type rawEntry struct {
		provider string
		model    RawModelInfo
	}
	var allEntries []rawEntry
	for _, provider := range m.rawProviders {
		for _, rm := range m.rawModels[provider] {
			allEntries = append(allEntries, rawEntry{provider: provider, model: rm})
		}
	}

	var b strings.Builder

	// Find which provider group the cursor is in
	cursorIdx := 0
	for _, provider := range m.rawProviders {
		models := m.rawModels[provider]
		b.WriteString(fmt.Sprintf("  %s%s (%d models)\n",
			InfoStyle.Render(provider),
			MutedStyle.Render(fmt.Sprintf("")),
			len(models),
		))

		// Column header
		header := "    "
		header += padRight("model", 5)
		for _, key := range tagKeys {
			header += "  " + padRight(abbrevKey(key), len(abbrevKey(key)))
		}
		b.WriteString(MutedStyle.Render(header))

		// Separator
		sep := "    " + strings.Repeat("-", 5)
		for _, key := range tagKeys {
			sep += "  " + strings.Repeat("-", len(abbrevKey(key)))
		}
		b.WriteString(MutedStyle.Render(sep))
		b.WriteString("\n")

		for _, rm := range models {
			cursor := "  "
			if cursorIdx == m.rawCursor {
				cursor = SuccessStyle.Render("▸ ")
			}

			line := "    " + cursor
			line += padRight(trunc(rm.Name, 5), 5)
			for _, key := range tagKeys {
				val := rm.Tags[key]
				if val == "" {
					val = "-"
				}
				line += "  " + padRight(val, len(abbrevKey(key)))
			}

			if cursorIdx == m.rawCursor {
				b.WriteString(SuccessStyle.Render(line))
			} else {
				b.WriteString(line)
			}
			b.WriteString("\n")
			cursorIdx++
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m VMViewModel) viewOverview() string {
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
				limit := 10
				renderResolvedTable(&b, vm, limit, -1)
				if len(vm.ResolvedModels) > limit {
					b.WriteString(MutedStyle.Render(fmt.Sprintf("    ... and %d more (press Enter for full view)\n", len(vm.ResolvedModels)-limit)))
				}
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m VMViewModel) viewDetail() string {
	vm := m.items[m.cursor]

	var b strings.Builder

	b.WriteString(fmt.Sprintf("  %s%s\n", SuccessStyle.Render("▸ "), InfoStyle.Render(vm.Name)))
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
		b.WriteString(StatHeaderStyle.Render(fmt.Sprintf("    Resolved Models (%d)", len(vm.ResolvedModels))))
		b.WriteString("\n")
		renderResolvedTable(&b, vm, 0, m.resolvedCursor)
	}

	b.WriteString("\n")
	b.WriteString(HelpStyle.Render("    esc: back  ↑/↓: navigate\n"))

	return b.String()
}

func renderResolvedTable(b *strings.Builder, vm VirtualModelInfo, limit int, highlightRow int) {
	cols := computeColumns(vm.SortExpr, vm.FilterExpr, vm.ResolvedModels)

	models := vm.ResolvedModels
	if limit > 0 && len(models) > limit {
		models = models[:limit]
	}

	rows := make([]tableRow, len(models))
	for i, rm := range models {
		effort := rm.ReasoningEffort
		if effort == "" {
			effort = "-"
		}
		rows[i] = tableRow{
			Position: fmt.Sprintf("#%d", rm.Position),
			Provider: rm.ProviderName,
			Model:    rm.ModelName,
			Effort:   effort,
			Values:   rm.Tags,
		}
	}

	colWidths := computeColWidths(cols, rows)

	// Header
	header := "    "
	header += padRight("#", colWidths[0]) + "  "
	header += padRight("provider", colWidths[1]) + "  "
	header += padRight("model", colWidths[2]) + "  "
	header += padRight("effort", colWidths[3])
	for j, col := range cols {
		header += "  " + padRight(col.Abbrev, colWidths[4+j])
	}
	b.WriteString(MutedStyle.Render(header))

	// Separator
	sep := "    " + strings.Repeat("-", colWidths[0]) + "  " +
		strings.Repeat("-", colWidths[1]) + "  " +
		strings.Repeat("-", colWidths[2]) + "  " +
		strings.Repeat("-", colWidths[3])
	for _, w := range colWidths[4:] {
		sep += "  " + strings.Repeat("-", w)
	}
	b.WriteString(MutedStyle.Render(sep))
	b.WriteString("\n")

	// Data rows
	for i, r := range rows {
		line := "    "
		prefix := "  "
		if i == highlightRow {
			prefix = SuccessStyle.Render("▸ ")
		}
		line += prefix
		line += padRight(r.Position, colWidths[0]) + "  "
		line += padRight(trunc(r.Provider, colWidths[1]), colWidths[1]) + "  "
		line += padRight(trunc(r.Model, colWidths[2]), colWidths[2]) + "  "
		line += padRight(r.Effort, colWidths[3])

		for j, col := range cols {
			val := r.Values[col.Key]
			if val == "" {
				val = "-"
			}
			line += "  " + padRight(val, colWidths[4+j])
		}

		if i == highlightRow {
			b.WriteString(SuccessStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
}

type tableCol struct {
	Key    string
	Abbrev string
}

type tableRow struct {
	Position string
	Provider string
	Model    string
	Effort   string
	Values   map[string]string
}

func computeColumns(sortExprJSON, filterExprJSON json.RawMessage, resolvedModels []ResolvedModelInfo) []tableCol {
	seen := make(map[string]bool)
	var cols []tableCol

	// Sort keys first
	var sortExprParsed models.SortExpr
	if len(sortExprJSON) > 0 {
		json.Unmarshal(sortExprJSON, &sortExprParsed)
	}
	for _, s := range sortExprParsed {
		if !seen[s.Key] {
			seen[s.Key] = true
			cols = append(cols, tableCol{Key: s.Key, Abbrev: abbrevKey(s.Key)})
		}
	}

	// Filter keys second
	var filterExprParsed models.FilterExpr
	if len(filterExprJSON) > 0 {
		json.Unmarshal(filterExprJSON, &filterExprParsed)
	}
	for _, f := range filterExprParsed.And {
		if !seen[f.Key] {
			seen[f.Key] = true
			cols = append(cols, tableCol{Key: f.Key, Abbrev: abbrevKey(f.Key)})
		}
	}

	// Remaining tags
	for _, m := range resolvedModels {
		for k := range m.Tags {
			if !seen[k] {
				seen[k] = true
				cols = append(cols, tableCol{Key: k, Abbrev: abbrevKey(k)})
			}
		}
	}

	return cols
}

func abbrevKey(key string) string {
	switch key {
	case "mc.intelligence", "intelligence":
		return "int"
	case "mc.coding", "coding":
		return "code"
	case "mc.speed", "speed":
		return "spd"
	case "mc.cost_per_task", "cost_per_task":
		return "$/task"
	case "mc.cost_per_1m_input", "cost_per_1m_input":
		return "$/1M"
	case "mc.cost_per_1m_output", "cost_per_1m_output":
		return "$/1M.out"
	case "mc.cost_per_1m_cache", "cost_per_1m_cache":
		return "$/1M.cache"
	case "mc.hallucination", "hallucination":
		return "hall"
	case "mc.latency", "latency":
		return "lat"
	case "m.context_window", "context_window":
		return "ctx"
	case "mc.has_reasoning_effort", "has_reasoning_effort":
		return "has_effort"
	case "mc.reasoning", "reasoning":
		return "reason"
	default:
		return key
	}
}

func computeColWidths(cols []tableCol, rows []tableRow) []int {
	widths := []int{2, 8, 5, 6} // #, provider, model, effort minimums

	for _, col := range cols {
		w := len(col.Abbrev)
		if w < 4 {
			w = 4
		}
		widths = append(widths, w)
	}

	for _, r := range rows {
		if len(r.Position) > widths[0] {
			widths[0] = len(r.Position)
		}
		if len(r.Provider) > widths[1] {
			widths[1] = len(r.Provider)
		}
		if len(r.Model) > widths[2] {
			widths[2] = len(r.Model)
		}
		if len(r.Effort) > widths[3] {
			widths[3] = len(r.Effort)
		}
		for j, col := range cols {
			val := r.Values[col.Key]
			if len(val) > widths[4+j] {
				widths[4+j] = len(val)
			}
		}
	}

	return widths
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func trunc(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func (m *VMViewModel) SetSize(w, h int) {
	m.width = w
	m.height = h
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
					ModelName:       r.ModelName,
					ProviderName:    r.ProviderName,
					ReasoningEffort: r.ReasoningEffort,
					Tags:            r.Tags,
					Position:        i + 1,
				})
			}

			filterJSON, _ := json.Marshal(vm.FilterExpr)
			sortJSON, _ := json.Marshal(vm.SortExpr)
			items = append(items, VirtualModelInfo{
				Name:           vm.Name,
				Filter:         formatFilter(vm.FilterExpr),
				Sort:           formatSort(vm.SortExpr),
				FilterExpr:     filterJSON,
				SortExpr:       sortJSON,
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
				efforts, _ := tagRepo.GetAvailableEfforts(ctx, m.ID)
				if len(efforts) == 0 {
					efforts = []string{""}
				}

				for _, effort := range efforts {
					tags, _ := tagRepo.GetByModelEffort(ctx, m.ID, effort)
					tagMap := make(map[string]string)
					for _, t := range tags {
						tagMap["mc."+t.Key] = t.Value
					}

					if !matchesFilter(tagMap, filterExpr) {
						continue
					}

					provider, _ := providerRepo.GetByID(ctx, m.ProviderID)
					if provider == nil {
						continue
					}

					resolved = append(resolved, ResolvedModelInfo{
						ModelName:       m.Name,
						ProviderName:    provider.Name,
						ReasoningEffort: effort,
						Tags:            tagMap,
					})
				}
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
				FilterExpr:     vm.FilterExpr,
				SortExpr:       vm.SortExpr,
				ResolvedModels: resolved,
			})
		}

		return VMViewMsg{VirtualModels: items}
	}
}

func FetchRawModelsLocal(modelRepo repository.ModelRepository, tagRepo repository.TagRepository, providerRepo repository.ProviderRepository) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		allModels, err := modelRepo.ListAll(ctx)
		if err != nil {
			return RawModelListMsg{}
		}

		rawModels := make(map[string][]RawModelInfo)
		for _, m := range allModels {
			provider, _ := providerRepo.GetByID(ctx, m.ProviderID)
			if provider == nil {
				continue
			}

			tags, _ := tagRepo.GetByModel(ctx, m.ID)
			tagMap := make(map[string]string)
			for _, t := range tags {
				tagMap["mc."+t.Key] = t.Value
			}

			rawModels[provider.Name] = append(rawModels[provider.Name], RawModelInfo{
				Name: m.Name,
				Tags: tagMap,
			})
		}

		return RawModelListMsg{RawModels: rawModels}
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

			aMissing := valI == ""
			bMissing := valJ == ""
			if aMissing && bMissing {
				continue
			}
			if aMissing {
				return false
			}
			if bMissing {
				return true
			}

			if s.Direction != "" {
				cmp := compareNumeric(valI, valJ)
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

func FetchRawModels(client *APIClient) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return RawModelListMsg{}
		}

		models, err := client.GetModels()
		if err != nil {
			return RawModelListMsg{}
		}

		rawModels := make(map[string][]RawModelInfo)
		for _, m := range models {
			tagMap := make(map[string]string)
			for _, t := range m.Tags {
				if t.ReasoningEffort == "" || t.ReasoningEffort == "default" {
					tagMap["mc."+t.Key] = t.Value
				}
			}
			rawModels[m.ProviderName] = append(rawModels[m.ProviderName], RawModelInfo{
				Name: m.Name,
				Tags: tagMap,
			})
		}

		return RawModelListMsg{RawModels: rawModels}
	}
}
