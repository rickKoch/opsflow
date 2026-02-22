package scheduler

import (
	"context"
	"testing"
	"time"
)

// TestAddAndRemoveJob ensures a scheduled job is invoked and can be removed.
func TestAddAndRemoveJob(t *testing.T) {
	invoked := make(chan struct{}, 4)
	s := NewScheduler(func(ctx context.Context, job Job) error {
		select {
		case invoked <- struct{}{}:
		default:
		}
		return nil
	}, nil)
	s.Start()
	defer s.Stop()

	job := Job{ID: "test1", CronExpression: "@every 1s", Enabled: true}
	if err := s.AddOrUpdate(context.Background(), job); err != nil {
		t.Fatalf("AddOrUpdate failed: %v", err)
	}

	// expect at least one invocation within 2500ms
	select {
	case <-invoked:
		// ok
	case <-time.After(2500 * time.Millisecond):
		t.Fatalf("expected job to be invoked at least once")
	}

	// remove and ensure no further invocations after a short wait
	s.Remove(job.ID)
	// drain any pending
	drain := func() {
		for {
			select {
			case <-invoked:
			default:
				return
			}
		}
	}
	drain()
	// wait to ensure no new invocations
	select {
	case <-invoked:
		t.Fatalf("job invoked after removal")
	case <-time.After(1500 * time.Millisecond):
		// ok
	}
}

func TestInvalidTimeZone(t *testing.T) {
	s := NewScheduler(func(ctx context.Context, job Job) error { return nil }, nil)
	defer s.Stop()
	job := Job{ID: "tz1", CronExpression: "@every 1s", TimeZone: "Invalid/Zone", Enabled: true}
	if err := s.AddOrUpdate(context.Background(), job); err == nil {
		t.Fatalf("expected error for invalid time zone")
	}
}

func TestInvalidCronExpression(t *testing.T) {
	s := NewScheduler(func(ctx context.Context, job Job) error { return nil }, nil)
	defer s.Stop()
	job := Job{ID: "bad1", CronExpression: "not a cron", Enabled: true}
	if err := s.AddOrUpdate(context.Background(), job); err == nil {
		t.Fatalf("expected error for invalid cron expression")
	}
}
