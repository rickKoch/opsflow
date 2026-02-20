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

// Test workflow marks failed when retries exceeded.
func TestWorkflowExceedMaxRetries(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := persistence.NewFileStore(dir)

	// router that always fails
	alwaysFail := func(ctx context.Context, pid actor.PID, msg actor.Message) error {
		return errors.New("permanent")
	}

	wf := &Workflow{ID: "wf_fail", Steps: []Step{{ID: "s1", Target: actor.PID("a1"), Message: actor.Message{Payload: []byte("x")}, MaxRetries: 2, Backoff: 5 * time.Millisecond}}}
	m := NewManager(store, alwaysFail, nil, nil)
	_ = m.Start(ctx, wf)

	// poll persisted state until failed
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := store.LoadSnapshot(ctx, persistence.PID("workflow:wf_fail"))
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
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("workflow did not fail in time")
}

// Test concurrently running multiple workflows.
func TestConcurrentWorkflows(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := persistence.NewFileStore(dir)

	// simple router that always succeeds
	okRouter := func(ctx context.Context, pid actor.PID, msg actor.Message) error { return nil }

	m := NewManager(store, okRouter, nil, nil)

	wf1 := &Workflow{ID: "concurrent1", Steps: []Step{{ID: "a", Target: actor.PID("x"), Message: actor.Message{Payload: []byte("1")}, MaxRetries: 1, Backoff: 1 * time.Millisecond}}}
	wf2 := &Workflow{ID: "concurrent2", Steps: []Step{{ID: "b", Target: actor.PID("y"), Message: actor.Message{Payload: []byte("2")}, MaxRetries: 1, Backoff: 1 * time.Millisecond}}}

	// start both
	_ = m.Start(ctx, wf1)
	_ = m.Start(ctx, wf2)

	// wait for both to complete
	deadline := time.Now().Add(1 * time.Second)
	done1, done2 := false, false
	for time.Now().Before(deadline) {
		if !done1 {
			b, _ := store.LoadSnapshot(ctx, persistence.PID("workflow:concurrent1"))
			if b != nil && len(b) > 0 {
				var st workflowState
				_ = json.Unmarshal(b, &st)
				if st.Completed {
					done1 = true
				}
			}
		}
		if !done2 {
			b, _ := store.LoadSnapshot(ctx, persistence.PID("workflow:concurrent2"))
			if b != nil && len(b) > 0 {
				var st workflowState
				_ = json.Unmarshal(b, &st)
				if st.Completed {
					done2 = true
				}
			}
		}
		if done1 && done2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("concurrent workflows did not complete in time")
}
