package core

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/acassiovilasboas/genoma/internal/shared"
)

// SandboxExecutorInterface defines the contract for script execution in sandbox.
type SandboxExecutorInterface interface {
	Execute(ctx context.Context, req ExecutionRequest) (*ExecutionResult, error)
}

// ExecutionRequest represents a script execution request for the sandbox.
type ExecutionRequest struct {
	Script   string         `json:"script"`
	Language ScriptLanguage `json:"language"`
	Input    map[string]any `json:"input,omitempty"`
	Limits   *ResourceLimits `json:"limits,omitempty"`
}

// ExecutionResult represents the result of a sandbox execution.
type ExecutionResult struct {
	Output   map[string]any `json:"output,omitempty"`
	Logs     []string       `json:"logs,omitempty"`
	ExitCode int            `json:"exit_code"`
	Duration time.Duration  `json:"duration"`
	Error    string         `json:"error,omitempty"`
}

// ResourceLimits defines constraints for sandbox execution.
type ResourceLimits struct {
	CPUQuota        int64 `json:"cpu_quota"`
	MemoryBytes     int64 `json:"memory_bytes"`
	NetworkDisabled bool  `json:"network_disabled"`
	TimeoutSec      int   `json:"timeout_sec"`
	MaxOutputBytes  int64 `json:"max_output_bytes"`
	ReadOnlyRootfs  bool  `json:"read_only_rootfs"`
	PidsLimit       int64 `json:"pids_limit"`
}

// FlowResult holds the final result of a flow execution.
type FlowResult struct {
	RunID     string                    `json:"run_id"`
	FlowID    string                    `json:"flow_id"`
	Status    NodeStatus                `json:"status"`
	Output    map[string]any            `json:"output,omitempty"`
	NodeRuns  map[string]*NodeInstance   `json:"node_runs"`
	Error     string                    `json:"error,omitempty"`
	StartedAt time.Time                `json:"started_at"`
	EndedAt   time.Time                `json:"ended_at"`
	Duration  time.Duration            `json:"duration"`
}

// FlowOrchestrator executes flow graphs by traversing nodes,
// managing state, and coordinating sandbox execution.
type FlowOrchestrator struct {
	sandbox   SandboxExecutorInterface
	stateBus  *StateBus
	validator *ContractValidator
	tools     *ToolRegistry
	events    *shared.EventBus

	// cycleCounters tracks edge traversal counts per flow run to prevent infinite loops.
	cycleCounters map[string]map[string]int // runID -> "from:to" -> count
	mu            sync.Mutex
}

// NewFlowOrchestrator creates a new flow orchestrator.
func NewFlowOrchestrator(
	sandbox SandboxExecutorInterface,
	stateBus *StateBus,
	validator *ContractValidator,
	tools *ToolRegistry,
	events *shared.EventBus,
) *FlowOrchestrator {
	return &FlowOrchestrator{
		sandbox:       sandbox,
		stateBus:      stateBus,
		validator:     validator,
		tools:         tools,
		events:        events,
		cycleCounters: make(map[string]map[string]int),
	}
}

// Execute runs a flow graph from its entry node to completion.
func (fo *FlowOrchestrator) Execute(ctx context.Context, graph *FlowGraph, input map[string]any) (*FlowResult, error) {
	// Validate graph before execution
	if err := graph.Validate(); err != nil {
		return nil, fmt.Errorf("graph validation failed: %w", err)
	}

	runID := shared.NewID()
	startTime := time.Now()

	// Initialize flow run state
	flowRun := &FlowRun{
		ID:        runID,
		FlowID:    graph.ID,
		Status:    StatusRunning,
		Input:     input,
		StartedAt: startTime,
	}
	if err := fo.stateBus.SetFlowRun(ctx, flowRun); err != nil {
		return nil, fmt.Errorf("save flow run state: %w", err)
	}

	// Initialize cycle counters for this run
	fo.mu.Lock()
	fo.cycleCounters[runID] = make(map[string]int)
	fo.mu.Unlock()
	defer func() {
		fo.mu.Lock()
		delete(fo.cycleCounters, runID)
		fo.mu.Unlock()
	}()

	// Track all node instances
	nodeRuns := make(map[string]*NodeInstance)

	// Emit flow started event
	fo.emitEvent(ctx, "flow.started", runID, graph.EntryNodeID, map[string]any{
		"flow_id": graph.ID,
		"input":   input,
	})

	// Execute starting from the entry node
	lastOutput, err := fo.executeNode(ctx, graph, runID, graph.EntryNodeID, input, nodeRuns)

	endTime := time.Now()
	result := &FlowResult{
		RunID:     runID,
		FlowID:    graph.ID,
		NodeRuns:  nodeRuns,
		StartedAt: startTime,
		EndedAt:   endTime,
		Duration:  endTime.Sub(startTime),
	}

	if err != nil {
		result.Status = StatusFailed
		result.Error = err.Error()

		flowRun.Status = StatusFailed
		flowRun.Error = err.Error()
		flowRun.EndedAt = endTime
	} else {
		result.Status = StatusSuccess
		result.Output = lastOutput

		flowRun.Status = StatusSuccess
		flowRun.Output = lastOutput
		flowRun.EndedAt = endTime
	}

	// Persist final state
	fo.stateBus.SetFlowRun(ctx, flowRun)

	// Emit flow completed event
	fo.emitEvent(ctx, "flow.completed", runID, "", map[string]any{
		"status":   result.Status,
		"duration": result.Duration.String(),
	})

	return result, nil
}

