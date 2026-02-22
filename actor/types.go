package actor

import (
	"context"
	"sync"
	"time"

	"github.com/rickKoch/opsflow/persistence"
)

// Message is a wrapper used internally.
type Message struct {
	Payload []byte
	Type    string
}

// Actor is the interface user implementations should implement.
type Actor interface {
	Receive(ctx context.Context, msg Message)
}

// ActorStarter is an optional interface actors can implement to receive start lifecycle event.
type ActorStarter interface {
	Started(ctx context.Context)
}

// ActorStopper is an optional interface actors can implement to receive stop lifecycle event.
type ActorStopper interface {
	Stopped(ctx context.Context)
}

// PID is the actor id.
type PID string

// Options for spawning actors.
type SpawnOptions struct {
	SupervisorStrategy string
}

// Mailbox interface to support enqueuing and draining for persistence.
type Mailbox interface {
	Enqueue(msg Message)
	Dequeue() (Message, bool)
	Drain() []Message
	Close()
}

// default mailbox implementation using channel with buffer.
type chanMailbox struct {
	ch chan Message
}

func newChanMailbox(size int) *chanMailbox {
	return &chanMailbox{ch: make(chan Message, size)}
}

func (m *chanMailbox) Enqueue(msg Message) {
	m.ch <- msg
}

func (m *chanMailbox) Dequeue() (Message, bool) {
	select {
	case msg := <-m.ch:
		return msg, true
	default:
		return Message{}, false
	}
}

// Helper to create mailbox according to persistence availability.
func newMailboxFor(id PID, pers persistence.Persistence, size int) Mailbox {
	if pers != nil {
		return newPersistentMailbox(pers, persistence.PID(id), size)
	}
	return newChanMailbox(size)
}

func (m *chanMailbox) Drain() []Message {
	var out []Message
	for {
		select {
		case msg := <-m.ch:
			out = append(out, msg)
		default:
			return out
		}
	}
}

func (m *chanMailbox) Close() {}

// ActorRef is a local handle to actor runtime.
type ActorRef struct {
	ID      PID
	mail    Mailbox
	mu      sync.Mutex
	actor   Actor
	stopped chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	created time.Time
}

func (r *ActorRef) Enqueue(msg Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mail.Enqueue(msg)
}

// SnapshotMailbox drains and then restores the mailbox, returning a copy of current messages.
func (r *ActorRef) SnapshotMailbox() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	msgs := r.mail.Drain()
	// restore
	for _, m := range msgs {
		r.mail.Enqueue(m)
	}
	return msgs
}
