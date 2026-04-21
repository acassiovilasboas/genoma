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

	// Default TTLs
	defaultNodeStateTTL = 1 * time.Hour
	defaultConvCtxTTL   = 24 * time.Hour
	defaultEmbCacheTTL  = 7 * 24 * time.Hour
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
