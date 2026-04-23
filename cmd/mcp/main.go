package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// MCP JSON-RPC types

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolDef struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema toolSchema `json:"inputSchema"`
}

type toolSchema struct {
	Type       string              `json:"type"`
	Properties map[string]schemaProp `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type schemaProp struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// server holds the HTTP client and Genoma API base URL.
type server struct {
	apiURL string
	apiKey string
	client *http.Client
	enc    *json.Encoder
}

func newServer() *server {
	apiURL := strings.TrimRight(os.Getenv("GENOMA_API_URL"), "/")
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}
	return &server{
		apiURL: apiURL,
		apiKey: os.Getenv("GENOMA_API_KEY"),
		client: &http.Client{Timeout: 60 * time.Second},
		enc:    json.NewEncoder(os.Stdout),
	}
}

func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(0)

	s := newServer()
	log.Printf("genoma-mcp starting (api=%s)", s.apiURL)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeError(nil, -32700, "parse error")
			continue
		}

		s.handle(req)
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		log.Printf("stdin error: %v", err)
	}
}

func (s *server) handle(req rpcRequest) {
	switch req.Method {
	case "initialize":
		s.writeResult(req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "genoma-mcp", "version": "0.1.0"},
		})
	case "notifications/initialized":
		// no response needed for notifications

	case "tools/list":
		s.writeResult(req.ID, map[string]any{"tools": tools()})

	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			s.writeError(req.ID, -32602, "invalid params")
			return
		}
		s.callTool(req.ID, p.Name, p.Arguments)

	default:
		s.writeError(req.ID, -32601, "method not found: "+req.Method)
	}
}

func (s *server) callTool(id any, name string, args map[string]any) {
	ctx := context.Background()
	var (
		result any
		err    error
	)

	switch name {
	case "genoma_list_flows":
		result, err = s.get(ctx, "/api/v1/flows")
	case "genoma_get_flow":
		result, err = s.get(ctx, "/api/v1/flows/"+str(args["flow_id"]))
	case "genoma_create_flow":
		result, err = s.post(ctx, "/api/v1/flows", args)
	case "genoma_execute_flow":
		flowID := str(args["flow_id"])
		input, _ := args["input"].(map[string]any)
		result, err = s.post(ctx, "/api/v1/flows/"+flowID+"/execute", map[string]any{"input": input})
	case "genoma_list_nodes":
		result, err = s.get(ctx, "/api/v1/nodes")
	case "genoma_get_node":
		result, err = s.get(ctx, "/api/v1/nodes/"+str(args["node_id"]))
	case "genoma_create_node":
		result, err = s.post(ctx, "/api/v1/nodes", args)
	case "genoma_get_run":
		result, err = s.get(ctx, "/api/v1/runs/"+str(args["run_id"]))
	case "genoma_submit_feedback":
		runID := str(args["run_id"])
		result, err = s.post(ctx, "/api/v1/runs/"+runID+"/feedback", map[string]any{"feedback": str(args["feedback"])})
	case "genoma_chat":
		result, err = s.post(ctx, "/api/v1/chat/message", map[string]any{
			"session_id": str(args["session_id"]),
			"message":    str(args["message"]),
		})
	case "genoma_ingest_knowledge":
		result, err = s.post(ctx, "/api/v1/knowledge/ingest", args)
	case "genoma_search_knowledge":
		result, err = s.post(ctx, "/api/v1/knowledge/search", args)
	case "genoma_list_tools":
		result, err = s.get(ctx, "/api/v1/tools")
	case "genoma_list_schedules":
		result, err = s.get(ctx, "/api/v1/schedules")
	default:
		s.writeError(id, -32602, "unknown tool: "+name)
		return
	}

	if err != nil {
		s.writeResult(id, toolError(err.Error()))
		return
	}

	body, _ := json.Marshal(result)
	s.writeResult(id, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(body)},
		},
	})
}

// --- HTTP helpers ---

func (s *server) get(ctx context.Context, path string) (any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiURL+path, nil)
	if err != nil {
		return nil, err
	}
	return s.do(req)
}

func (s *server) post(ctx context.Context, path string, body any) (any, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return s.do(req)
}

func (s *server) do(req *http.Request) (any, error) {
	if s.apiKey != "" {
		req.Header.Set("X-API-Key", s.apiKey)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if resp.StatusCode >= 400 {
		if m, ok := result.(map[string]any); ok {
			if e, ok := m["error"].(string); ok {
				return nil, fmt.Errorf("%s", e)
			}
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return result, nil
}

// --- Response helpers ---

func (s *server) writeResult(id any, result any) {
	s.enc.Encode(rpcResponse{JSONRPC: "2.0", ID: id, Result: result}) //nolint:errcheck
}

func (s *server) writeError(id any, code int, msg string) {
	s.enc.Encode(rpcResponse{ //nolint:errcheck
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	})
}

func toolError(msg string) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": fmt.Sprintf(`{"error": "%s"}`, msg)},
		},
		"isError": true,
	}
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// tools returns the MCP tool definitions exposed by this server.
func tools() []toolDef {
	return []toolDef{
		{
			Name:        "genoma_list_flows",
			Description: "List all registered flows in Genoma.",
			InputSchema: toolSchema{Type: "object"},
		},
		{
			Name:        "genoma_get_flow",
			Description: "Get details of a specific flow by ID.",
			InputSchema: toolSchema{
				Type:     "object",
				Required: []string{"flow_id"},
				Properties: map[string]schemaProp{
					"flow_id": {Type: "string", Description: "Flow ID"},
				},
			},
		},
		{
			Name:        "genoma_create_flow",
			Description: "Create a new flow in Genoma. A flow is a DAG of nodes.",
			InputSchema: toolSchema{
				Type:     "object",
				Required: []string{"name", "description", "entry_node_id", "node_ids"},
				Properties: map[string]schemaProp{
					"name":          {Type: "string", Description: "Flow name"},
					"description":   {Type: "string", Description: "Human-readable description used for semantic routing"},
					"entry_node_id": {Type: "string", Description: "ID of the first node to execute"},
					"node_ids":      {Type: "array", Description: "Ordered list of node IDs in this flow"},
				},
			},
		},
		{
			Name:        "genoma_execute_flow",
			Description: "Execute a flow by ID with a JSON input payload. Returns the flow result or a run_id if waiting for human feedback.",
			InputSchema: toolSchema{
				Type:     "object",
				Required: []string{"flow_id"},
				Properties: map[string]schemaProp{
					"flow_id": {Type: "string", Description: "Flow ID to execute"},
					"input":   {Type: "object", Description: "JSON input for the flow"},
				},
			},
		},
		{
			Name:        "genoma_list_nodes",
			Description: "List all node definitions registered in Genoma.",
			InputSchema: toolSchema{Type: "object"},
		},
		{
			Name:        "genoma_get_node",
			Description: "Get details of a specific node by ID.",
			InputSchema: toolSchema{
				Type:     "object",
				Required: []string{"node_id"},
				Properties: map[string]schemaProp{
					"node_id": {Type: "string", Description: "Node ID"},
				},
			},
		},
		{
			Name:        "genoma_create_node",
			Description: "Create a new node definition with a script (Python or NodeJS).",
			InputSchema: toolSchema{
				Type:     "object",
				Required: []string{"name", "purpose", "script_lang", "script_content"},
				Properties: map[string]schemaProp{
					"name":           {Type: "string", Description: "Node name"},
					"purpose":        {Type: "string", Description: "What this node does"},
					"script_lang":    {Type: "string", Description: "Script language: 'python' or 'nodejs'"},
					"script_content": {Type: "string", Description: "Script source code"},
					"timeout_sec":    {Type: "integer", Description: "Execution timeout in seconds (default 30)"},
					"max_retries":    {Type: "integer", Description: "Max retry attempts on failure (default 3)"},
				},
			},
		},
		{
			Name:        "genoma_get_run",
			Description: "Get the status and output of a flow run. Use this to poll for completion or check for HITL prompts.",
			InputSchema: toolSchema{
				Type:     "object",
				Required: []string{"run_id"},
				Properties: map[string]schemaProp{
					"run_id": {Type: "string", Description: "Run ID returned by execute_flow"},
				},
			},
		},
		{
			Name:        "genoma_submit_feedback",
			Description: "Submit human feedback to unblock a flow waiting for human-in-the-loop input.",
			InputSchema: toolSchema{
				Type:     "object",
				Required: []string{"run_id", "feedback"},
				Properties: map[string]schemaProp{
					"run_id":   {Type: "string", Description: "Run ID that is WAITING_FEEDBACK"},
					"feedback": {Type: "string", Description: "Human feedback text to inject into the flow"},
				},
			},
		},
		{
			Name:        "genoma_chat",
			Description: "Send a natural-language message. Genoma will route it to the best matching flow and return a reply.",
			InputSchema: toolSchema{
				Type:     "object",
				Required: []string{"message"},
				Properties: map[string]schemaProp{
					"message":    {Type: "string", Description: "User message"},
					"session_id": {Type: "string", Description: "Session ID for conversation continuity (optional)"},
				},
			},
		},
		{
			Name:        "genoma_ingest_knowledge",
			Description: "Ingest a document into Genoma's semantic knowledge base.",
			InputSchema: toolSchema{
				Type:     "object",
				Required: []string{"content"},
				Properties: map[string]schemaProp{
					"title":        {Type: "string", Description: "Document title"},
					"content":      {Type: "string", Description: "Document text content"},
					"content_type": {Type: "string", Description: "Type: 'knowledge', 'rule', 'manual'"},
				},
			},
		},
		{
			Name:        "genoma_search_knowledge",
			Description: "Semantic search over Genoma's knowledge base using vector similarity.",
			InputSchema: toolSchema{
				Type:     "object",
				Required: []string{"query"},
				Properties: map[string]schemaProp{
					"query":  {Type: "string", Description: "Natural language search query"},
					"top_k":  {Type: "integer", Description: "Number of results to return (default 10)"},
				},
			},
		},
		{
			Name:        "genoma_list_tools",
			Description: "List built-in tools available to nodes in Genoma.",
			InputSchema: toolSchema{Type: "object"},
		},
		{
			Name:        "genoma_list_schedules",
			Description: "List all scheduled flow executions.",
			InputSchema: toolSchema{Type: "object"},
		},
	}
}
