package workflow

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rickKoch/opsflow/actor"
	"github.com/rickKoch/opsflow/persistence"
)

// Test that independent steps run in parallel (their start times overlap).
func TestParallelStepsRunConcurrently(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := persistence.NewFileStore(dir)
	// Use channels to deterministically assert both steps started before either finished.
	startedCh := make(chan string, 2)
	proceedCh := make(chan struct{})

	// router records start then waits until proceedCh is closed to finish.
	router := func(ctx context.Context, pid actor.PID, msg actor.Message) error {
		startedCh <- string(pid)
		select {
		case <-proceedCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	mgr := NewManager(store, router, nil, nil)

	wf := &Workflow{
		ID: "parallel_test",
		Steps: []Step{
			{ID: "a", Target: actor.PID("a"), Message: actor.Message{Payload: []byte("1")}, MaxRetries: 1, Backoff: 5 * time.Millisecond},
			{ID: "b", Target: actor.PID("b"), Message: actor.Message{Payload: []byte("2")}, MaxRetries: 1, Backoff: 5 * time.Millisecond},
		},
	}

	// start parallel scheduler
	_ = mgr.StartParallel(ctx, wf)

	// wait for both starts to be observed
	got := map[string]bool{}
	timeout := time.After(1 * time.Second)
	for len(got) < 2 {
		select {
		case id := <-startedCh:
			got[id] = true
		case <-timeout:
			t.Fatalf("timed out waiting for both steps to start, got=%v", got)
		}
	}

	// both started; allow them to finish
	close(proceedCh)

	// wait for workflow to persist completion
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := store.LoadSnapshot(ctx, persistence.PID("workflow:parallel_test"))
		if b != nil && len(b) > 0 {
			var st workflowState
			if err := json.Unmarshal(b, &st); err == nil {
				if st.Completed {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("workflow did not complete in time")
}

// Test that dependencies are honored: step c should run after a and b complete.
func TestDependenciesEnforced(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := persistence.NewFileStore(dir)

	// Use channels to make ordering deterministic: a and b will signal when done;
	// c will signal when it starts. Test asserts c started after both a and b signaled done.
	doneA := make(chan struct{})
	doneB := make(chan struct{})
	startedC := make(chan struct{}, 1)

	router := func(ctx context.Context, pid actor.PID, msg actor.Message) error {
		id := string(pid)
		switch id {
		case "a":
			// simulate work then signal done
			close(doneA)
			return nil
		case "b":
			close(doneB)
			return nil
		case "c":
			// record that c started
			startedC <- struct{}{}
			return nil
		default:
			return nil
		}
	}

	mgr := NewManager(store, router, nil, nil)

	wf := &Workflow{
		ID: "deps_test",
		Steps: []Step{
			{ID: "a", Target: actor.PID("a"), Message: actor.Message{Payload: []byte("a")}, MaxRetries: 1, Backoff: 1 * time.Millisecond},
			{ID: "b", Target: actor.PID("b"), Message: actor.Message{Payload: []byte("b")}, MaxRetries: 1, Backoff: 1 * time.Millisecond},
			{ID: "c", Target: actor.PID("c"), Message: actor.Message{Payload: []byte("c")}, Depends: []string{"a", "b"}, MaxRetries: 1, Backoff: 1 * time.Millisecond},
		},
	}

	_ = mgr.StartParallel(ctx, wf)

	// wait until c starts or timeout
	timeout := time.After(2 * time.Second)
	select {
	case <-startedC:
		// ensure both done signals occurred before c started
		select {
		case <-doneA:
		default:
			t.Fatalf("c started before a completed")
		}
		select {
		case <-doneB:
		default:
			t.Fatalf("c started before b completed")
		}
	case <-timeout:
		t.Fatalf("timeout waiting for c to start")
	}
}
