package workflow

import (
	"context"
	"encoding/json"
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
}

// Workflow represents a linear sequence of steps. (Graph future)
type Workflow struct {
	ID    string `json:"id"`
	Steps []Step `json:"steps"`
}

// workflowState is persisted between runs.
type workflowState struct {
	WorkflowID string         `json:"workflow_id"`
	Current    int            `json:"current"`
	Attempts   map[string]int `json:"attempts"`
	Completed  bool           `json:"completed"`
	Failed     bool           `json:"failed"`
	LastError  string         `json:"last_error"`
}

// Manager runs workflows, persists state, and supports retries.
type Manager struct {
	pers   persistence.Persistence
	router func(context.Context, actor.PID, actor.Message) error
	log    logging.Logger
	tracer tracing.Tracer
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
		if attempts >= step.MaxRetries {
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
