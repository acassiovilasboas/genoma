package core

import (
	"encoding/json"
	"time"
)

// ScriptLanguage defines supported script execution languages.
type ScriptLanguage string

const (
	LangPython ScriptLanguage = "python"
	LangNodeJS ScriptLanguage = "nodejs"
)

// NodeStatus represents the execution state of a node instance.
type NodeStatus string

const (
	StatusPending         NodeStatus = "PENDING"
	StatusRunning         NodeStatus = "RUNNING"
	StatusSuccess         NodeStatus = "SUCCESS"
	StatusFailed          NodeStatus = "FAILED"
	StatusWaitingFeedback NodeStatus = "WAITING_FEEDBACK"
	StatusSkipped         NodeStatus = "SKIPPED"
)

// NodeDefinition represents the static blueprint of a node in the graph.
// Each node is an isolated processing unit with a defined purpose, contract,
// and execution script.
type NodeDefinition struct {
	// ID is the unique identifier for this node (ULID).
	ID string `json:"id"`

	// Name is the human-readable name of this node.
	Name string `json:"name"`

	// Purpose describes what this node does. Used by the semantic router
	// to match user intents to the correct flow.
	Purpose string `json:"purpose"`

	// InputSchema is the JSON Schema that validates input data.
	InputSchema json.RawMessage `json:"input_schema"`

	// OutputSchema is the JSON Schema that validates output data.
	OutputSchema json.RawMessage `json:"output_schema"`

	// Tools lists the tools this node is allowed to use.
	Tools []ToolRef `json:"tools"`

	// ScriptLang defines the language of the execution script.
	ScriptLang ScriptLanguage `json:"script_lang"`

	// ScriptContent holds the script source code to execute in the sandbox.
	ScriptContent string `json:"script_content"`

	// MaxRetries is the maximum number of retry attempts on failure.
	MaxRetries int `json:"max_retries"`

	// TimeoutSec is the execution timeout in seconds.
	TimeoutSec int `json:"timeout_sec"`

	// Metadata holds arbitrary key-value pairs.
	Metadata map[string]any `json:"metadata,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ToolRef references a tool available to a node.
type ToolRef struct {
	Name   string         `json:"name"`
	Config map[string]any `json:"config,omitempty"`
}

// NodeInstance represents the runtime state of a node during flow execution.
type NodeInstance struct {
	// NodeID references the NodeDefinition being executed.
	NodeID string `json:"node_id"`

	// FlowRunID is the execution run this instance belongs to.
	FlowRunID string `json:"flow_run_id"`

	// Status is the current execution state.
	Status NodeStatus `json:"status"`

	// Input is the data passed to this node.
	Input map[string]any `json:"input,omitempty"`

	// Output is the data produced by this node.
	Output map[string]any `json:"output,omitempty"`

	// Error holds the error message if the node failed.
	Error string `json:"error,omitempty"`

	// Attempts tracks how many times this node has been executed.
	Attempts int `json:"attempts"`

	// Feedback holds feedback from a critic node for retry loops.
	Feedback string `json:"feedback,omitempty"`

	StartedAt   time.Time `json:"started_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

// IsTerminal returns true if the node has reached a final state.
func (ni *NodeInstance) IsTerminal() bool {
	return ni.Status == StatusSuccess || ni.Status == StatusFailed || ni.Status == StatusSkipped
}

// CanRetry returns true if the node can be retried based on the definition.
func (ni *NodeInstance) CanRetry(def *NodeDefinition) bool {
	return ni.Status == StatusFailed && ni.Attempts < def.MaxRetries
}

// NewNodeInstance creates a new node instance in PENDING state.
func NewNodeInstance(nodeID, flowRunID string) *NodeInstance {
	return &NodeInstance{
		NodeID:    nodeID,
		FlowRunID: flowRunID,
		Status:    StatusPending,
		Attempts:  0,
	}
}
