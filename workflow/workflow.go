package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rickKoch/opsflow/actor"
	"github.com/rickKoch/opsflow/logging"
	"github.com/rickKoch/opsflow/persistence"
	"github.com/rickKoch/opsflow/tracing"
)

// Step represents a single actor invocation within a workflow.
type Step struct {
	ID         string        `json:"id"`
	Target     actor.PID     `json:"target"`
	Message    actor.Message `json:"message"`
	MaxRetries int           `json:"max_retries"`
	Backoff    time.Duration `json:"backoff"`
	// Optional Cron schedule for this individual step.
	Cron string `json:"cron,omitempty"`
	// Depends lists step IDs that must complete before this step is eligible to run.
	Depends []string `json:"depends,omitempty"`
}

// Workflow represents a linear sequence of steps. (Graph future)
type Workflow struct {
	ID    string `json:"id"`
	Steps []Step `json:"steps"`
	// Optional cron expression to schedule this workflow automatically.
	Cron string `json:"cron,omitempty"`
}

// workflowState is persisted between runs.
type workflowState struct {
	WorkflowID string `json:"workflow_id"`
	// CompletedSteps maps step ID -> true when a step finished successfully.
	CompletedSteps map[string]bool `json:"completed_steps"`
	// Current is for compatibility with the previous sequential manager.
	Current   int            `json:"current"`
	Attempts  map[string]int `json:"attempts"`
	Completed bool           `json:"completed"`
	Failed    bool           `json:"failed"`
	LastError string         `json:"last_error"`
}

// Manager runs workflows, persists state, and supports retries.
type Manager struct {
	pers   persistence.Persistence
	router func(context.Context, actor.PID, actor.Message) error
	log    logging.Logger
	tracer tracing.Tracer
	mu     sync.Mutex
}

func NewManager(p persistence.Persistence, router func(context.Context, actor.PID, actor.Message) error, log logging.Logger, tracer tracing.Tracer) *Manager {
	if log == nil {
		log = logging.StdLogger{}
	}
	if tracer == nil {
		tracer = tracing.NoopTracer{}
	}
	return &Manager{pers: p, router: router, log: log, tracer: tracer}
}

func (m *Manager) stateKey(id string) persistence.PID { return persistence.PID("workflow:" + id) }

