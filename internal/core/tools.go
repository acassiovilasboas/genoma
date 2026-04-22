package core

import (
	"context"
	"fmt"
	"sync"
)

// ToolHandler defines the interface for a tool that can be invoked by nodes.
type ToolHandler interface {
	// Name returns the unique name of the tool.
	Name() string

	// Description returns a human-readable description.
	Description() string

	// Execute runs the tool with the given parameters.
	Execute(ctx context.Context, params map[string]any) (map[string]any, error)
}

// ToolRegistry manages available tools and their permissions per node.
type ToolRegistry struct {
	tools map[string]ToolHandler
	mu    sync.RWMutex
}

// NewToolRegistry creates a new tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]ToolHandler),
	}
}

// Register adds a tool to the registry.
func (tr *ToolRegistry) Register(tool ToolHandler) error {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	name := tool.Name()
	if _, exists := tr.tools[name]; exists {
		return fmt.Errorf("tool already registered: %s", name)
	}

	tr.tools[name] = tool
	return nil
}

// Get returns a tool by name.
func (tr *ToolRegistry) Get(name string) (ToolHandler, bool) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	tool, ok := tr.tools[name]
	return tool, ok
}

// List returns all registered tool names.
func (tr *ToolRegistry) List() []string {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	names := make([]string, 0, len(tr.tools))
	for name := range tr.tools {
		names = append(names, name)
	}
	return names
}

// ToolInfo holds the public metadata of a registered tool.
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ListInfo returns name and description for every registered tool.
func (tr *ToolRegistry) ListInfo() []ToolInfo {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	infos := make([]ToolInfo, 0, len(tr.tools))
	for _, tool := range tr.tools {
		infos = append(infos, ToolInfo{
			Name:        tool.Name(),
			Description: tool.Description(),
		})
	}
	return infos
}

// ValidateNodeTools checks if all tools referenced by a node are registered.
func (tr *ToolRegistry) ValidateNodeTools(node *NodeDefinition) error {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	for _, ref := range node.Tools {
		if _, exists := tr.tools[ref.Name]; !exists {
			return fmt.Errorf("node %s references unregistered tool: %s", node.ID, ref.Name)
		}
	}
	return nil
}

// ExecuteForNode runs a tool, checking that the node has permission to use it.
func (tr *ToolRegistry) ExecuteForNode(ctx context.Context, node *NodeDefinition, toolName string, params map[string]any) (map[string]any, error) {
	// Check permission
	allowed := false
	for _, ref := range node.Tools {
		if ref.Name == toolName {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("node %s is not authorized to use tool %s", node.ID, toolName)
	}

	tool, ok := tr.Get(toolName)
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", toolName)
	}

	return tool.Execute(ctx, params)
}

// ====================================================================
// Built-in Tools
// ====================================================================

// LogTool writes a log message.
type LogTool struct{}

func (t *LogTool) Name() string        { return "log" }
func (t *LogTool) Description() string { return "Write a log message" }

func (t *LogTool) Execute(_ context.Context, params map[string]any) (map[string]any, error) {
	msg, _ := params["message"].(string)
	level, _ := params["level"].(string)
	if level == "" {
		level = "info"
	}
	return map[string]any{
		"logged":  true,
		"message": msg,
		"level":   level,
	}, nil
}

// HTTPRequestTool makes an HTTP request (to be executed inside sandbox context).
type HTTPRequestTool struct{}

func (t *HTTPRequestTool) Name() string        { return "http_request" }
func (t *HTTPRequestTool) Description() string { return "Make an HTTP request" }

func (t *HTTPRequestTool) Execute(_ context.Context, params map[string]any) (map[string]any, error) {
	// This tool is a declaration for the sandbox — actual execution
	// happens inside the sandboxed container. The core just validates
	// the permission here.
	return map[string]any{
		"tool":   "http_request",
		"params": params,
	}, nil
}

// RegisterBuiltinTools registers all built-in tools in the registry.
func RegisterBuiltinTools(registry *ToolRegistry) {
	registry.Register(&LogTool{})
	registry.Register(&HTTPRequestTool{})
}
