package actor

import (
	"context"
	"sync"
	"time"

	"github.com/rickKoch/opsflow/logging"
	"github.com/rickKoch/opsflow/persistence"
	"github.com/rickKoch/opsflow/tracing"
)

// Registry manages local and remote actors.
type RemoteActorRef struct {
	ID       PID
	Address  string
	LastSeen time.Time
}

type Registry struct {
	mu              sync.RWMutex
	localRefs       map[PID]*ActorRef
	remoteRefs      map[PID]*RemoteActorRef
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
	return &Registry{
		localRefs:  make(map[PID]*ActorRef),
		remoteRefs: make(map[PID]*RemoteActorRef),
		pers:       p,
		log:        log,
		tracer:     tracer,
	}
}

// RegisterRemote registers or updates a single remote actor.
func (r *Registry) RegisterRemote(a RemoteActorRef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.remoteRefs[a.ID] = &RemoteActorRef{ID: a.ID, Address: a.Address, LastSeen: time.Now()}
}

// UpdateRemoteActors replaces/updates remote refs from a peer push.
func (r *Registry) UpdateRemoteActors(ctx context.Context, actors []RemoteActorRef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range actors {
		r.remoteRefs[a.ID] = &RemoteActorRef{ID: a.ID, Address: a.Address, LastSeen: a.LastSeen}
	}
}

// GetRemote returns the remote actor ref for a pid if present.
func (r *Registry) GetRemote(id PID) (*RemoteActorRef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ra, ok := r.remoteRefs[id]
	return ra, ok
}

// ListLocalActors returns a slice of local actors as RemoteActorRef entries (address empty for local).
func (r *Registry) ListLocalActors() []RemoteActorRef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RemoteActorRef, 0, len(r.localRefs))
	for id := range r.localRefs {
		out = append(out, RemoteActorRef{ID: id, Address: ""})
	}
	return out
}

// ListLocalActorsWithAddress returns local actors including the node address field.
// For local entries the Address field is empty; callers can set it when building
// propagation/Register requests to indicate where these actors can be reached.
func (r *Registry) ListLocalActorsWithAddress(addr string) []RemoteActorRef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RemoteActorRef, 0, len(r.localRefs))
	for id := range r.localRefs {
		out = append(out, RemoteActorRef{ID: id, Address: addr})
	}
	return out
}

// RemoveRemote deletes a remote actor entry explicitly.
func (r *Registry) RemoveRemote(id PID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.remoteRefs, id)
}

// StartRemotePruner periodically removes remote actors that have not been seen within ttl.
func (r *Registry) StartRemotePruner(ctx context.Context, ttl time.Duration, interval time.Duration) {
	if ttl <= 0 || interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				cutoff := time.Now().Add(-ttl)
				r.mu.Lock()
				for id, ra := range r.remoteRefs {
					if ra.LastSeen.Before(cutoff) {
						delete(r.remoteRefs, id)
						r.log.Info("pruned stale remote actor", "id", id, "addr", ra.Address)
					}
				}
				r.mu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()
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
	ids := make([]PID, 0, len(r.localRefs))
	for id := range r.localRefs {
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
	if _, ok := r.localRefs[id]; ok {
		return nil, nil
	}
	cctx, cancel := context.WithCancel(ctx)
	ref := &ActorRef{
		ID:      id,
		mail:    newMailboxFor(id, r.pers, mailboxSize),
		actor:   a,
		stopped: make(chan struct{}),
		created: time.Now(),
		ctx:     cctx,
		cancel:  cancel,
	}
	r.localRefs[id] = ref
	go r.runActor(ref.ctx, ref)
	r.log.Info("spawned actor", "id", id)
	return ref, nil
}

func (r *Registry) Send(ctx context.Context, id PID, msg Message) error {
	r.mu.RLock()
	ref, ok := r.localRefs[id]
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
			// close mailbox flusher if supported
			ref.mail.Close()
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
