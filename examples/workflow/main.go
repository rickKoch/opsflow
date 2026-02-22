package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rickKoch/opsflow/actor"
	"github.com/rickKoch/opsflow/logging"
	"github.com/rickKoch/opsflow/orchestrator"
	"github.com/rickKoch/opsflow/persistence"
	"github.com/rickKoch/opsflow/workflow"
)

// PrinterActor prints received payloads with a prefix.
type PrinterActor struct{ Prefix string }

func (p PrinterActor) Receive(ctx context.Context, msg actor.Message) {
	fmt.Printf("%s%s\n", p.Prefix, string(msg.Payload))
}

func main() {
	ctx := context.Background()

	// simple file-backed persistence for the example
	store := persistence.NewFileStore("./data_workflow")

	// registry and orchestrator
	reg := actor.NewRegistry(store, logging.StdLogger{}, nil)
	orch := orchestrator.NewOrchestrator(orchestrator.Config{Registry: reg, Persistence: store, Logger: logging.StdLogger{}, Tracer: nil})

	// spawn a few local actors to demonstrate parallel steps
	_ = orch.SpawnAndRegister(ctx, actor.PID("printer1"), PrinterActor{Prefix: "p1: "}, 16)
	_ = orch.SpawnAndRegister(ctx, actor.PID("printer2"), PrinterActor{Prefix: "p2: "}, 16)
	_ = orch.SpawnAndRegister(ctx, actor.PID("printer3"), PrinterActor{Prefix: "final: "}, 16)

	// build a workflow where step3 depends on step1 and step2. step1 and step2
	// can run in parallel; step3 waits for both to complete.
	wf := &workflow.Workflow{
		ID: "example_wf",
		Steps: []workflow.Step{
			{ID: "a", Target: actor.PID("printer1"), Message: actor.Message{Payload: []byte("hello from A")}, MaxRetries: 1, Backoff: 10 * time.Millisecond},
			{ID: "b", Target: actor.PID("printer2"), Message: actor.Message{Payload: []byte("hello from B")}, MaxRetries: 1, Backoff: 10 * time.Millisecond},
			{ID: "c", Target: actor.PID("printer3"), Message: actor.Message{Payload: []byte("A and B completed")}, Depends: []string{"a", "b"}, MaxRetries: 1, Backoff: 10 * time.Millisecond},
		},
		// schedule this workflow to run every 10 seconds for the example
		Cron: "@every 10s",
	}

	// persist + register the workflow (this also registers the scheduler)
	if err := orch.RegisterWorkflow(ctx, wf); err != nil {
		fmt.Println("failed to register workflow:", err)
		return
	}

	// wait for workflow to complete by polling persisted state
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := store.LoadSnapshot(ctx, persistence.PID("workflow:example_wf"))
		if b != nil && len(b) > 0 {
			var st struct {
				Completed bool `json:"completed"`
			}
			if err := json.Unmarshal(b, &st); err == nil {
				if st.Completed {
					fmt.Println("workflow completed")
					return
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Println("workflow did not complete in time")
}
