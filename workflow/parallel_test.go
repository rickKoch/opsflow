package workflow

import (
	"context"
	"encoding/json"
	"sync"
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

	var mu sync.Mutex
	starts := map[string]time.Time{}

	// router sleeps for a duration after recording start time
	router := func(ctx context.Context, pid actor.PID, msg actor.Message) error {
		mu.Lock()
		starts[string(pid)] = time.Now()
		mu.Unlock()
		// simulate work
		select {
		case <-time.After(200 * time.Millisecond):
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

	// wait for completion
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := store.LoadSnapshot(ctx, persistence.PID("workflow:parallel_test"))
		if b != nil && len(b) > 0 {
			var st workflowState
			if err := json.Unmarshal(b, &st); err == nil {
				if st.Completed {
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if starts["a"].IsZero() || starts["b"].IsZero() {
		t.Fatalf("both steps should have started: starts=%v", starts)
	}
	diff := starts["a"].Sub(starts["b"]).Seconds()
	if diff < 0 {
		diff = -diff
	}
	// both should start within 100ms of each other when run in parallel
	if diff > 0.1 {
		t.Fatalf("steps did not start concurrently enough: diff=%v seconds", diff)
	}
}

// Test that dependencies are honored: step c should run after a and b complete.
func TestDependenciesEnforced(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := persistence.NewFileStore(dir)

	var mu sync.Mutex
	starts := map[string]time.Time{}

	// router records invocation time
	router := func(ctx context.Context, pid actor.PID, msg actor.Message) error {
		mu.Lock()
		starts[string(pid)] = time.Now()
		mu.Unlock()
		return nil
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

	// wait for completion
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := store.LoadSnapshot(ctx, persistence.PID("workflow:deps_test"))
		if b != nil && len(b) > 0 {
			var st workflowState
			if err := json.Unmarshal(b, &st); err == nil {
				if st.Completed {
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if starts["c"].IsZero() || starts["a"].IsZero() || starts["b"].IsZero() {
		t.Fatalf("expected all steps to have started: %v", starts)
	}
	// c must start after both a and b
	if !starts["c"].After(starts["a"]) || !starts["c"].After(starts["b"]) {
		t.Fatalf("dependency ordering violated: starts=%v", starts)
	}
}
