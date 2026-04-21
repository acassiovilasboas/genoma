package shared

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

// Event represents a framework event.
type Event struct {
	Type      string `json:"type"`
	FlowRunID string `json:"flow_run_id,omitempty"`
	NodeID    string `json:"node_id,omitempty"`
	Data      any    `json:"data,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// EventBus provides pub/sub functionality via Redis.
type EventBus struct {
	rdb *redis.Client
}

// NewEventBus creates a new event bus backed by Redis.
func NewEventBus(rdb *redis.Client) *EventBus {
	return &EventBus{rdb: rdb}
}

// Publish sends an event to the specified channel.
func (eb *EventBus) Publish(ctx context.Context, channel string, event Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return eb.rdb.Publish(ctx, "genoma:"+channel, data).Err()
}

// Subscribe listens for events on the specified channel.
// Returns a channel that receives events. Cancel the context to stop.
func (eb *EventBus) Subscribe(ctx context.Context, channel string) <-chan Event {
	events := make(chan Event, 64)
	sub := eb.rdb.Subscribe(ctx, "genoma:"+channel)

	go func() {
		defer close(events)
		defer sub.Close()

		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var event Event
				if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
					slog.Error("failed to unmarshal event", "error", err, "channel", channel)
					continue
				}
				select {
				case events <- event:
				default:
					slog.Warn("event bus channel full, dropping event", "channel", channel)
				}
			}
		}
	}()

	return events
}
