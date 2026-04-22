package core

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// Key prefixes for Redis state storage.
	prefixNodeState = "genoma:state:node:"
	prefixFlowState = "genoma:state:flow:"
	prefixConvCtx   = "genoma:conv:"
	prefixEmbCache  = "genoma:emb:cache:"
	prefixHITL      = "genoma:hitl:"
	prefixSchedule  = "genoma:schedule:"

	// Sorted set index of pending scheduled runs (score = unix timestamp).
	scheduleIndex = "genoma:schedule:pending"
	// Set of all schedule IDs ever created (for listing history).
	scheduleAllSet = "genoma:schedule:all"

	// Default TTLs
	defaultNodeStateTTL = 1 * time.Hour
	defaultConvCtxTTL   = 24 * time.Hour
	defaultEmbCacheTTL  = 7 * 24 * time.Hour
	defaultHITLTTL      = 72 * time.Hour
	defaultScheduleTTL  = 7 * 24 * time.Hour
)

// Schedule status constants.
const (
	ScheduleStatusPending   = "PENDING"
	ScheduleStatusRunning   = "RUNNING"
	ScheduleStatusDone      = "DONE"
	ScheduleStatusCancelled = "CANCELLED"
	ScheduleStatusFailed    = "FAILED"
)

// StateBus manages shared state across the framework via Redis.
// It provides persistence for node execution state, conversation context,
// and embedding cache.
type StateBus struct {
	rdb *redis.Client
}

// NewStateBus creates a new state bus backed by Redis.
func NewStateBus(rdb *redis.Client) *StateBus {
	return &StateBus{rdb: rdb}
}

// --- Node State ---

// SetNodeState persists the execution state of a node instance.
func (sb *StateBus) SetNodeState(ctx context.Context, flowRunID, nodeID string, state *NodeInstance) error {
	key := prefixNodeState + flowRunID + ":" + nodeID
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal node state: %w", err)
	}
	return sb.rdb.Set(ctx, key, data, defaultNodeStateTTL).Err()
}

// GetNodeState retrieves the execution state of a node instance.
func (sb *StateBus) GetNodeState(ctx context.Context, flowRunID, nodeID string) (*NodeInstance, error) {
	key := prefixNodeState + flowRunID + ":" + nodeID
	data, err := sb.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get node state: %w", err)
	}

	var state NodeInstance
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal node state: %w", err)
	}
	return &state, nil
}

// --- Flow State ---

