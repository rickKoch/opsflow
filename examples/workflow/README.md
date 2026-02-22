Workflow Example
================

This example demonstrates a small workflow with parallel steps and dependencies.

How to run
----------

Run the example program from the repository root:

```sh
go run ./examples/workflow
```

What it shows
------------

- Spawns three local actors: `printer1`, `printer2`, `printer3`.
- Steps `a` and `b` run in parallel; step `c` depends on both (`Depends: ["a","b"]`) and runs after they complete.
- The `Orchestrator.StartWorkflow` call auto-selects the parallel scheduler when it sees dependencies on steps.
- Workflow progress is persisted under `./data_workflow` so runs can recover after restarts.

Notes
-----

- The `workflow.Step` type supports `Depends []string` and per-step retry/backoff settings.
- Use `Orchestrator.StartWorkflow` to start workflows; the orchestrator will pick sequential or parallel execution automatically.
