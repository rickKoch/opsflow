package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rickKoch/opsflow/actor"
	"github.com/rickKoch/opsflow/persistence"
)

// Test workflow retry and recovery across manager restart.
func TestWorkflowRetryAndRecovery(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := persistence.NewFileStore(dir)

	// router that fails for the first N attempts, then succeeds
	var mu sync.Mutex
	attempts := 0
	failUntil := 2
	routerFailThenSucceed := func(ctx context.Context, pid actor.PID, msg actor.Message) error {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts <= failUntil {
			return errors.New("transient")
		}
		return nil
	}

	// workflow with one step and several retries
	wf := &Workflow{ID: "w1", Steps: []Step{{ID: "s1", Target: actor.PID("a1"), Message: actor.Message{Payload: []byte("x")}, MaxRetries: 5, Backoff: 10 * time.Millisecond}}}

	// start manager with failing router, then cancel quickly to simulate crash after some attempts
	m1 := NewManager(store, func(ctx context.Context, p actor.PID, m actor.Message) error { return errors.New("always") }, nil, nil)
	cctx1, cancel1 := context.WithCancel(ctx)
	_ = m1.Start(cctx1, wf)
	// let it attempt once
	time.Sleep(30 * time.Millisecond)
	cancel1()

	// now start manager with router that will succeed eventually
	m2 := NewManager(store, routerFailThenSucceed, nil, nil)
	// start and wait for completion
	_ = m2.Start(ctx, wf)

	// poll persisted state until completed
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := store.LoadSnapshot(ctx, persistence.PID("workflow:w1"))
		if b != nil && len(b) > 0 {
			var st workflowState
			if err := json.Unmarshal(b, &st); err == nil {
				if st.Completed {
					return
				}
				if st.Failed {
					t.Fatalf("workflow failed unexpectedly")
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("workflow did not complete in time")
}