// FlowRun represents the overall state of a flow execution.
type FlowRun struct {
	ID        string         `json:"id"`
	FlowID    string         `json:"flow_id"`
	Status    NodeStatus     `json:"status"`
	Input     map[string]any `json:"input,omitempty"`
	Output    map[string]any `json:"output,omitempty"`
	Error     string         `json:"error,omitempty"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   time.Time      `json:"ended_at,omitempty"`
}

// SetFlowRun persists the overall flow execution state.
func (sb *StateBus) SetFlowRun(ctx context.Context, run *FlowRun) error {
	key := prefixFlowState + run.ID
	data, err := json.Marshal(run)
	if err != nil {
		return fmt.Errorf("marshal flow run: %w", err)
	}
	return sb.rdb.Set(ctx, key, data, defaultNodeStateTTL).Err()
}

// GetFlowRun retrieves the overall flow execution state.
func (sb *StateBus) GetFlowRun(ctx context.Context, flowRunID string) (*FlowRun, error) {
	key := prefixFlowState + flowRunID
	data, err := sb.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get flow run: %w", err)
	}

	var run FlowRun
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, fmt.Errorf("unmarshal flow run: %w", err)
	}
	return &run, nil
}

// --- Human-in-the-loop State ---

// HITLState holds everything needed to resume a flow run that is awaiting human feedback.
type HITLState struct {
	RunID      string                   `json:"run_id"`
	FlowID     string                   `json:"flow_id"`
	WaitNodeID string                   `json:"wait_node_id"`
	Prompt     string                   `json:"prompt"`
	// NodeOutput is the waiting node's output with internal fields stripped.
	NodeOutput map[string]any `json:"node_output"`
	// NodeInput is the input the waiting node received — used as originalInput when resuming.
	NodeInput map[string]any           `json:"node_input"`
	NodeRuns  map[string]*NodeInstance `json:"node_runs"`
	CreatedAt time.Time                `json:"created_at"`
}

// SetHITLState persists the human-in-the-loop state for a flow run.
func (sb *StateBus) SetHITLState(ctx context.Context, state *HITLState) error {
	key := prefixHITL + state.RunID
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal hitl state: %w", err)
	}
	return sb.rdb.Set(ctx, key, data, defaultHITLTTL).Err()
}

// GetHITLState retrieves the human-in-the-loop state for a flow run.
func (sb *StateBus) GetHITLState(ctx context.Context, runID string) (*HITLState, error) {
	key := prefixHITL + runID
	data, err := sb.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get hitl state: %w", err)
	}
	var state HITLState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal hitl state: %w", err)
	}
	return &state, nil
}

// DeleteHITLState removes the human-in-the-loop state after a successful resume.
func (sb *StateBus) DeleteHITLState(ctx context.Context, runID string) error {
	return sb.rdb.Del(ctx, prefixHITL+runID).Err()
}

// SetFlowRunWaiting persists a flow run with the longer HITL TTL.
func (sb *StateBus) SetFlowRunWaiting(ctx context.Context, run *FlowRun) error {
	key := prefixFlowState + run.ID
	data, err := json.Marshal(run)
	if err != nil {
		return fmt.Errorf("marshal flow run: %w", err)
	}
	return sb.rdb.Set(ctx, key, data, defaultHITLTTL).Err()
}

// --- Flow Schedule State ---

// FlowSchedule represents a scheduled future flow execution.
type FlowSchedule struct {
	ID          string         `json:"id"`
	FlowID      string         `json:"flow_id"`
	Input       map[string]any `json:"input"`
	ScheduledAt time.Time      `json:"scheduled_at"`
	Status      string         `json:"status"`
	RunID       string         `json:"run_id,omitempty"`
	Error       string         `json:"error,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

// SetSchedule persists a flow schedule and updates the sorted-set index.
func (sb *StateBus) SetSchedule(ctx context.Context, s *FlowSchedule) error {
	key := prefixSchedule + s.ID
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal schedule: %w", err)
	}
	pipe := sb.rdb.Pipeline()
	pipe.Set(ctx, key, data, defaultScheduleTTL)
	pipe.SAdd(ctx, scheduleAllSet, s.ID)
	if s.Status == ScheduleStatusPending {
		pipe.ZAdd(ctx, scheduleIndex, redis.Z{
			Score:  float64(s.ScheduledAt.Unix()),
			Member: s.ID,
		})
	}
	_, err = pipe.Exec(ctx)
	return err
}

// GetSchedule retrieves a flow schedule by ID.
func (sb *StateBus) GetSchedule(ctx context.Context, scheduleID string) (*FlowSchedule, error) {
	key := prefixSchedule + scheduleID
	data, err := sb.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get schedule: %w", err)
	}
	var s FlowSchedule
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshal schedule: %w", err)
	}
	return &s, nil
}

// ListSchedules returns all known schedules (all statuses).
func (sb *StateBus) ListSchedules(ctx context.Context) ([]*FlowSchedule, error) {
	ids, err := sb.rdb.SMembers(ctx, scheduleAllSet).Result()
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	schedules := make([]*FlowSchedule, 0, len(ids))
	for _, id := range ids {
		s, err := sb.GetSchedule(ctx, id)
		if err != nil || s == nil {
			continue
		}
		schedules = append(schedules, s)
	}
	return schedules, nil
}

