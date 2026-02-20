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
	mu              sync.RWMutex
	refs            map[PID]*ActorRef
	pers            persistence.Persistence
	log             logging.Logger
	tracer          tracing.Tracer
	persistInterval time.Duration
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

// StartPersistenceLoop starts a background goroutine that periodically persists actor snapshots and mailboxes.
func (r *Registry) StartPersistenceLoop(ctx context.Context, interval time.Duration) {
	if r.pers == nil || interval <= 0 {
		return
	}
	r.persistInterval = interval
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = r.SaveAll(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// SaveAll persists snapshots and mailboxes for all registered actors.
func (r *Registry) SaveAll(ctx context.Context) error {
	r.mu.RLock()
	ids := make([]PID, 0, len(r.refs))
	for id := range r.refs {
		ids = append(ids, id)
	}
	r.mu.RUnlock()
	for _, id := range ids {
		_ = r.SaveSnapshot(ctx, id)
		_ = r.SaveMailbox(ctx, id)
	}
	return nil
}

func (r *Registry) Spawn(ctx context.Context, id PID, a Actor, mailboxSize int) (*ActorRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.refs[id]; ok {
		return nil, nil
	}
	cctx, cancel := context.WithCancel(ctx)
	ref := &ActorRef{ID: id, mail: newChanMailbox(mailboxSize), actor: a, stopped: make(chan struct{}), created: time.Now(), ctx: cctx, cancel: cancel}
	r.refs[id] = ref
	go r.runActor(ref.ctx, ref)
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
	ref.Enqueue(msg)
	return nil
}

var ErrNotFound = &NotFoundError{}

type NotFoundError struct{}

func (NotFoundError) Error() string { return "actor not found" }

func (r *Registry) runActor(ctx context.Context, ref *ActorRef) {
	ctx, span := r.tracer.Start(ctx, "runActor")
	defer span.End(nil)
	// optional lifecycle: call Started if implemented
	if s, ok := ref.actor.(interface{ Started(context.Context) }); ok {
		s.Started(ctx)
	}
	for {
		select {
		case <-ctx.Done():
			r.log.Info("actor context done", "id", ref.ID)
			// lifecycle stop
			if st, ok := ref.actor.(interface{ Stopped(context.Context) }); ok {
				st.Stopped(ctx)
			}
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
