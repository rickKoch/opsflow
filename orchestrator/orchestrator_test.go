package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/rickKoch/opsflow/actor"
	"github.com/rickKoch/opsflow/internal/grpcpool"
	"github.com/rickKoch/opsflow/logging"
	"github.com/rickKoch/opsflow/persistence"
)

type testActor struct {
	started chan struct{}
	stopped chan struct{}
	recv    chan actor.Message
}

func newTestActor() *testActor {
	return &testActor{started: make(chan struct{}, 1), stopped: make(chan struct{}, 1), recv: make(chan actor.Message, 4)}
}

func (t *testActor) Started(ctx context.Context)                    { t.started <- struct{}{} }
func (t *testActor) Stopped(ctx context.Context)                    { t.stopped <- struct{}{} }
func (t *testActor) Receive(ctx context.Context, msg actor.Message) { t.recv <- msg }

func TestOrchestratorRouterIntegration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := persistence.NewFileStore(dir)
	reg := actor.NewRegistry(store, logging.StdLogger{}, nil)

	// create orchestrator with router config (short dial timeout to avoid stalls)
	orch := NewOrchestratorWithRouterConfig(Config{Registry: reg, Persistence: store, Logger: logging.StdLogger{}, Tracer: nil}, grpcpool.PoolConfig{DialTimeout: 500 * time.Millisecond, MaxConnsPerAddr: 1})

	// spawn a local actor and ensure Send via orchestrator uses router -> local
	ta := newTestActor()
	_, err := reg.Spawn(ctx, actor.PID("local1"), ta, 4)
	if err != nil {
		t.Fatalf("spawn local actor: %v", err)
	}

	// send via orchestrator Send (which should use router)
	if err := orch.Send(ctx, actor.PID("local1"), actor.Message{Payload: []byte("hi")}); err != nil {
		t.Fatalf("orch send failed: %v", err)
	}

	select {
	case m := <-ta.recv:
		if string(m.Payload) != "hi" {
			t.Fatalf("unexpected payload: %s", string(m.Payload))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("message not received by local actor")
	}
}
