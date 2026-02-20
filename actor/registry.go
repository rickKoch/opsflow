package actor

import (
	"context"
	"sync"
	"time"

	"github.com/rickKoch/opsflow/logging"
	"github.com/rickKoch/opsflow/persistence"
	"github.com/rickKoch/opsflow/tracing"
)

// Registry manages local actors.
type Registry struct {
	mu     sync.RWMutex
	refs   map[PID]*ActorRef
	pers   persistence.Persistence
	log    logging.Logger
	tracer tracing.Tracer
}

func NewRegistry(p persistence.Persistence, log logging.Logger, tracer tracing.Tracer) *Registry {
	if log == nil {
		log = logging.StdLogger{}
	}
	if tracer == nil {
		tracer = tracing.NoopTracer{}
	}
	return &Registry{refs: make(map[PID]*ActorRef), pers: p, log: log, tracer: tracer}
}

func (r *Registry) Spawn(ctx context.Context, id PID, a Actor, mailboxSize int) (*ActorRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.refs[id]; ok {
		return nil, nil
	}
	ref := &ActorRef{ID: id, mail: newChanMailbox(mailboxSize), actor: a, stopped: make(chan struct{}), created: time.Now()}
	r.refs[id] = ref
	go r.runActor(ctx, ref)
	r.log.Info("spawned actor", "id", id)
	return ref, nil
}

func (r *Registry) Send(ctx context.Context, id PID, msg Message) error {
	r.mu.RLock()
	ref, ok := r.refs[id]
	r.mu.RUnlock()
	if !ok {
		return ErrNotFound
	}
	ref.mail.Enqueue(msg)
	return nil
}

var ErrNotFound = &NotFoundError{}

type NotFoundError struct{}

func (NotFoundError) Error() string { return "actor not found" }

func (r *Registry) runActor(ctx context.Context, ref *ActorRef) {
	ctx, span := r.tracer.Start(ctx, "runActor")
	defer span.End(nil)
	for {
		select {
		case <-ctx.Done():
			r.log.Info("actor context done", "id", ref.ID)
			close(ref.stopped)
			return
		default:
			msg, ok := ref.mail.Dequeue()
			if !ok {
				// sleep briefly to avoid busy loop
				time.Sleep(10 * time.Millisecond)
				continue
			}
			ref.actor.Receive(ctx, msg)
		}
	}
}
