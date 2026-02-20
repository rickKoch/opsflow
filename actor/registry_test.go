package actor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rickKoch/opsflow/logging"
	"github.com/rickKoch/opsflow/persistence"
)

type testActor struct {
	started chan struct{}
	stopped chan struct{}
	recv    chan Message
}

func newTestActor() *testActor {
	return &testActor{started: make(chan struct{}, 1), stopped: make(chan struct{}, 1), recv: make(chan Message, 4)}
}

func (t *testActor) Started(ctx context.Context)              { t.started <- struct{}{} }
func (t *testActor) Stopped(ctx context.Context)              { t.stopped <- struct{}{} }
func (t *testActor) Receive(ctx context.Context, msg Message) { t.recv <- msg }

func TestLifecycleHooks(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := persistence.NewFileStore(dir)
	reg := NewRegistry(store, logging.StdLogger{}, nil)

	ta := newTestActor()
	ref, err := reg.Spawn(ctx, PID("actor1"), ta, 8)
	if err != nil {
		t.Fatalf("spawn error: %v", err)
	}
	// wait for started
	select {
	case <-ta.started:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("started not called")
	}

	// send a message
	_ = reg.Send(ctx, ref.ID, Message{Payload: []byte("hello")})
	select {
	case m := <-ta.recv:
		if string(m.Payload) != "hello" {
			t.Fatalf("unexpected payload: %s", string(m.Payload))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("message not received")
	}

	// stop actor
	ref.cancel()
	select {
	case <-ta.stopped:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("stopped not called")
	}
}

func TestPersistenceMailboxSnapshot(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := persistence.NewFileStore(dir)
	reg := NewRegistry(store, logging.StdLogger{}, nil)

	// create persistent mailbox directly and enqueue messages
	pm := newPersistentMailbox(store, persistence.PID("actor2"), 4)
	pm.Enqueue(Message{Payload: []byte("m1")})
	pm.Enqueue(Message{Payload: []byte("m2")})

	// allow a brief moment for persistence to complete
	time.Sleep(50 * time.Millisecond)

	// ensure file exists
	p := filepath.Join(dir, "actor2.mailbox.json")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("mailbox file missing: %v", err)
	}

	// load via store
	msgs, err := store.LoadMailbox(ctx, persistence.PID("actor2"))
	if err != nil {
		t.Fatalf("load mailbox error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}
