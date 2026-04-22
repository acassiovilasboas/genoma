package core

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/acassiovilasboas/genoma/internal/shared"
)

// ScheduledFlowExecutor is the function called by FlowScheduler to run a flow.
// Implementations are responsible for loading the graph from storage.
type ScheduledFlowExecutor func(ctx context.Context, flowID string, input map[string]any) (*FlowResult, error)

// FlowScheduler polls Redis for due scheduled flow executions and runs them.
type FlowScheduler struct {
	stateBus *StateBus
	execute  ScheduledFlowExecutor
}

// NewFlowScheduler creates a scheduler backed by the given state bus and executor.
func NewFlowScheduler(stateBus *StateBus, execute ScheduledFlowExecutor) *FlowScheduler {
	return &FlowScheduler{stateBus: stateBus, execute: execute}
}

// Schedule registers a future flow execution and returns the created schedule.
func (fs *FlowScheduler) Schedule(ctx context.Context, flowID string, input map[string]any, at time.Time) (*FlowSchedule, error) {
	if at.IsZero() {
		return nil, fmt.Errorf("scheduled_at is required")
	}
	s := &FlowSchedule{
		ID:          shared.NewID(),
		FlowID:      flowID,
		Input:       input,
		ScheduledAt: at,
		Status:      ScheduleStatusPending,
		CreatedAt:   time.Now(),
	}
	if err := fs.stateBus.SetSchedule(ctx, s); err != nil {
		return nil, fmt.Errorf("persist schedule: %w", err)
	}
	slog.Info("flow scheduled", "schedule_id", s.ID, "flow_id", flowID, "at", at)
	return s, nil
}

// Cancel marks a pending schedule as cancelled.
func (fs *FlowScheduler) Cancel(ctx context.Context, scheduleID string) error {
	s, err := fs.stateBus.GetSchedule(ctx, scheduleID)
	if err != nil {
		return err
	}
	if s == nil {
		return fmt.Errorf("schedule not found: %s", scheduleID)
	}
	if s.Status != ScheduleStatusPending {
		return fmt.Errorf("cannot cancel schedule in status %s", s.Status)
	}
	s.Status = ScheduleStatusCancelled
	if err := fs.stateBus.SetSchedule(ctx, s); err != nil {
		return err
	}
	return fs.stateBus.RemoveScheduleFromIndex(ctx, scheduleID)
}

// List returns all schedules regardless of status.
func (fs *FlowScheduler) List(ctx context.Context) ([]*FlowSchedule, error) {
	return fs.stateBus.ListSchedules(ctx)
}

// Start runs the background polling loop until ctx is cancelled.
// Each tick it picks up any due schedules and executes them concurrently.
func (fs *FlowScheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	slog.Info("flow scheduler started")
	for {
		select {
		case <-ctx.Done():
			slog.Info("flow scheduler stopped")
			return
		case <-ticker.C:
			fs.runDue(ctx)
		}
	}
}

func (fs *FlowScheduler) runDue(ctx context.Context) {
	schedules, err := fs.stateBus.DueSchedules(ctx)
	if err != nil {
		slog.Error("scheduler: fetch due schedules", "error", err)
		return
	}
	for _, s := range schedules {
		s := s
		go func() {
			s.Status = ScheduleStatusRunning
			if err := fs.stateBus.SetSchedule(ctx, s); err != nil {
				slog.Error("scheduler: mark running", "schedule_id", s.ID, "error", err)
				return
			}
			fs.stateBus.RemoveScheduleFromIndex(ctx, s.ID)

			result, err := fs.execute(ctx, s.FlowID, s.Input)
			if err != nil {
				s.Status = ScheduleStatusFailed
				s.Error = err.Error()
				slog.Error("scheduled flow failed", "schedule_id", s.ID, "flow_id", s.FlowID, "error", err)
			} else {
				s.Status = ScheduleStatusDone
				s.RunID = result.RunID
				slog.Info("scheduled flow completed", "schedule_id", s.ID, "run_id", result.RunID)
			}
			fs.stateBus.SetSchedule(ctx, s)
		}()
	}
}
