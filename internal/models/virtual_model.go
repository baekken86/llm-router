package models

import (
	"encoding/json"
	"fmt"
	"time"
)

type VirtualModel struct {
	ID            int64            `json:"id"`
	Name          string           `json:"name"`
	Description   string           `json:"description"`
	FilterExpr    json.RawMessage  `json:"filter_expr"`
	SortExpr      json.RawMessage  `json:"sort_expr"`
	IncludeModels json.RawMessage  `json:"include_models"`
	Composition   *CompositionNode `json:"composition,omitempty"`
	MaxRetries    int              `json:"max_retries"`
	RetryOnStatus json.RawMessage  `json:"retry_on_status"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type CreateVirtualModelRequest struct {
	Name          string           `json:"name"`
	Description   string           `json:"description,omitempty"`
	FilterExpr    json.RawMessage  `json:"filter_expr,omitempty"`
	SortExpr      json.RawMessage  `json:"sort_expr,omitempty"`
	IncludeModels json.RawMessage  `json:"include_models,omitempty"`
	Composition   *CompositionNode `json:"composition,omitempty"`
	MaxRetries    *int             `json:"max_retries,omitempty"`
	RetryOnStatus json.RawMessage  `json:"retry_on_status,omitempty"`
}

type UpdateVirtualModelRequest struct {
	Name          *string           `json:"name,omitempty"`
	Description   *string           `json:"description,omitempty"`
	FilterExpr    *json.RawMessage  `json:"filter_expr,omitempty"`
	SortExpr      *json.RawMessage  `json:"sort_expr,omitempty"`
	IncludeModels *json.RawMessage  `json:"include_models,omitempty"`
	Composition   *CompositionNode  `json:"composition,omitempty"`
	MaxRetries    *int              `json:"max_retries,omitempty"`
	RetryOnStatus *json.RawMessage  `json:"retry_on_status,omitempty"`
}

type PreviewVirtualModelRequest struct {
	FilterExpr    json.RawMessage  `json:"filter_expr,omitempty"`
	SortExpr      json.RawMessage  `json:"sort_expr,omitempty"`
	IncludeModels json.RawMessage  `json:"include_models,omitempty"`
	Composition   *CompositionNode `json:"composition,omitempty"`
}

// FilterNode is a recursive filter expression.
// A node is either a leaf condition (key+op+value) or a logical operator (and/or/not).
type FilterNode struct {
	// Leaf fields
	Key   string      `json:"key,omitempty"`
	Op    string      `json:"op,omitempty"`
	Value interface{} `json:"value,omitempty"`

	// Logical operator fields
	And []FilterNode `json:"and,omitempty"`
	Or  []FilterNode `json:"or,omitempty"`
	Not *FilterNode  `json:"not,omitempty"`
}

// IsLeaf returns true if this is a simple condition (key+op+value).
func (n FilterNode) IsLeaf() bool {
	return n.Key != ""
}

// CollectKeys recursively collects all leaf keys from the filter tree.
func (n FilterNode) CollectKeys() []string {
	if n.IsLeaf() {
		return []string{n.Key}
	}
	var keys []string
	for _, child := range n.And {
		keys = append(keys, child.CollectKeys()...)
	}
	for _, child := range n.Or {
		keys = append(keys, child.CollectKeys()...)
	}
	if n.Not != nil {
		keys = append(keys, n.Not.CollectKeys()...)
	}
	return keys
}

type SortExpr []SortEntry

// SortEntry is a union type: either a predicate sort or a field sort.
// Predicate: Condition is set → sort by whether condition is true (asc=true first, desc=false first)
// Field: Key is set → sort by field value
type SortEntry struct {
	Condition *FilterNode `json:"condition,omitempty"`
	Key       string      `json:"key,omitempty"`
	Direction string      `json:"direction,omitempty"`
	Order     []string    `json:"order,omitempty"`
}

// IsCondition returns true if this entry is a predicate sort.
func (e SortEntry) IsCondition() bool {
	return e.Condition != nil
}

// IncludeModelRef is a reference to a specific model on a specific provider.
type IncludeModelRef struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// CompositionNode is a recursive tree node for composing virtual models.
// Each node is either a VM reference (Vm set) or an operation (Operation set).
// Every node can optionally filter and sort its results.
type CompositionNode struct {
	// VM Reference (leaf of the tree)
	Vm string `json:"vm,omitempty"`

	// Operation node (recursive)
	Operation string            `json:"operation,omitempty"` // "union", "intersection", "difference"
	Sources   []CompositionNode `json:"sources,omitempty"`

	// Optional filter/sort - valid on ANY node type (VM ref or operation)
	FilterExpr *FilterNode `json:"filter_expr,omitempty"`
	SortExpr   SortExpr    `json:"sort_expr,omitempty"`
}

// IsVMRef returns true if this node references an existing VM.
func (n *CompositionNode) IsVMRef() bool {
	return n.Vm != ""
}

// IsOperation returns true if this node is a set operation.
func (n *CompositionNode) IsOperation() bool {
	return n.Operation != ""
}

// CollectVMNames recursively collects all unique VM names referenced in this tree.
func (n *CompositionNode) CollectVMNames() []string {
	if n == nil {
		return nil
	}
	seen := make(map[string]bool)
	var names []string
	n.collectVMNamesHelper(seen, &names)
	return names
}

func (n *CompositionNode) collectVMNamesHelper(seen map[string]bool, names *[]string) {
	if n == nil {
		return
	}
	if n.Vm != "" && !seen[n.Vm] {
		seen[n.Vm] = true
		*names = append(*names, n.Vm)
	}
	for i := range n.Sources {
		n.Sources[i].collectVMNamesHelper(seen, names)
	}
}

// ValidateCompositionNode recursively validates a composition tree.
func ValidateCompositionNode(node *CompositionNode, depth int) error {
	if node == nil {
		return nil
	}
	if depth > 10 {
		return fmt.Errorf("composition tree exceeds maximum depth of 10")
	}

	hasVM := node.Vm != ""
	hasOp := node.Operation != ""

	if hasVM && hasOp {
		return fmt.Errorf("composition node cannot have both vm and operation")
	}
	if !hasVM && !hasOp {
		return fmt.Errorf("composition node must have either vm or operation")
	}

	if hasOp {
		validOps := map[string]bool{"union": true, "intersection": true, "difference": true}
		if !validOps[node.Operation] {
			return fmt.Errorf("unknown composition operation: %s", node.Operation)
		}
		if len(node.Sources) < 2 {
			return fmt.Errorf("composition operation %s requires at least 2 sources", node.Operation)
		}
		for i := range node.Sources {
			if err := ValidateCompositionNode(&node.Sources[i], depth+1); err != nil {
				return fmt.Errorf("source[%d]: %w", i, err)
			}
		}
	}

	return nil
}

// ParseCompositionJSON parses a JSON byte slice into a CompositionNode.
func ParseCompositionJSON(data []byte, node *CompositionNode) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, node)
}

// MarshalComposition marshals a CompositionNode to JSON.
func MarshalComposition(node *CompositionNode) ([]byte, error) {
	if node == nil {
		return nil, nil
	}
	return json.Marshal(node)
}
