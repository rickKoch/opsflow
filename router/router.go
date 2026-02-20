package router

import (
	"context"

	"github.com/rickKoch/opsflow/actor"
	"github.com/rickKoch/opsflow/logging"
	"github.com/rickKoch/opsflow/tracing"
)

type Router struct {
	reg    *actor.Registry
	log    logging.Logger
	tracer tracing.Tracer
}

func NewRouter(reg *actor.Registry, log logging.Logger, tracer tracing.Tracer) *Router {
	if log == nil {
		log = logging.StdLogger{}
	}
	if tracer == nil {
		tracer = tracing.NoopTracer{}
	}
	return &Router{reg: reg, log: log, tracer: tracer}
}

func (r *Router) Send(ctx context.Context, pid actor.PID, msg actor.Message) error {
	// for now, local-only
	return r.reg.Send(ctx, pid, msg)
}
