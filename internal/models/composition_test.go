package models

import (
	"encoding/json"
	"testing"
)

func TestValidateCompositionNode(t *testing.T) {
	tests := []struct {
		name    string
		node    *CompositionNode
		wantErr bool
	}{
		{
			name:    "nil node is valid",
			node:    nil,
			wantErr: false,
		},
		{
			name:    "valid VM ref",
			node:    &CompositionNode{Vm: "my-vm"},
			wantErr: false,
		},
		{
			name: "valid union",
			node: &CompositionNode{
				Operation: "union",
				Sources: []CompositionNode{
					{Vm: "a"},
					{Vm: "b"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid intersection",
			node: &CompositionNode{
				Operation: "intersection",
				Sources: []CompositionNode{
					{Vm: "a"},
					{Vm: "b"},
					{Vm: "c"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid difference",
			node: &CompositionNode{
				Operation: "difference",
				Sources: []CompositionNode{
					{Vm: "a"},
					{Vm: "b"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid nested",
			node: &CompositionNode{
				Operation: "intersection",
				Sources: []CompositionNode{
					{
						Operation: "union",
						Sources: []CompositionNode{
							{Vm: "a"},
							{Vm: "b"},
						},
					},
					{Vm: "c"},
				},
			},
			wantErr: false,
		},
		{
			name:    "empty node is valid (all models source)",
			node:    &CompositionNode{},
			wantErr: false,
		},
		{
			name: "both vm and operation",
			node: &CompositionNode{
				Vm:        "a",
				Operation: "union",
			},
			wantErr: true,
		},
		{
			name: "unknown operation",
			node: &CompositionNode{
				Operation: "xor",
				Sources: []CompositionNode{
					{Vm: "a"},
					{Vm: "b"},
				},
			},
			wantErr: true,
		},
		{
			name: "union with single source",
			node: &CompositionNode{
				Operation: "union",
				Sources: []CompositionNode{
					{Vm: "a"},
				},
			},
			wantErr: true,
		},
		{
			name: "union with no sources",
			node: &CompositionNode{
				Operation: "union",
				Sources:   []CompositionNode{},
			},
			wantErr: true,
		},
		{
			name:    "valid filter source",
			node:    &CompositionNode{FilterExpr: &FilterNode{Key: "mc.coding", Op: "gte", Value: float64(8)}},
			wantErr: false,
		},
		{
			name: "filter source with sources",
			node: &CompositionNode{
				FilterExpr: &FilterNode{Key: "x", Op: "eq", Value: "y"},
				Sources:    []CompositionNode{{Vm: "a"}},
			},
			wantErr: true,
		},
		{
			name: "vm ref with post-filter",
			node: &CompositionNode{
				Vm:         "a",
				FilterExpr: &FilterNode{Key: "x", Op: "eq", Value: "y"},
			},
			wantErr: false,
		},
		{
			name: "mixed tree: union(VM-ref, filter-source)",
			node: &CompositionNode{
				Operation: "union",
				Sources: []CompositionNode{
					{Vm: "a"},
					{FilterExpr: &FilterNode{Key: "mc.coding", Op: "gte", Value: float64(8)}},
				},
			},
			wantErr: false,
		},
		{
			name: "filter source at nested depth",
			node: &CompositionNode{
				Operation: "intersection",
				Sources: []CompositionNode{
					{
						Operation: "union",
						Sources: []CompositionNode{
							{Vm: "a"},
							{FilterExpr: &FilterNode{Key: "x", Op: "eq", Value: "y"}},
						},
					},
					{Vm: "c"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCompositionNode(tt.node, 0)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCompositionNode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateCompositionNode_MaxDepth(t *testing.T) {
	// Build a tree that exceeds depth 10
	node := &CompositionNode{Vm: "deep"}
	for i := 0; i < 11; i++ {
		node = &CompositionNode{
			Operation: "union",
			Sources: []CompositionNode{
				*node,
				{Vm: "sibling"},
			},
		}
	}

	err := ValidateCompositionNode(node, 0)
	if err == nil {
		t.Error("expected error for depth > 10, got nil")
	}
}

func TestCollectVMNames(t *testing.T) {
	tests := []struct {
		name     string
		node     *CompositionNode
		expected []string
	}{
		{
			name:     "nil node",
			node:     nil,
			expected: nil,
		},
		{
			name:     "single VM ref",
			node:     &CompositionNode{Vm: "a"},
			expected: []string{"a"},
		},
		{
			name: "nested tree",
			node: &CompositionNode{
				Operation: "intersection",
				Sources: []CompositionNode{
					{
						Operation: "union",
						Sources: []CompositionNode{
							{Vm: "a"},
							{Vm: "b"},
						},
					},
					{Vm: "c"},
				},
			},
			expected: []string{"a", "b", "c"},
		},
		{
			name: "deduplicates VM names",
			node: &CompositionNode{
				Operation: "union",
				Sources: []CompositionNode{
					{Vm: "a"},
					{Vm: "a"},
				},
			},
			expected: []string{"a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.node.CollectVMNames()
			if len(result) != len(tt.expected) {
				t.Errorf("CollectVMNames() returned %d items, want %d", len(result), len(tt.expected))
				return
			}
			for i, name := range result {
				if name != tt.expected[i] {
					t.Errorf("CollectVMNames()[%d] = %q, want %q", i, name, tt.expected[i])
				}
			}
		})
	}
}

func TestCompositionJSONRoundtrip(t *testing.T) {
	original := &CompositionNode{
		Operation: "intersection",
		Sources: []CompositionNode{
			{
				Operation: "union",
				Sources: []CompositionNode{
					{Vm: "fast-cheap"},
					{Vm: "coding-models"},
				},
			},
			{Vm: "anthropic-only"},
		},
		FilterExpr: &FilterNode{
			Key: "mc.coding",
			Op:  "gte",
			Value: float64(70),
		},
	}

	data, err := MarshalComposition(original)
	if err != nil {
		t.Fatalf("MarshalComposition() error = %v", err)
	}

	var parsed CompositionNode
	if err := ParseCompositionJSON(data, &parsed); err != nil {
		t.Fatalf("ParseCompositionJSON() error = %v", err)
	}

	if parsed.Operation != "intersection" {
		t.Errorf("Operation = %q, want %q", parsed.Operation, "intersection")
	}
	if len(parsed.Sources) != 2 {
		t.Fatalf("Sources len = %d, want 2", len(parsed.Sources))
	}
	if parsed.Sources[0].Operation != "union" {
		t.Errorf("Sources[0].Operation = %q, want %q", parsed.Sources[0].Operation, "union")
	}
	if len(parsed.Sources[0].Sources) != 2 {
		t.Fatalf("Sources[0].Sources len = %d, want 2", len(parsed.Sources[0].Sources))
	}
	if parsed.Sources[0].Sources[0].Vm != "fast-cheap" {
		t.Errorf("Sources[0].Sources[0].Vm = %q, want %q", parsed.Sources[0].Sources[0].Vm, "fast-cheap")
	}
	if parsed.Sources[1].Vm != "anthropic-only" {
		t.Errorf("Sources[1].Vm = %q, want %q", parsed.Sources[1].Vm, "anthropic-only")
	}

	// Verify new-format "collection" key also works
	var parsed2 CompositionNode
	if err := json.Unmarshal([]byte(`{"collection":"my-vm","filter_expr":{"key":"mc.coding","op":"gte","value":8}}`), &parsed2); err != nil {
		t.Fatalf("Unmarshal new format: %v", err)
	}
	if parsed2.Vm != "my-vm" {
		t.Errorf("Vm = %q, want my-vm", parsed2.Vm)
	}
	if parsed.FilterExpr == nil || parsed.FilterExpr.Key != "mc.coding" {
		t.Errorf("FilterExpr.Key = %v, want mc.coding", parsed.FilterExpr)
	}
}

func TestIsSource(t *testing.T) {
	tests := []struct {
		name string
		node *CompositionNode
		want bool
	}{
		{
			name: "filter source (all models)",
			node: &CompositionNode{FilterExpr: &FilterNode{Key: "mc.coding", Op: "gte", Value: float64(8)}},
			want: true,
		},
		{
			name: "vm ref (source with collection)",
			node: &CompositionNode{Vm: "a"},
			want: true,
		},
		{
			name: "vm ref with post-filter",
			node: &CompositionNode{Vm: "a", FilterExpr: &FilterNode{Key: "x", Op: "eq", Value: "y"}},
			want: true,
		},
		{
			name: "operation",
			node: &CompositionNode{Operation: "union", Sources: []CompositionNode{{Vm: "a"}, {Vm: "b"}}},
			want: false,
		},
		{
			name: "empty node (all models, no filter)",
			node: &CompositionNode{},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.node.IsSource()
			if got != tt.want {
				t.Errorf("IsSource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMarshalCompositionNil(t *testing.T) {
	data, err := MarshalComposition(nil)
	if err != nil {
		t.Fatalf("MarshalComposition(nil) error = %v", err)
	}
	if data != nil {
		t.Errorf("MarshalComposition(nil) = %v, want nil", data)
	}
}

func TestParseCompositionJSONEmpty(t *testing.T) {
	var node CompositionNode
	if err := ParseCompositionJSON(nil, &node); err != nil {
		t.Fatalf("ParseCompositionJSON(nil) error = %v", err)
	}
	if err := ParseCompositionJSON([]byte{}, &node); err != nil {
		t.Fatalf("ParseCompositionJSON(empty) error = %v", err)
	}
}

func TestVirtualModelJSONRoundtrip(t *testing.T) {
	vm := VirtualModel{
		Name:        "test-vm",
		Description: "A test virtual model",
		FilterExpr:  json.RawMessage(`{"and":[{"key":"mc.coding","op":"gte","value":80}]}`),
		Composition: &CompositionNode{
			Operation: "union",
			Sources: []CompositionNode{
				{Vm: "a"},
				{Vm: "b"},
			},
		},
	}

	data, err := json.Marshal(vm)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var parsed VirtualModel
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if parsed.Name != "test-vm" {
		t.Errorf("Name = %q, want %q", parsed.Name, "test-vm")
	}
	if parsed.Description != "A test virtual model" {
		t.Errorf("Description = %q, want %q", parsed.Description, "A test virtual model")
	}
	if parsed.Composition == nil {
		t.Fatal("Composition is nil after roundtrip")
	}
	if parsed.Composition.Operation != "union" {
		t.Errorf("Composition.Operation = %q, want %q", parsed.Composition.Operation, "union")
	}
	if len(parsed.Composition.Sources) != 2 {
		t.Errorf("Composition.Sources len = %d, want 2", len(parsed.Composition.Sources))
	}
}
