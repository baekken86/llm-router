package service

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/chris/llm-router/internal/models"
)

func TestVM38SortMerge(t *testing.T) {
	comp := `{"filter_expr":{"and":[{"key":"mc.coding","op":"gte","value":50},{"key":"mc.cost_per_1m_input","op":"lte","value":10},{"key":"mc.hallucination","op":"gte","value":35},{"key":"mc.intelligence","op":"gte","value":40}]},"sort_expr":[{"condition":{"and":[{"key":"p.name","op":"eq","value":"chatgpt"}]},"priority":-2},{"key":"mc.cost_per_1m_input","direction":"asc"},{"key":"mc.cost_per_task","direction":"asc"}]}`
	node := &models.CompositionNode{}
	if err := models.ParseCompositionJSON([]byte(comp), node); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(node.SortExpr)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("root sort entries:", len(node.SortExpr), "json:", string(data))

	// simulate ResolveModels composite path: sortSource = vm.SortExpr (empty) -> root SortExpr
	sortSource := json.RawMessage(nil)
	if len(sortSource) == 0 && len(node.SortExpr) > 0 {
		if b, err := json.Marshal(node.SortExpr); err == nil {
			sortSource = b
		}
	}
	merged, ok := mergeSortByPriority(nil, map[int64]bool{}, sortSource)
	fmt.Println("merged ok:", ok, "count:", len(merged))
	for _, e := range merged {
		b, _ := json.Marshal(e)
		fmt.Println(string(b))
	}
}
