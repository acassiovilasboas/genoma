package core

import "fmt"

// --- Node Errors ---

// ErrNodeNotFound indicates a node was not found in the graph.
type ErrNodeNotFound struct {
	NodeID string
}

func (e *ErrNodeNotFound) Error() string {
	return fmt.Sprintf("node not found: %s", e.NodeID)
}

// ErrNodeAlreadyExists indicates a node with the same ID already exists.
type ErrNodeAlreadyExists struct {
	NodeID string
}

func (e *ErrNodeAlreadyExists) Error() string {
	return fmt.Sprintf("node already exists: %s", e.NodeID)
}

// --- Graph Errors ---

// ErrInvalidGraph indicates the graph has structural problems.
type ErrInvalidGraph struct {
	Reason string
}

func (e *ErrInvalidGraph) Error() string {
	return fmt.Sprintf("invalid graph: %s", e.Reason)
}

// ErrMaxCyclesExceeded indicates a cycle has been executed too many times.
type ErrMaxCyclesExceeded struct {
	EdgeFrom string
	EdgeTo   string
	Count    int
}

func (e *ErrMaxCyclesExceeded) Error() string {
	return fmt.Sprintf("max cycles exceeded on edge %s→%s (count: %d)", e.EdgeFrom, e.EdgeTo, e.Count)
}

// --- Contract Errors ---

// ErrContractViolation indicates input or output data doesn't match the JSON Schema.
type ErrContractViolation struct {
	NodeID    string
	Direction string // "input" or "output"
	Details   string
}

func (e *ErrContractViolation) Error() string {
	return fmt.Sprintf("contract violation on node %s (%s): %s", e.NodeID, e.Direction, e.Details)
}

// --- Orchestration Errors ---

// ErrFlowExecution indicates an error during flow execution.
type ErrFlowExecution struct {
	FlowID  string
	NodeID  string
	Message string
	Cause   error
}

func (e *ErrFlowExecution) Error() string {
	msg := fmt.Sprintf("flow execution error [flow=%s, node=%s]: %s", e.FlowID, e.NodeID, e.Message)
	if e.Cause != nil {
		msg += fmt.Sprintf(" (cause: %v)", e.Cause)
	}
	return msg
}

func (e *ErrFlowExecution) Unwrap() error {
	return e.Cause
}

// --- Routing Errors ---

// ErrNoRouteFound indicates no flow matched the user's intent.
type ErrNoRouteFound struct {
	Message       string
	BestMatch     string
	BestMatchConf float64
}

func (e *ErrNoRouteFound) Error() string {
	if e.BestMatch != "" {
		return fmt.Sprintf("no route found for message (best match: %s with confidence %.2f)", e.BestMatch, e.BestMatchConf)
	}
	return "no route found for message"
}

// --- Sandbox Errors ---

// ErrSandboxExecution indicates an error in sandbox script execution.
type ErrSandboxExecution struct {
	Script   string
	ExitCode int
	Stderr   string
}

func (e *ErrSandboxExecution) Error() string {
	return fmt.Sprintf("sandbox execution failed (exit code %d): %s", e.ExitCode, e.Stderr)
}

// ErrSandboxTimeout indicates the sandbox execution exceeded its time limit.
type ErrSandboxTimeout struct {
	TimeoutSec int
}

func (e *ErrSandboxTimeout) Error() string {
	return fmt.Sprintf("sandbox execution timed out after %ds", e.TimeoutSec)
}

// ErrSandboxSecurity indicates a security violation was detected pre-execution.
type ErrSandboxSecurity struct {
	Reason string
}

func (e *ErrSandboxSecurity) Error() string {
	return fmt.Sprintf("sandbox security violation: %s", e.Reason)
}
