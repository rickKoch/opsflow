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

// Restore actor state from persistence if available.
func (r *Registry) restoreActor(ctx context.Context, id PID, a Actor, mailboxSize int) (*ActorRef, error) {
	// attempt to load snapshot and mailbox
	var ref *ActorRef
	if r.pers != nil {
		if b, err := r.pers.LoadSnapshot(ctx, persistence.PID(id)); err == nil && len(b) > 0 {
			var st ActorState
			_ = json.Unmarshal(b, &st)
			ref = &ActorRef{ID: id, mail: newChanMailbox(mailboxSize), actor: a, stopped: make(chan struct{}), created: st.CreatedAt}
		}
		// load mailbox
		if msgs, err := r.pers.LoadMailbox(ctx, persistence.PID(id)); err == nil && len(msgs) > 0 {
			if ref == nil {
				ref = &ActorRef{ID: id, mail: newChanMailbox(mailboxSize), actor: a, stopped: make(chan struct{}), created: time.Now()}
			}
			for _, m := range msgs {
				ref.mail.Enqueue(Message{Payload: m})
			}
		}
	}
	if ref == nil {
		return nil, nil
	}
	r.mu.Lock()
	r.refs[id] = ref
	r.mu.Unlock()
	go r.runActor(ctx, ref)
	r.log.Info("restored actor", "id", id)
	return ref, nil
}

// SaveSnapshot saves actor metadata.
func (r *Registry) SaveSnapshot(ctx context.Context, id PID) error {
	if r.pers == nil {
		return nil
	}
	st := ActorState{CreatedAt: r.refs[id].created}
	b, _ := json.Marshal(st)
	return r.pers.SaveSnapshot(ctx, persistence.PID(id), b)
}

// SaveMailbox persists mailbox contents.
func (r *Registry) SaveMailbox(ctx context.Context, id PID) error {
	if r.pers == nil {
		return nil
	}
	r.mu.RLock()
	ref, ok := r.refs[id]
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
