package actor

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rickKoch/opsflow/persistence"
)

// ActorState is a helper structure saved to persistence.
type ActorState struct {
	CreatedAt time.Time `json:"created_at"`
}

// SaveSnapshot saves actor metadata.
func (r *Registry) SaveSnapshot(ctx context.Context, id PID) error {
	if r.pers == nil {
		return nil
	}
	st := ActorState{CreatedAt: r.localRefs[id].created}
	b, _ := json.Marshal(st)
	return r.pers.SaveSnapshot(ctx, persistence.PID(id), b)
}

// SaveMailbox persists mailbox contents.
func (r *Registry) SaveMailbox(ctx context.Context, id PID) error {
	if r.pers == nil {
		return nil
	}
	r.mu.RLock()
	ref, ok := r.localRefs[id]
	r.mu.RUnlock()
	if !ok {
		return nil
	}
	msgs := ref.SnapshotMailbox()
	var out [][]byte
	for _, m := range msgs {
		out = append(out, m.Payload)
	}
	return r.pers.SaveMailbox(ctx, persistence.PID(id), out)
}
