package adi

import (
	"encoding/json"
	"time"
)

// --- Node API Models ---

// CreateNodeRequest is the request body for creating a node.
type CreateNodeRequest struct {
	Name          string          `json:"name"`
	Purpose       string          `json:"purpose"`
	InputSchema   json.RawMessage `json:"input_schema,omitempty"`
	OutputSchema  json.RawMessage `json:"output_schema,omitempty"`
	Tools         json.RawMessage `json:"tools,omitempty"`
	ScriptLang    string          `json:"script_lang"`
	ScriptContent string          `json:"script_content"`
	MaxRetries    int             `json:"max_retries,omitempty"`
	TimeoutSec    int             `json:"timeout_sec,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
}

// UpdateNodeRequest is the request body for updating a node.
type UpdateNodeRequest struct {
	Name          *string          `json:"name,omitempty"`
	Purpose       *string          `json:"purpose,omitempty"`
	InputSchema   *json.RawMessage `json:"input_schema,omitempty"`
	OutputSchema  *json.RawMessage `json:"output_schema,omitempty"`
	Tools         *json.RawMessage `json:"tools,omitempty"`
	ScriptLang    *string          `json:"script_lang,omitempty"`
	ScriptContent *string          `json:"script_content,omitempty"`
	MaxRetries    *int             `json:"max_retries,omitempty"`
	TimeoutSec    *int             `json:"timeout_sec,omitempty"`
	Metadata      *json.RawMessage `json:"metadata,omitempty"`
}

// NodeResponse is the response for a single node.
type NodeResponse struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Purpose       string          `json:"purpose"`
	InputSchema   json.RawMessage `json:"input_schema"`
	OutputSchema  json.RawMessage `json:"output_schema"`
	Tools         json.RawMessage `json:"tools"`
	ScriptLang    string          `json:"script_lang"`
	ScriptContent string          `json:"script_content"`
	MaxRetries    int             `json:"max_retries"`
	TimeoutSec    int             `json:"timeout_sec"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// --- Flow API Models ---

// CreateFlowRequest is the request body for creating a flow.
type CreateFlowRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	EntryNodeID string          `json:"entry_node_id"`
	NodeIDs     []string        `json:"node_ids"`
	Edges       json.RawMessage `json:"edges"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

// ExecuteFlowRequest is the request body for executing a flow.
type ExecuteFlowRequest struct {
	Input map[string]any `json:"input"`
}

// FlowResponse is the response for a single flow.
type FlowResponse struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	EntryNodeID string          `json:"entry_node_id"`
	NodeIDs     []string        `json:"node_ids"`
	Edges       json.RawMessage `json:"edges"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// --- Knowledge API Models ---

// IngestKnowledgeRequest is the request body for ingesting knowledge.
type IngestKnowledgeRequest struct {
	Title       string         `json:"title"`
	Content     string         `json:"content"`
	ContentType string         `json:"content_type,omitempty"` // "document", "rule", "manual"
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// SearchKnowledgeRequest is the request body for searching knowledge.
type SearchKnowledgeRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k,omitempty"`
}

// KnowledgeResult is a single knowledge search result.
type KnowledgeResult struct {
	ID          string         `json:"id"`
	Title       string         `json:"title,omitempty"`
	ContentText string         `json:"content_text"`
	Score       float64        `json:"score"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// --- Test API Models ---

// RunTestRequest is the request body for running a test in sandbox.
type RunTestRequest struct {
	Script   string         `json:"script"`
	Language string         `json:"language"`
	Input    map[string]any `json:"input,omitempty"`
}

// TestResult is the response for a test execution.
type TestResult struct {
	ID       string         `json:"id"`
	Status   string         `json:"status"`
	Output   map[string]any `json:"output,omitempty"`
	Logs     []string       `json:"logs,omitempty"`
	Error    string         `json:"error,omitempty"`
	Duration string         `json:"duration"`
}

// --- Tools API Models ---

// ToolListResponse is the response for GET /api/v1/tools.
type ToolListResponse struct {
	Tools []toolEntry `json:"tools"`
	Total int         `json:"total"`
}

type toolEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// --- Schedule API Models ---

// ScheduleFlowRequest is the body for POST /api/v1/flows/{flowID}/schedule.
type ScheduleFlowRequest struct {
	Input       map[string]any `json:"input"`
	ScheduledAt time.Time      `json:"scheduled_at"`
}

// ScheduleResponse is returned after creating or retrieving a schedule.
type ScheduleResponse struct {
	ScheduleID  string         `json:"schedule_id"`
	FlowID      string         `json:"flow_id"`
	Input       map[string]any `json:"input,omitempty"`
	ScheduledAt time.Time      `json:"scheduled_at"`
	Status      string         `json:"status"`
	RunID       string         `json:"run_id,omitempty"`
	Error       string         `json:"error,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

// --- Run & HITL API Models ---

// RunStatusResponse is the response for GET /api/v1/runs/{runID}.
type RunStatusResponse struct {
	RunID     string         `json:"run_id"`
	FlowID    string         `json:"flow_id"`
	Status    string         `json:"status"`
	Output    map[string]any `json:"output,omitempty"`
	Error     string         `json:"error,omitempty"`
	StartedAt time.Time      `json:"started_at"`
	EndedAt   *time.Time     `json:"ended_at,omitempty"`
	HITL      *HITLInfo      `json:"hitl,omitempty"`
}

// HITLInfo surfaces the waiting node and human prompt in RunStatusResponse.
type HITLInfo struct {
	NodeID string `json:"node_id"`
	Prompt string `json:"prompt"`
}

// FeedbackRequest is the body for POST /api/v1/runs/{runID}/feedback.
type FeedbackRequest struct {
	Feedback string `json:"feedback"`
}