// DueSchedules returns PENDING schedules whose scheduled time has passed.
func (sb *StateBus) DueSchedules(ctx context.Context) ([]*FlowSchedule, error) {
	now := float64(time.Now().Unix())
	ids, err := sb.rdb.ZRangeByScore(ctx, scheduleIndex, &redis.ZRangeBy{
		Min: "-inf",
		Max: fmt.Sprintf("%.0f", now),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("due schedules: %w", err)
	}
	schedules := make([]*FlowSchedule, 0, len(ids))
	for _, id := range ids {
		s, err := sb.GetSchedule(ctx, id)
		if err != nil || s == nil {
			continue
		}
		if s.Status == ScheduleStatusPending {
			schedules = append(schedules, s)
		}
	}
	return schedules, nil
}

// RemoveScheduleFromIndex removes a schedule ID from the pending sorted-set index.
func (sb *StateBus) RemoveScheduleFromIndex(ctx context.Context, scheduleID string) error {
	return sb.rdb.ZRem(ctx, scheduleIndex, scheduleID).Err()
}

// --- Conversation Context ---

// SetConversationContext persists the conversation context for a chat session.
func (sb *StateBus) SetConversationContext(ctx context.Context, sessionID string, data map[string]any) error {
	key := prefixConvCtx + sessionID
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal conversation context: %w", err)
	}
	return sb.rdb.Set(ctx, key, jsonData, defaultConvCtxTTL).Err()
}

// GetConversationContext retrieves the conversation context for a chat session.
func (sb *StateBus) GetConversationContext(ctx context.Context, sessionID string) (map[string]any, error) {
	key := prefixConvCtx + sessionID
	data, err := sb.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return make(map[string]any), nil
	}
	if err != nil {
		return nil, fmt.Errorf("get conversation context: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal conversation context: %w", err)
	}
	return result, nil
}

// --- Embedding Cache ---

// CacheEmbedding stores an embedding vector in Redis cache.
func (sb *StateBus) CacheEmbedding(ctx context.Context, text string, embedding []float32) error {
	key := prefixEmbCache + hashString(text)
	data, err := json.Marshal(embedding)
	if err != nil {
		return err
	}
	return sb.rdb.Set(ctx, key, data, defaultEmbCacheTTL).Err()
}

// GetCachedEmbedding retrieves a cached embedding vector.
func (sb *StateBus) GetCachedEmbedding(ctx context.Context, text string) ([]float32, error) {
	key := prefixEmbCache + hashString(text)
	data, err := sb.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var embedding []float32
	if err := json.Unmarshal(data, &embedding); err != nil {
		return nil, err
	}
	return embedding, nil
}

// --- Pub/Sub ---

// Publish sends a message to a Redis pub/sub channel.
func (sb *StateBus) Publish(ctx context.Context, channel string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return sb.rdb.Publish(ctx, "genoma:"+channel, payload).Err()
}

// Subscribe listens for messages on a Redis pub/sub channel.
func (sb *StateBus) Subscribe(ctx context.Context, channel string) <-chan string {
	ch := make(chan string, 64)
	sub := sb.rdb.Subscribe(ctx, "genoma:"+channel)

	go func() {
		defer close(ch)
		defer sub.Close()

		msgCh := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				select {
				case ch <- msg.Payload:
				default:
					// Drop message if channel is full
				}
			}
		}
	}()

	return ch
}

// Ping checks Redis connectivity.
func (sb *StateBus) Ping(ctx context.Context) error {
	return sb.rdb.Ping(ctx).Err()
}

// Close closes the Redis connection.
func (sb *StateBus) Close() error {
	return sb.rdb.Close()
}

// hashString creates a simple hash of a string for cache keys.
func hashString(s string) string {
	var h uint64
	for _, c := range s {
		h = h*31 + uint64(c)
	}
	return fmt.Sprintf("%x", h)
}
