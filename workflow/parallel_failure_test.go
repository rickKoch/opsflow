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

// Test that if a step exhausts its retries the entire workflow is marked failed.
func TestParallelStepExhaustsRetriesMarksWorkflowFailed(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := persistence.NewFileStore(dir)

	// router: step "bad" always fails; others succeed
	router := func(ctx context.Context, pid actor.PID, msg actor.Message) error {
		if string(pid) == "bad" {
			return errors.New("permanent-failure")
		}
		return nil
	}

	mgr := NewManager(store, router, nil, nil)

	wf := &Workflow{
		ID: "parallel_failure",
		Steps: []Step{
			{ID: "good", Target: actor.PID("good"), Message: actor.Message{Payload: []byte("ok")}, MaxRetries: 1, Backoff: 1 * time.Millisecond},
			{ID: "bad", Target: actor.PID("bad"), Message: actor.Message{Payload: []byte("fail")}, MaxRetries: 2, Backoff: 1 * time.Millisecond},
		},
	}

	_ = mgr.StartParallel(ctx, wf)

	// poll persisted state until failed
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := store.LoadSnapshot(ctx, persistence.PID("workflow:parallel_failure"))
		if b != nil && len(b) > 0 {
			var st workflowState
			if err := json.Unmarshal(b, &st); err == nil {
				if st.Failed {
					return
				}
				if st.Completed {
					t.Fatalf("workflow unexpectedly completed")
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("workflow did not fail in time")
}

// Test that a flaky step which fails initially but then succeeds within retries
// allows the workflow to complete when running in parallel with other steps.
func TestParallelFlakyStepRetriesAndSucceeds(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := persistence.NewFileStore(dir)

	var mu sync.Mutex
	attempts := map[string]int{}

	// router: "flaky" fails twice then succeeds; others succeed immediately
	router := func(ctx context.Context, pid actor.PID, msg actor.Message) error {
		id := string(pid)
		if id == "flaky" {
			mu.Lock()
			attempts[id]++
			a := attempts[id]
			mu.Unlock()
			if a <= 2 {
				return errors.New("transient")
			}
			return nil
		}
		return nil
	}

	mgr := NewManager(store, router, nil, nil)

	wf := &Workflow{
		ID: "parallel_flaky",
		Steps: []Step{
			{ID: "flaky", Target: actor.PID("flaky"), Message: actor.Message{Payload: []byte("f")}, MaxRetries: 5, Backoff: 1 * time.Millisecond},
			{ID: "other", Target: actor.PID("other"), Message: actor.Message{Payload: []byte("o")}, MaxRetries: 1, Backoff: 1 * time.Millisecond},
		},
	}

	_ = mgr.StartParallel(ctx, wf)

	// poll until completed
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := store.LoadSnapshot(ctx, persistence.PID("workflow:parallel_flaky"))
		if b != nil && len(b) > 0 {
			var st workflowState
			if err := json.Unmarshal(b, &st); err == nil {
				if st.Completed {
					return
				}
				if st.Failed {
					t.Fatalf("workflow failed unexpectedly: last_err=%s", st.LastError)
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("workflow did not complete in time")
}
