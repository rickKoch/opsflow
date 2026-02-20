package orchestrator

import (
	"context"

	"github.com/rickKoch/opsflow/actor"
	"github.com/rickKoch/opsflow/logging"
	"github.com/rickKoch/opsflow/persistence"
	"github.com/rickKoch/opsflow/tracing"
)

// Orchestrator coordinates actor lifecycles and workflows.
type Orchestrator struct {
	reg    *actor.Registry
	pers   persistence.Persistence
	log    logging.Logger
	tracer tracing.Tracer
}

func NewOrchestrator(reg *actor.Registry, pers persistence.Persistence, log logging.Logger, tracer tracing.Tracer) *Orchestrator {
	if log == nil {
		log = logging.StdLogger{}
	}
	if tracer == nil {
		tracer = tracing.NoopTracer{}
	}
	return &Orchestrator{reg: reg, pers: pers, log: log, tracer: tracer}
}

// SpawnAndRegister spawns an actor and ensures persistence for it.
func (o *Orchestrator) SpawnAndRegister(ctx context.Context, id actor.PID, a actor.Actor, mailboxSize int) error {
	_, err := o.reg.Spawn(ctx, id, a, mailboxSize)
	if err != nil {
		return err
	}
	// save snapshot for new actor
	_ = o.reg.SaveSnapshot(ctx, id)
	return nil
}
