package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

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

// runState holds per-execution mutable state shared across concurrent goroutines.
type runState struct {
	mu       sync.RWMutex
	nodeRuns map[string]*NodeInstance
}

func (rs *runState) set(nodeID string, instance *NodeInstance) {
	rs.mu.Lock()
	rs.nodeRuns[nodeID] = instance
	rs.mu.Unlock()
}

func (rs *runState) snapshot() map[string]*NodeInstance {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	out := make(map[string]*NodeInstance, len(rs.nodeRuns))
	for k, v := range rs.nodeRuns {
		out[k] = v
	}
	return out
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

	// Track all node instances (thread-safe across parallel branches)
	rs := &runState{nodeRuns: make(map[string]*NodeInstance)}

	// Emit flow started event
	fo.emitEvent(ctx, "flow.started", runID, graph.EntryNodeID, map[string]any{
		"flow_id": graph.ID,
		"input":   input,
	})

	// Execute starting from the entry node
	lastOutput, err := fo.executeNode(ctx, graph, runID, graph.EntryNodeID, input, rs)

	endTime := time.Now()
	result := &FlowResult{
		RunID:     runID,
		FlowID:    graph.ID,
		NodeRuns:  rs.snapshot(),
		StartedAt: startTime,
		EndedAt:   endTime,
		Duration:  endTime.Sub(startTime),
	}

	if err != nil {
		var awaitErr *ErrAwaitingHuman
		if errors.As(err, &awaitErr) {
			// HITL state and FlowRun already persisted inside executeNode.
			result.Status = StatusWaitingFeedback
			fo.emitEvent(ctx, "flow.waiting_feedback", runID, awaitErr.NodeID, map[string]any{
				"prompt": awaitErr.Prompt,
			})
			return result, err
		}

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
// When multiple next nodes exist, they execute in parallel; their outputs are
// merged by node ID so downstream nodes can reference each branch by name.
func (fo *FlowOrchestrator) executeNode(
	ctx context.Context,
	graph *FlowGraph,
	runID, nodeID string,
	input map[string]any,
	rs *runState,
) (map[string]any, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	node, exists := graph.Nodes[nodeID]
	if !exists {
		return nil, &ErrNodeNotFound{NodeID: nodeID}
	}

	instance := NewNodeInstance(nodeID, runID)
	instance.Input = input
	instance.Status = StatusRunning
	instance.StartedAt = time.Now()
	instance.Attempts++
	rs.set(nodeID, instance)

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
		if instance.CanRetry(node) {
			slog.Warn("node execution failed, retrying",
				"node_id", nodeID,
				"attempt", instance.Attempts,
				"error", err,
			)
			return fo.executeNode(ctx, graph, runID, nodeID, input, rs)
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

	// 3b. Check for human-in-the-loop pause signal (_await_human: true in output).
	if awaitHuman, _ := result.Output["_await_human"].(bool); awaitHuman {
		prompt, _ := result.Output["_await_human_prompt"].(string)
		cleanOutput := make(map[string]any, len(result.Output))
		for k, v := range result.Output {
			if k != "_await_human" && k != "_await_human_prompt" {
				cleanOutput[k] = v
			}
		}

		instance.Status = StatusWaitingFeedback
		instance.Output = cleanOutput
		instance.CompletedAt = time.Now()
		rs.set(nodeID, instance)
		fo.stateBus.SetNodeState(ctx, runID, nodeID, instance)

		hitlState := &HITLState{
			RunID:      runID,
			FlowID:     graph.ID,
			WaitNodeID: nodeID,
			Prompt:     prompt,
			NodeOutput: cleanOutput,
			NodeInput:  input,
			NodeRuns:   rs.snapshot(),
			CreatedAt:  time.Now(),
		}
		fo.stateBus.SetHITLState(ctx, hitlState)

		waitingRun := &FlowRun{
			ID:        runID,
			FlowID:    graph.ID,
			Status:    StatusWaitingFeedback,
			Input:     input,
			StartedAt: instance.StartedAt,
		}
		fo.stateBus.SetFlowRunWaiting(ctx, waitingRun)

		fo.emitEvent(ctx, "flow.waiting_feedback", runID, nodeID, map[string]any{"prompt": prompt})
		return nil, &ErrAwaitingHuman{RunID: runID, NodeID: nodeID, Prompt: prompt}
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
	nextNodeIDs := fo.resolveNextNodes(graph, runID, nodeID, result.Output)

	if len(nextNodeIDs) == 0 {
		return result.Output, nil
	}

	// 6. Single next node — execute directly without overhead
	if len(nextNodeIDs) == 1 {
		return fo.executeNext(ctx, graph, runID, nodeID, nextNodeIDs[0], input, result.Output, rs)
	}

	// 7. Multiple next nodes — execute in parallel, merge outputs by node ID
	type branchResult struct {
		nodeID string
		output map[string]any
	}

	results := make([]branchResult, len(nextNodeIDs))
	g, gCtx := errgroup.WithContext(ctx)

	for i, nextID := range nextNodeIDs {
		i, nextID := i, nextID
		g.Go(func() error {
			out, err := fo.executeNext(gCtx, graph, runID, nodeID, nextID, input, result.Output, rs)
			if err != nil {
				return err
			}
			results[i] = branchResult{nodeID: nextID, output: out}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Merge parallel outputs: each branch keyed by its node ID
	merged := make(map[string]any, len(results))
	for _, br := range results {
		merged[br.nodeID] = br.output
	}
	return merged, nil
}

// resolveNextNodes returns the list of next node IDs after applying cycle limits.
func (fo *FlowOrchestrator) resolveNextNodes(graph *FlowGraph, runID, nodeID string, output map[string]any) []string {
	candidates := graph.GetNextNodes(nodeID, output)
	if len(candidates) == 0 {
		return nil
	}

	next := make([]string, 0, len(candidates))
	for _, toID := range candidates {
		edge := graph.GetEdge(nodeID, toID)
		if edge == nil {
			next = append(next, toID)
			continue
		}
		edgeKey := nodeID + ":" + toID
		fo.mu.Lock()
		count := fo.cycleCounters[runID][edgeKey]
		if count >= edge.MaxCycles {
			fo.mu.Unlock()
			slog.Warn("max cycles reached, skipping edge",
				"from", nodeID,
				"to", toID,
				"count", count,
			)
			continue
		}
		fo.cycleCounters[runID][edgeKey] = count + 1
		fo.mu.Unlock()
		next = append(next, toID)
	}
	return next
}

// executeNext dispatches execution to a single next node, building the correct
// input for regular vs. feedback edges.
func (fo *FlowOrchestrator) executeNext(
	ctx context.Context,
	graph *FlowGraph,
	runID, fromID, toID string,
	originalInput, nodeOutput map[string]any,
	rs *runState,
) (map[string]any, error) {
	edge := graph.GetEdge(fromID, toID)
	if edge != nil && edge.IsFeedback {
		nextInput := make(map[string]any, len(originalInput)+2)
		for k, v := range originalInput {
			nextInput[k] = v
		}
		nextInput["_feedback"] = nodeOutput
		nextInput["_feedback_from"] = fromID
		return fo.executeNode(ctx, graph, runID, toID, nextInput, rs)
	}
	return fo.executeNode(ctx, graph, runID, toID, nodeOutput, rs)
}

// Resume continues a flow run that was paused waiting for human feedback.
// It injects the feedback into the waiting node's output and resumes execution
// from the next nodes. The FlowResult will include all node runs (before + after resume).
func (fo *FlowOrchestrator) Resume(ctx context.Context, state *HITLState, graph *FlowGraph, feedback string) (*FlowResult, error) {
	// Restore graph cycle counters (fresh — simplification for first resume).
	fo.mu.Lock()
	fo.cycleCounters[state.RunID] = make(map[string]int)
	fo.mu.Unlock()
	defer func() {
		fo.mu.Lock()
		delete(fo.cycleCounters, state.RunID)
		fo.mu.Unlock()
	}()

	// Restore the accumulated node run state from before the pause.
	rs := &runState{nodeRuns: make(map[string]*NodeInstance, len(state.NodeRuns))}
	for k, v := range state.NodeRuns {
		rs.nodeRuns[k] = v
	}

	// Inject the human feedback into the waiting node's output.
	mergedOutput := make(map[string]any, len(state.NodeOutput)+1)
	for k, v := range state.NodeOutput {
		mergedOutput[k] = v
	}
	mergedOutput["_human_feedback"] = feedback

	startedAt := time.Now()
	if existing, _ := fo.stateBus.GetFlowRun(ctx, state.RunID); existing != nil {
		startedAt = existing.StartedAt
	}

	fo.emitEvent(ctx, "flow.resumed", state.RunID, state.WaitNodeID, map[string]any{
		"feedback_len": len(feedback),
	})

	nextNodeIDs := fo.resolveNextNodes(graph, state.RunID, state.WaitNodeID, mergedOutput)

	endTime := time.Now()
	var lastOutput map[string]any
	var execErr error

	switch len(nextNodeIDs) {
	case 0:
		lastOutput = mergedOutput
	case 1:
		lastOutput, execErr = fo.executeNext(ctx, graph, state.RunID, state.WaitNodeID, nextNodeIDs[0], state.NodeInput, mergedOutput, rs)
		endTime = time.Now()
	default:
		type branchResult struct {
			nodeID string
			output map[string]any
		}
		results := make([]branchResult, len(nextNodeIDs))
		g, gCtx := errgroup.WithContext(ctx)
		for i, nextID := range nextNodeIDs {
			i, nextID := i, nextID
			g.Go(func() error {
				out, err := fo.executeNext(gCtx, graph, state.RunID, state.WaitNodeID, nextID, state.NodeInput, mergedOutput, rs)
				if err != nil {
					return err
				}
				results[i] = branchResult{nodeID: nextID, output: out}
				return nil
			})
		}
		execErr = g.Wait()
		endTime = time.Now()
		if execErr == nil {
			merged := make(map[string]any, len(results))
			for _, br := range results {
				merged[br.nodeID] = br.output
			}
			lastOutput = merged
		}
	}

	result := &FlowResult{
		RunID:     state.RunID,
		FlowID:    graph.ID,
		NodeRuns:  rs.snapshot(),
		StartedAt: startedAt,
		EndedAt:   endTime,
		Duration:  endTime.Sub(startedAt),
	}

	if execErr != nil {
		var awaitErr *ErrAwaitingHuman
		if errors.As(execErr, &awaitErr) {
			// Another node triggered HITL — state already persisted by executeNode.
			result.Status = StatusWaitingFeedback
			return result, execErr
		}

		result.Status = StatusFailed
		result.Error = execErr.Error()
		fo.stateBus.SetFlowRun(ctx, &FlowRun{
			ID:        state.RunID,
			FlowID:    graph.ID,
			Status:    StatusFailed,
			Error:     execErr.Error(),
			StartedAt: startedAt,
			EndedAt:   endTime,
		})
		fo.stateBus.DeleteHITLState(ctx, state.RunID)
	} else {
		result.Status = StatusSuccess
		result.Output = lastOutput
		fo.stateBus.SetFlowRun(ctx, &FlowRun{
			ID:        state.RunID,
			FlowID:    graph.ID,
			Status:    StatusSuccess,
			Output:    lastOutput,
			StartedAt: startedAt,
			EndedAt:   endTime,
		})
		fo.stateBus.DeleteHITLState(ctx, state.RunID)
	}

	fo.emitEvent(ctx, "flow.completed", state.RunID, "", map[string]any{
		"status":   result.Status,
		"duration": result.Duration.String(),
	})

	return result, execErr
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