// executeNode runs a single node and recursively follows edges.
func (fo *FlowOrchestrator) executeNode(
	ctx context.Context,
	graph *FlowGraph,
	runID, nodeID string,
	input map[string]any,
	nodeRuns map[string]*NodeInstance,
) (map[string]any, error) {
	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	node, exists := graph.Nodes[nodeID]
	if !exists {
		return nil, &ErrNodeNotFound{NodeID: nodeID}
	}

	// Create or get node instance
	instance := NewNodeInstance(nodeID, runID)
	instance.Input = input
	instance.Status = StatusRunning
	instance.StartedAt = time.Now()
	instance.Attempts++
	nodeRuns[nodeID] = instance

	// Persist state
	fo.stateBus.SetNodeState(ctx, runID, nodeID, instance)
	fo.emitEvent(ctx, "node.started", runID, nodeID, nil)

	slog.Info("executing node",
		"run_id", runID,
		"node_id", nodeID,
		"node_name", node.Name,
		"attempt", instance.Attempts,
	)

	// 1. Validate input contract
	if err := fo.validator.ValidateInput(node, input); err != nil {
		instance.Status = StatusFailed
		instance.Error = err.Error()
		instance.CompletedAt = time.Now()
		fo.stateBus.SetNodeState(ctx, runID, nodeID, instance)
		return nil, err
	}

	// 2. Execute script in sandbox
	timeout := node.TimeoutSec
	if timeout <= 0 {
		timeout = 30
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	result, err := fo.sandbox.Execute(execCtx, ExecutionRequest{
		Script:   node.ScriptContent,
		Language: node.ScriptLang,
		Input:    input,
	})
	if err != nil {
		// Check if we can retry
		if instance.CanRetry(node) {
			slog.Warn("node execution failed, retrying",
				"node_id", nodeID,
				"attempt", instance.Attempts,
				"error", err,
			)
			return fo.executeNode(ctx, graph, runID, nodeID, input, nodeRuns)
		}

		instance.Status = StatusFailed
		instance.Error = err.Error()
		instance.CompletedAt = time.Now()
		fo.stateBus.SetNodeState(ctx, runID, nodeID, instance)
		fo.emitEvent(ctx, "node.failed", runID, nodeID, map[string]any{"error": err.Error()})
		return nil, &ErrFlowExecution{
			FlowID:  graph.ID,
			NodeID:  nodeID,
			Message: "sandbox execution failed",
			Cause:   err,
		}
	}

	// 3. Validate output contract
	if err := fo.validator.ValidateOutput(node, result.Output); err != nil {
		instance.Status = StatusFailed
		instance.Error = err.Error()
		instance.CompletedAt = time.Now()
		fo.stateBus.SetNodeState(ctx, runID, nodeID, instance)
		return nil, err
	}

	// 4. Mark node as successful
	instance.Status = StatusSuccess
	instance.Output = result.Output
	instance.CompletedAt = time.Now()
	fo.stateBus.SetNodeState(ctx, runID, nodeID, instance)
	fo.emitEvent(ctx, "node.completed", runID, nodeID, result.Output)

	slog.Info("node completed",
		"run_id", runID,
		"node_id", nodeID,
		"duration", instance.CompletedAt.Sub(instance.StartedAt),
	)

	// 5. Resolve next nodes based on output and edge conditions
	nextNodeIDs := graph.GetNextNodes(nodeID, result.Output)

	if len(nextNodeIDs) == 0 {
		// Terminal node — return its output
		return result.Output, nil
	}

	// 6. Execute next nodes
	var lastOutput map[string]any

	for _, nextID := range nextNodeIDs {
		// Check cycle limits
		edge := graph.GetEdge(nodeID, nextID)
		if edge != nil {
			edgeKey := nodeID + ":" + nextID
			fo.mu.Lock()
			count := fo.cycleCounters[runID][edgeKey]
			if count >= edge.MaxCycles {
				fo.mu.Unlock()
				slog.Warn("max cycles reached, skipping edge",
					"from", nodeID,
					"to", nextID,
					"count", count,
				)
				continue
			}
			fo.cycleCounters[runID][edgeKey] = count + 1
			fo.mu.Unlock()

			// For feedback edges, pass the output along with feedback context
			if edge.IsFeedback {
				nextInput := make(map[string]any)
				for k, v := range input {
					nextInput[k] = v
				}
				nextInput["_feedback"] = result.Output
				nextInput["_feedback_from"] = nodeID
				lastOutput, err = fo.executeNode(ctx, graph, runID, nextID, nextInput, nodeRuns)
			} else {
				lastOutput, err = fo.executeNode(ctx, graph, runID, nextID, result.Output, nodeRuns)
			}
		} else {
			lastOutput, err = fo.executeNode(ctx, graph, runID, nextID, result.Output, nodeRuns)
		}

		if err != nil {
			return nil, err
		}
	}

	return lastOutput, nil
}

// emitEvent publishes an event to the event bus.
func (fo *FlowOrchestrator) emitEvent(ctx context.Context, eventType, runID, nodeID string, data map[string]any) {
	if fo.events == nil {
		return
	}
	fo.events.Publish(ctx, "orchestrator", shared.Event{
		Type:      eventType,
		FlowRunID: runID,
		NodeID:    nodeID,
		Data:      data,
		Timestamp: time.Now().UnixMilli(),
	})
}
