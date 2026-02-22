package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rickKoch/opsflow/logging"
	cron "github.com/robfig/cron/v3"
)

// printfAdapter adapts the repo's structured Logger to a Printf(logger) used by
// robfig/cron's VerbosePrintfLogger.
type printfAdapter struct{ l logging.Logger }

func (p printfAdapter) Printf(format string, v ...interface{}) { p.l.Info(fmt.Sprintf(format, v...)) }

// Job represents a scheduled job persisted elsewhere. It carries enough
// information for the scheduler to invoke the correct actor with message.
type Job struct {
	ID             string `json:"id"`
	Type           string `json:"type"` // e.g. "workflow"
	WorkflowID     string `json:"workflow_id"`
	CronExpression string `json:"cron"`
	// TimeZone is an optional IANA time zone name (eg. "America/New_York").
	// If set, the scheduler will interpret the cron expression in that zone.
	TimeZone string `json:"timezone,omitempty"`
	// Actor scheduling fields (optional)
	ActorID string `json:"actor_id,omitempty"`
	Payload []byte `json:"payload,omitempty"`
	MsgType string `json:"msg_type,omitempty"`
	// StepID is used when scheduling a specific workflow step.
	StepID  string `json:"step_id,omitempty"`
	Enabled bool   `json:"enabled"`
}

type Scheduler struct {
	mu      sync.Mutex
	cron    *cron.Cron
	entries map[string]cron.EntryID // map job.ID -> cron entry id
	invoke  func(context.Context, Job) error
	log     logging.Logger
}

func NewScheduler(invoke func(context.Context, Job) error, log logging.Logger) *Scheduler {
	if log == nil {
		log = logging.StdLogger{}
	}
	// cron.VerbosePrintfLogger expects a Printf-like logger. Create a small
	// adapter that delegates to our structured logger.
	pad := printfAdapter{l: log}

	// Use DelayIfStillRunning to ensure overlapping runs are delayed.
	c := cron.New(
		cron.WithChain(cron.DelayIfStillRunning(cron.VerbosePrintfLogger(pad))),
		cron.WithParser(cron.NewParser(cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow|cron.Descriptor)),
	)
	return &Scheduler{cron: c, entries: make(map[string]cron.EntryID), invoke: invoke, log: log}
}

// Start the underlying cron scheduler.
func (s *Scheduler) Start() { s.cron.Start() }

// Stop stops the scheduler and waits for running jobs to complete.
func (s *Scheduler) Stop() context.Context { return s.cron.Stop() }

// AddOrUpdate registers a job with the cron scheduler. If a job with the
// same ID exists it will be removed first and replaced. The job must be
// persisted by the caller; this function only affects in-memory scheduling.
func (s *Scheduler) AddOrUpdate(ctx context.Context, job Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !job.Enabled {
		// If disabled, ensure it's removed from cron
		if id, ok := s.entries[job.ID]; ok {
			s.cron.Remove(id)
			delete(s.entries, job.ID)
		}
		return nil
	}
	// validate cron expression by attempting to add a noop job
	// first remove existing if present
	if id, ok := s.entries[job.ID]; ok {
		s.cron.Remove(id)
		delete(s.entries, job.ID)
	}
	// create job closure
	j := func() {
		ctx2 := context.Background()
		s.log.Info("scheduler: invoking job", "job_id", job.ID, "expr", job.CronExpression)
		if err := s.invoke(ctx2, job); err != nil {
			s.log.Info("scheduler: job invocation failed", "job_id", job.ID, "err", err)
		}
	}
	// If a TimeZone is provided, validate it and prepend the CRON_TZ prefix
	spec := job.CronExpression
	if job.TimeZone != "" {
		// validate time zone
		if _, err := time.LoadLocation(job.TimeZone); err != nil {
			return fmt.Errorf("invalid time zone %q: %w", job.TimeZone, err)
		}
		spec = "CRON_TZ=" + job.TimeZone + " " + spec
	}

	eid, err := s.cron.AddFunc(spec, j)
	if err != nil {
		return fmt.Errorf("add cron job: %w", err)
	}
	s.entries[job.ID] = eid
	return nil
}

// Remove deletes a job from the cron scheduler (in-memory only).
func (s *Scheduler) Remove(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.entries[jobID]; ok {
		s.cron.Remove(id)
		delete(s.entries, jobID)
	}
}
