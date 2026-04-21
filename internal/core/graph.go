package core

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/acassiovilasboas/genoma/internal/shared"
)

// EdgeOperator defines comparison operators for conditional edges.
type EdgeOperator string

const (
	OpEqual      EdgeOperator = "eq"
	OpNotEqual   EdgeOperator = "neq"
	OpContains   EdgeOperator = "contains"
	OpGreaterThn EdgeOperator = "gt"
	OpLessThan   EdgeOperator = "lt"
	OpExists     EdgeOperator = "exists"
)

// Edge connects two nodes in the graph with an optional condition.
type Edge struct {
	// FromNodeID is the source node.
	FromNodeID string `json:"from_node_id"`

	// ToNodeID is the destination node.
	ToNodeID string `json:"to_node_id"`

	// Condition is evaluated against the source node's output.
	// If nil, the edge is unconditional.
	Condition *EdgeCondition `json:"condition,omitempty"`

	// MaxCycles limits how many times this edge can be traversed
	// in a single flow run (prevents infinite loops). Default: 3.
	MaxCycles int `json:"max_cycles"`

	// IsFeedback marks this edge as a feedback/critique loop edge.
	IsFeedback bool `json:"is_feedback"`
}

// EdgeCondition defines when a conditional edge should be followed.
type EdgeCondition struct {
	// Field is the JSON path in the output to evaluate (e.g., "status", "result.approved").
	Field string `json:"field"`

	// Operator is the comparison operator.
	Operator EdgeOperator `json:"operator"`

	// Value is the expected value for comparison.
	Value any `json:"value"`
}

// Evaluate checks if the condition is met against the given output data.
func (ec *EdgeCondition) Evaluate(output map[string]any) bool {
	if ec == nil {
		return true // No condition = always true
	}

	val, exists := output[ec.Field]

	switch ec.Operator {
	case OpExists:
		return exists
	case OpEqual:
		return fmt.Sprintf("%v", val) == fmt.Sprintf("%v", ec.Value)
	case OpNotEqual:
		return fmt.Sprintf("%v", val) != fmt.Sprintf("%v", ec.Value)
	case OpContains:
		if s, ok := val.(string); ok {
			if target, ok := ec.Value.(string); ok {
				return len(s) >= len(target) && containsStr(s, target)
			}
		}
		return false
	case OpGreaterThn:
		return toFloat(val) > toFloat(ec.Value)
	case OpLessThan:
		return toFloat(val) < toFloat(ec.Value)
	default:
		return false
	}
}

