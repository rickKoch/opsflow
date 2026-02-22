package main

import (
	"context"
	// "encoding/json" not used in this example but useful for extensions
	"fmt"
	"time"

	"github.com/rickKoch/opsflow/actor"
	"github.com/rickKoch/opsflow/logging"
	"github.com/rickKoch/opsflow/orchestrator"
	"github.com/rickKoch/opsflow/persistence"
	"github.com/rickKoch/opsflow/scheduler"
	"github.com/rickKoch/opsflow/workflow"
)

// PrintActor prints payloads with an id.
type PrintActor struct{ Prefix string }

func (p PrintActor) Receive(ctx context.Context, msg actor.Message) {
	fmt.Printf("%s %s\n", p.Prefix, string(msg.Payload))
}

func main() {
	ctx := context.Background()
	store := persistence.NewFileStore("./data_scheduled_workflow")
	reg := actor.NewRegistry(store, logging.StdLogger{}, nil)
	orch := orchestrator.NewOrchestrator(orchestrator.Config{Registry: reg, Persistence: store, Logger: logging.StdLogger{}, Tracer: nil})

	// spawn two actors which will be triggered at different schedules
	_ = orch.SpawnAndRegister(ctx, actor.PID("printer_fast"), PrintActor{Prefix: "fast:"}, 8)
	_ = orch.SpawnAndRegister(ctx, actor.PID("printer_slow"), PrintActor{Prefix: "slow:"}, 8)

	// create a workflow with two independent steps, but we will also
	// schedule the individual actors separately to demonstrate mixed scheduling.
	wf := &workflow.Workflow{
		ID: "mixed_sched_wf",
		Steps: []workflow.Step{
			{ID: "s1", Target: actor.PID("printer_fast"), Message: actor.Message{Payload: []byte("from workflow step 1")}, MaxRetries: 1, Backoff: 10 * time.Millisecond, Cron: "@every 3s"},
			{ID: "s2", Target: actor.PID("printer_slow"), Message: actor.Message{Payload: []byte("from workflow step 2")}, MaxRetries: 1, Backoff: 10 * time.Millisecond, Cron: "@every 7s"},
		},
		// no top-level cron for the workflow itself
	}

	if err := orch.RegisterWorkflow(ctx, wf); err != nil {
		fmt.Println("failed to register workflow:", err)
		return
	}

	// schedule per-step cron from workflow definition (if present)
	for _, s := range wf.Steps {
		if s.Cron != "" {
			job := scheduler.Job{ID: wf.ID + ":step:" + s.ID, Type: "workflow_step", WorkflowID: wf.ID, StepID: s.ID, CronExpression: s.Cron, Enabled: true}
			if err := orch.Scheduler().AddOrUpdate(context.Background(), job); err != nil {
				fmt.Println("failed to schedule step:", err)
			}
		}
	}

	// schedule the workflow itself every 15s (optional)
	j1 := scheduler.Job{ID: wf.ID + "_wfjob", Type: "workflow", WorkflowID: wf.ID, CronExpression: "@every 15s", Enabled: true}
	_ = orch.Scheduler().AddOrUpdate(context.Background(), j1)

	fmt.Println("scheduled workflow and actors; running for 20s...")
	time.Sleep(20 * time.Second)
	fmt.Println("shutting down")
}
