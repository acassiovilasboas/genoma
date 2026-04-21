package contracts

import (
	"encoding/json"
	"time"
)

// NodeDefinition is the public type for external integrations.
type NodeDefinition struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Purpose       string          `json:"purpose"`
	InputSchema   json.RawMessage `json:"input_schema"`
	OutputSchema  json.RawMessage `json:"output_schema"`
	ScriptLang    string          `json:"script_lang"`
	ScriptContent string          `json:"script_content"`
	MaxRetries    int             `json:"max_retries"`
	TimeoutSec    int             `json:"timeout_sec"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// FlowGraph is the public type for flow definitions.
type FlowGraph struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	EntryNodeID string          `json:"entry_node_id"`
	NodeIDs     []string        `json:"node_ids"`
	Edges       json.RawMessage `json:"edges"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// FlowResult is the public type for flow execution results.
type FlowResult struct {
	RunID    string         `json:"run_id"`
	FlowID   string         `json:"flow_id"`
	Status   string         `json:"status"`
	Output   map[string]any `json:"output,omitempty"`
	Error    string         `json:"error,omitempty"`
	Duration string         `json:"duration"`
}

// Entity is the public type for hybrid-persisted entities.
type Entity struct {
	ID         string         `json:"id"`
	EntityType string         `json:"entity_type"`
	Data       map[string]any `json:"data"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}
