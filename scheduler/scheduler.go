package scheduler

import (
	"context"
	"time"

	"github.com/rickKoch/opsflow/actor"
)

type Scheduler struct {
}

func NewScheduler() *Scheduler { return &Scheduler{} }

// ScheduleOnce schedules a single message to pid after duration d.
func (s *Scheduler) ScheduleOnce(ctx context.Context, pid actor.PID, msg actor.Message, d time.Duration, r func(actor.PID, actor.Message) error) {
	go func() {
		select {
		case <-time.After(d):
			_ = r(pid, msg)
		case <-ctx.Done():
			return
		}
	}()
}