// FlowGraph represents a directed cyclic graph of interconnected nodes.
// It defines the structure of a processing flow — which nodes execute,
// in what order, and under what conditions.
type FlowGraph struct {
	// ID is the unique identifier for this flow.
	ID string `json:"id"`

	// Name is the human-readable name.
	Name string `json:"name"`

	// Description describes what this flow does (used for semantic routing).
	Description string `json:"description"`

	// Nodes maps node IDs to their definitions.
	Nodes map[string]*NodeDefinition `json:"nodes"`

	// Edges defines connections between nodes.
	Edges []*Edge `json:"edges"`

	// EntryNodeID is the starting node of the flow.
	EntryNodeID string `json:"entry_node_id"`

	// Metadata holds arbitrary key-value pairs.
	Metadata map[string]any `json:"metadata,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	mu sync.RWMutex
}

// NewFlowGraph creates a new empty flow graph.
func NewFlowGraph(name, description string) *FlowGraph {
	return &FlowGraph{
		ID:          shared.NewID(),
		Name:        name,
		Description: description,
		Nodes:       make(map[string]*NodeDefinition),
		Edges:       make([]*Edge, 0),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// AddNode adds a node definition to the graph.
func (fg *FlowGraph) AddNode(node *NodeDefinition) error {
	fg.mu.Lock()
	defer fg.mu.Unlock()

	if _, exists := fg.Nodes[node.ID]; exists {
		return &ErrNodeAlreadyExists{NodeID: node.ID}
	}
	fg.Nodes[node.ID] = node
	fg.UpdatedAt = time.Now()
	return nil
}

// RemoveNode removes a node and all its edges from the graph.
func (fg *FlowGraph) RemoveNode(nodeID string) error {
	fg.mu.Lock()
	defer fg.mu.Unlock()

	if _, exists := fg.Nodes[nodeID]; !exists {
		return &ErrNodeNotFound{NodeID: nodeID}
	}

	delete(fg.Nodes, nodeID)

	// Remove edges referencing this node
	filtered := make([]*Edge, 0, len(fg.Edges))
	for _, e := range fg.Edges {
		if e.FromNodeID != nodeID && e.ToNodeID != nodeID {
			filtered = append(filtered, e)
		}
	}
	fg.Edges = filtered

	if fg.EntryNodeID == nodeID {
		fg.EntryNodeID = ""
	}

	fg.UpdatedAt = time.Now()
	return nil
}

// AddEdge connects two nodes with an optional condition.
func (fg *FlowGraph) AddEdge(fromID, toID string, condition *EdgeCondition) error {
	fg.mu.Lock()
	defer fg.mu.Unlock()

	if _, exists := fg.Nodes[fromID]; !exists {
		return &ErrNodeNotFound{NodeID: fromID}
	}
	if _, exists := fg.Nodes[toID]; !exists {
		return &ErrNodeNotFound{NodeID: toID}
	}

	edge := &Edge{
		FromNodeID: fromID,
		ToNodeID:   toID,
		Condition:  condition,
		MaxCycles:  3, // default
	}

	fg.Edges = append(fg.Edges, edge)
	fg.UpdatedAt = time.Now()
	return nil
}

// AddFeedbackEdge adds a feedback/critique loop edge between two nodes.
func (fg *FlowGraph) AddFeedbackEdge(fromID, toID string, condition *EdgeCondition, maxCycles int) error {
	fg.mu.Lock()
	defer fg.mu.Unlock()

	if _, exists := fg.Nodes[fromID]; !exists {
		return &ErrNodeNotFound{NodeID: fromID}
	}
	if _, exists := fg.Nodes[toID]; !exists {
		return &ErrNodeNotFound{NodeID: toID}
	}

	if maxCycles <= 0 {
		maxCycles = 3
	}

	edge := &Edge{
		FromNodeID: fromID,
		ToNodeID:   toID,
		Condition:  condition,
		MaxCycles:  maxCycles,
		IsFeedback: true,
	}

	fg.Edges = append(fg.Edges, edge)
	fg.UpdatedAt = time.Now()
	return nil
}

// SetEntryNode sets the starting node of the flow.
func (fg *FlowGraph) SetEntryNode(nodeID string) error {
	fg.mu.RLock()
	defer fg.mu.RUnlock()

	if _, exists := fg.Nodes[nodeID]; !exists {
		return &ErrNodeNotFound{NodeID: nodeID}
	}
	fg.EntryNodeID = nodeID
	return nil
}

// GetNextNodes returns the nodes that should execute after the given node,
// based on the node's output and edge conditions.
func (fg *FlowGraph) GetNextNodes(nodeID string, output map[string]any) []string {
	fg.mu.RLock()
	defer fg.mu.RUnlock()

	var next []string
	for _, edge := range fg.Edges {
		if edge.FromNodeID != nodeID {
			continue
		}
		if edge.Condition == nil || edge.Condition.Evaluate(output) {
			next = append(next, edge.ToNodeID)
		}
	}
	return next
}

// GetEdge returns the edge between two specific nodes, if it exists.
func (fg *FlowGraph) GetEdge(fromID, toID string) *Edge {
	fg.mu.RLock()
	defer fg.mu.RUnlock()

	for _, edge := range fg.Edges {
		if edge.FromNodeID == fromID && edge.ToNodeID == toID {
			return edge
		}
	}
	return nil
}

// Validate checks the graph for structural integrity.
func (fg *FlowGraph) Validate() error {
	fg.mu.RLock()
	defer fg.mu.RUnlock()

	if len(fg.Nodes) == 0 {
		return &ErrInvalidGraph{Reason: "graph has no nodes"}
	}

	if fg.EntryNodeID == "" {
		return &ErrInvalidGraph{Reason: "no entry node defined"}
	}

	if _, exists := fg.Nodes[fg.EntryNodeID]; !exists {
		return &ErrInvalidGraph{Reason: fmt.Sprintf("entry node %s not found in graph", fg.EntryNodeID)}
	}

	// Validate all edges reference existing nodes
	for _, edge := range fg.Edges {
		if _, exists := fg.Nodes[edge.FromNodeID]; !exists {
			return &ErrInvalidGraph{Reason: fmt.Sprintf("edge references non-existent source node: %s", edge.FromNodeID)}
		}
		if _, exists := fg.Nodes[edge.ToNodeID]; !exists {
			return &ErrInvalidGraph{Reason: fmt.Sprintf("edge references non-existent target node: %s", edge.ToNodeID)}
		}
	}

	// Validate all nodes are reachable from entry node
	reachable := fg.findReachable(fg.EntryNodeID)
	for id := range fg.Nodes {
		if !reachable[id] {
			return &ErrInvalidGraph{Reason: fmt.Sprintf("node %s is not reachable from entry node", id)}
		}
	}

	return nil
}

// TopologicalLayers returns nodes grouped by execution layer.
// Nodes in the same layer can execute in parallel.
// Feedback edges are excluded from dependency calculation.
func (fg *FlowGraph) TopologicalLayers() ([][]string, error) {
	fg.mu.RLock()
	defer fg.mu.RUnlock()

	// Build adjacency and in-degree maps (excluding feedback edges)
	inDegree := make(map[string]int)
	adj := make(map[string][]string)
	for id := range fg.Nodes {
		inDegree[id] = 0
		adj[id] = nil
	}
	for _, edge := range fg.Edges {
		if edge.IsFeedback {
			continue // skip feedback edges for topological sort
		}
		adj[edge.FromNodeID] = append(adj[edge.FromNodeID], edge.ToNodeID)
		inDegree[edge.ToNodeID]++
	}

	// Kahn's algorithm
	var layers [][]string
	for {
		var layer []string
		for id, deg := range inDegree {
			if deg == 0 {
				layer = append(layer, id)
			}
		}
		if len(layer) == 0 {
			break
		}
		layers = append(layers, layer)
		for _, id := range layer {
			delete(inDegree, id)
			for _, next := range adj[id] {
				inDegree[next]--
			}
		}
	}

	// If there are remaining nodes, there's a non-feedback cycle
	if len(inDegree) > 0 {
		remaining := make([]string, 0, len(inDegree))
		for id := range inDegree {
			remaining = append(remaining, id)
		}
		return layers, &ErrInvalidGraph{
			Reason: fmt.Sprintf("non-feedback cycle detected involving nodes: %v", remaining),
		}
	}

	return layers, nil
}

// MarshalJSON implements JSON serialization.
func (fg *FlowGraph) MarshalJSON() ([]byte, error) {
	fg.mu.RLock()
	defer fg.mu.RUnlock()

	type Alias FlowGraph
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(fg),
	})
}

// findReachable returns a set of node IDs reachable from the given start node.
func (fg *FlowGraph) findReachable(startID string) map[string]bool {
	visited := make(map[string]bool)
	queue := []string{startID}
	visited[startID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, edge := range fg.Edges {
			if edge.FromNodeID == current && !visited[edge.ToNodeID] {
				visited[edge.ToNodeID] = true
				queue = append(queue, edge.ToNodeID)
			}
		}
	}

	return visited
}

// --- Helper functions ---

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func toFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case json.Number:
		f, _ := val.Float64()
		return f
	default:
		return 0
	}
}