func (m *Manager) persistState(ctx context.Context, st *workflowState) error {
	if m.pers == nil {
		return nil
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return m.pers.SaveSnapshot(ctx, m.stateKey(st.WorkflowID), b)
}

func (m *Manager) loadState(ctx context.Context, id string) (*workflowState, error) {
	if m.pers == nil {
		return nil, nil
	}
	b, err := m.pers.LoadSnapshot(ctx, m.stateKey(id))
	if err != nil || len(b) == 0 {
		return nil, nil
	}
	var st workflowState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// Start runs the workflow asynchronously and persists progress.
func (m *Manager) Start(ctx context.Context, wf *Workflow) error {
	go m.run(ctx, wf)
	return nil
}

func (m *Manager) run(ctx context.Context, wf *Workflow) {
	ctx, span := m.tracer.Start(ctx, "workflow.run")
	defer span.End(nil)

	// load previous state if any
	st, _ := m.loadState(ctx, wf.ID)
	if st == nil {
		st = &workflowState{WorkflowID: wf.ID, Current: 0, Attempts: map[string]int{}}
	}

	for st.Current < len(wf.Steps) {
		step := wf.Steps[st.Current]
		attempts := st.Attempts[step.ID]
		err := m.router(ctx, step.Target, step.Message)
		if err == nil {
			// success
			st.Current++
			st.Attempts[step.ID] = 0
			st.LastError = ""
			_ = m.persistState(ctx, st)
			continue
		}
		// failure
		attempts++
		st.Attempts[step.ID] = attempts
		st.LastError = err.Error()
		_ = m.persistState(ctx, st)
		m.log.Warn("workflow step failed", "workflow", wf.ID, "step", step.ID, "err", err.Error(), "attempt", attempts)
		if attempts > step.MaxRetries {
			st.Failed = true
			_ = m.persistState(ctx, st)
			span.End(err)
			return
		}
		// backoff
		select {
		case <-time.After(step.Backoff * time.Duration(1<<uint(attempts-1))):
		case <-ctx.Done():
			return
		}
	}
	st.Completed = true
	_ = m.persistState(ctx, st)
}

// StartParallel runs the workflow honoring step dependencies and running
// independent steps in parallel. Progress is persisted to allow recovery.
func (m *Manager) StartParallel(ctx context.Context, wf *Workflow) error {
	go m.runParallel(ctx, wf)
	return nil
}

// runParallel is the advanced scheduler that supports dependencies and parallelism.
func (m *Manager) runParallel(ctx context.Context, wf *Workflow) {
	// copy of the advanced scheduler implemented earlier
	ctx, span := m.tracer.Start(ctx, "workflow.run.parallel")
	defer span.End(nil)

	st, _ := m.loadState(ctx, wf.ID)
	if st == nil {
		st = &workflowState{WorkflowID: wf.ID, CompletedSteps: map[string]bool{}, Attempts: map[string]int{}}
	}

	depsSatisfied := func(step Step) bool {
		for _, d := range step.Depends {
			if !st.CompletedSteps[d] {
				return false
			}
		}
		return true
	}

	running := map[string]bool{}
	type stepResult struct {
		id  string
		err error
	}
	resCh := make(chan stepResult)
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for {
		if st.Failed {
			span.End(fmt.Errorf("workflow failed: %s", st.LastError))
			return
		}
		allDone := true
		for _, s := range wf.Steps {
			if !st.CompletedSteps[s.ID] {
				allDone = false
				break
			}
		}
		if allDone {
			st.Completed = true
			_ = m.persistState(wctx, st)
			return
		}

		launched := false
		for _, step := range wf.Steps {
			if st.CompletedSteps[step.ID] || running[step.ID] {
				continue
			}
			if !depsSatisfied(step) {
				continue
			}
			running[step.ID] = true
			launched = true
			go func(s Step) {
				for {
					select {
					case <-wctx.Done():
						return
					default:
					}
					err := m.router(wctx, s.Target, s.Message)
					if err == nil {
						resCh <- stepResult{id: s.ID, err: nil}
						return
					}
					m.mu.Lock()
					attempts := st.Attempts[s.ID]
					attempts++
					st.Attempts[s.ID] = attempts
					st.LastError = err.Error()
					_ = m.persistState(wctx, st)
					m.mu.Unlock()
					m.log.Warn("workflow step failed", "workflow", wf.ID, "step", s.ID, "err", err.Error(), "attempt", attempts)
					if attempts >= s.MaxRetries {
						resCh <- stepResult{id: s.ID, err: err}
						return
					}
					select {
					case <-time.After(s.Backoff * time.Duration(1<<uint(attempts-1))):
					case <-wctx.Done():
						return
					}
				}
			}(step)
		}

		if !launched {
			select {
			case r := <-resCh:
				m.mu.Lock()
				delete(running, r.id)
				if r.err != nil {
					st.Failed = true
					st.LastError = r.err.Error()
					_ = m.persistState(wctx, st)
					m.mu.Unlock()
					cancel()
					span.End(r.err)
					return
				}
				st.CompletedSteps[r.id] = true
				st.Attempts[r.id] = 0
				st.LastError = ""
				_ = m.persistState(wctx, st)
				m.mu.Unlock()
			case <-wctx.Done():
				return
			}
		} else {
			select {
			case r := <-resCh:
				m.mu.Lock()
				delete(running, r.id)
				if r.err != nil {
					st.Failed = true
					st.LastError = r.err.Error()
					_ = m.persistState(wctx, st)
					m.mu.Unlock()
					cancel()
					span.End(r.err)
					return
				}
				st.CompletedSteps[r.id] = true
				st.Attempts[r.id] = 0
				st.LastError = ""
				_ = m.persistState(wctx, st)
				m.mu.Unlock()
			case <-wctx.Done():
				return
			}
		}
	}
}
