package orchestrator

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rickKoch/opsflow/actor"
	genpb "github.com/rickKoch/opsflow/grpc/gen"
	"github.com/rickKoch/opsflow/internal/grpcpool"
	"github.com/rickKoch/opsflow/logging"
	"github.com/rickKoch/opsflow/persistence"
	"github.com/rickKoch/opsflow/router"
	"github.com/rickKoch/opsflow/scheduler"
	"github.com/rickKoch/opsflow/tracing"
	"github.com/rickKoch/opsflow/workflow"
)

// Orchestrator coordinates actor lifecycles and workflows.
type Orchestrator struct {
	reg    *actor.Registry
	pers   persistence.Persistence
	log    logging.Logger
	tracer tracing.Tracer
	router *router.Router
	pool   *grpcpool.Pool
	sched  *scheduler.Scheduler
}

type Config struct {
	Registry    *actor.Registry
	Persistence persistence.Persistence
	Logger      logging.Logger
	Tracer      tracing.Tracer
}

func NewOrchestrator(cfg Config) *Orchestrator {
	return NewOrchestratorWithRouterConfig(cfg, grpcpool.PoolConfig{DialTimeout: 2 * time.Second, MaxConnsPerAddr: 1})
}

func NewOrchestratorWithRouterConfig(cfg Config, poolCfg grpcpool.PoolConfig) *Orchestrator {
	log := cfg.Logger
	if log == nil {
		log = logging.StdLogger{}
	}

	tracer := cfg.Tracer
	if tracer == nil {
		tracer = tracing.NoopTracer{}
	}

	r := router.NewRouterWithConfig(cfg.Registry, log, tracer, poolCfg)
	orch := &Orchestrator{reg: cfg.Registry, pers: cfg.Persistence, log: log, tracer: tracer, router: r, pool: grpcpool.NewWithConfig(poolCfg)}
	// initialize scheduler with invoke callback that starts workflows by id
	orch.sched = scheduler.NewScheduler(func(ctx context.Context, job scheduler.Job) error {
		// load workflow from persistence
		if orch.pers == nil {
			orch.log.Info("scheduler: no persistence available to load workflow", "workflow_id", job.WorkflowID)
			return nil
		}
		b, err := orch.pers.LoadSnapshot(context.Background(), persistence.PID("workflow_def:"+job.WorkflowID))
		if err != nil || len(b) == 0 {
			orch.log.Info("scheduler: workflow not found", "workflow_id", job.WorkflowID, "err", err)
			return nil
		}
		var wf workflow.Workflow
		if err := json.Unmarshal(b, &wf); err != nil {
			orch.log.Info("scheduler: failed to unmarshal workflow", "workflow_id", job.WorkflowID, "err", err)
			return err
		}
		// clear previous run state so scheduled runs execute each time
		_ = orch.pers.SaveSnapshot(context.Background(), persistence.PID("workflow:"+job.WorkflowID), nil)
		orch.log.Info("scheduler: triggering workflow", "workflow_id", job.WorkflowID)
		return orch.StartWorkflow(ctx, &wf)
	}, log)
	orch.sched.Start()
	// restore persisted schedules (best-effort)
	go func() {
		_ = orch.restoreSchedules(context.Background())
	}()
	return orch
}

// RegisterWorkflow persists a workflow definition and registers any cron
// schedule declared on it.
func (o *Orchestrator) RegisterWorkflow(ctx context.Context, wf *workflow.Workflow) error {
	if o.pers == nil {
		return nil
	}
	b, err := json.Marshal(wf)
	if err != nil {
		return err
	}
	if err := o.pers.SaveSnapshot(ctx, persistence.PID("workflow_def:"+wf.ID), b); err != nil {
		return err
	}
	// if workflow has a cron, persist schedule and register
	if wf.Cron != "" {
		job := scheduler.Job{ID: wf.ID, Type: "workflow", WorkflowID: wf.ID, CronExpression: wf.Cron, Enabled: true}
		if err := o.saveSchedule(ctx, job); err != nil {
			return err
		}
		if err := o.sched.AddOrUpdate(ctx, job); err != nil {
			return err
		}
	}
	return nil
}

// saveSchedule persists a schedule and updates the schedules index.
func (o *Orchestrator) saveSchedule(ctx context.Context, job scheduler.Job) error {
	if o.pers == nil {
		return nil
	}
	b, err := json.Marshal(job)
	if err != nil {
		return err
	}
	if err := o.pers.SaveSnapshot(ctx, persistence.PID("schedule:"+job.ID), b); err != nil {
		return err
	}
	// update index
	idxKey := persistence.PID("schedules:index")
	var ids []string
	data, _ := o.pers.LoadSnapshot(ctx, idxKey)
	if len(data) > 0 {
		_ = json.Unmarshal(data, &ids)
	}
	found := false
	for _, v := range ids {
		if v == job.ID {
			found = true
			break
		}
	}
	if !found {
		ids = append(ids, job.ID)
		nb, _ := json.Marshal(ids)
		_ = o.pers.SaveSnapshot(ctx, idxKey, nb)
	}
	return nil
}

// removeSchedule removes a persisted schedule and updates the index.
func (o *Orchestrator) removeSchedule(ctx context.Context, id string) error {
	if o.pers == nil {
		return nil
	}
	// remove snapshot
	_ = o.pers.SaveSnapshot(ctx, persistence.PID("schedule:"+id), nil)
	// update index
	idxKey := persistence.PID("schedules:index")
	var ids []string
	data, _ := o.pers.LoadSnapshot(ctx, idxKey)
	if len(data) > 0 {
		_ = json.Unmarshal(data, &ids)
	}
	var out []string
	for _, v := range ids {
		if v != id {
			out = append(out, v)
		}
	}
	nb, _ := json.Marshal(out)
	_ = o.pers.SaveSnapshot(ctx, idxKey, nb)
	return nil
}

// restoreSchedules loads persisted schedules and registers them with scheduler.
func (o *Orchestrator) restoreSchedules(ctx context.Context) error {
	if o.pers == nil {
		return nil
	}
	idxKey := persistence.PID("schedules:index")
	data, err := o.pers.LoadSnapshot(ctx, idxKey)
	if err != nil || len(data) == 0 {
		return nil
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return err
	}
	for _, id := range ids {
		b, err := o.pers.LoadSnapshot(ctx, persistence.PID("schedule:"+id))
		if err != nil || len(b) == 0 {
			continue
		}
		var job scheduler.Job
		if err := json.Unmarshal(b, &job); err != nil {
			o.log.Info("scheduler: failed to unmarshal job", "id", id, "err", err)
			continue
		}
		if job.Enabled {
			if err := o.sched.AddOrUpdate(ctx, job); err != nil {
				o.log.Info("scheduler: failed to add job", "id", id, "err", err)
			}
		}
	}
	return nil
}

// SpawnAndRegister spawns an actor and ensures persistence for it.
func (o *Orchestrator) SpawnAndRegister(ctx context.Context, id actor.PID, a actor.Actor, mailboxSize int) error {
	_, err := o.reg.Spawn(ctx, id, a, mailboxSize)
	if err != nil {
		return err
	}
	// save snapshot for new actor
	_ = o.reg.SaveSnapshot(ctx, id)
	return nil
}

// Send sends a message to an actor using the router (prefer local, fallback to remote).
func (o *Orchestrator) Send(ctx context.Context, id actor.PID, msg actor.Message) error {
	if o.router != nil {
		return o.router.Send(ctx, id, msg)
	}
	return o.reg.Send(ctx, id, msg)
}

// StartPropagation begins periodically sending our local actors to provided peer addresses.
// peers is a list of grpc addresses like "host:port". interval controls how often to push updates.
func (o *Orchestrator) StartPropagation(ctx context.Context, peers []string, interval time.Duration) {
	if interval <= 0 || len(peers) == 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// gather local actors
				locals := o.reg.ListLocalActors()
				// build ShareActorsRequest
				var req genpb.RegisterRequest
				for _, a := range locals {
					req.Actors = append(req.Actors, &genpb.ActorInfo{Name: string(a.ID), Address: a.Address})
				}
				// push to each peer with retries/backoff and pooled connections
				for _, p := range peers {
					const attempts = 3
					base := 200 * time.Millisecond
					var lastErr error
					for i := 0; i < attempts; i++ {
						conn, err := o.pool.GetConn(ctx, p)
						if err != nil {
							lastErr = err
							o.log.Info("propagate: failed to get connection", "peer", p, "err", err, "attempt", i+1)
						} else {
							client := genpb.NewActorServiceClient(conn)
							// per-call timeout
							callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
							_, err = client.Register(callCtx, &req)
							cancel()
							if err == nil {
								o.log.Info("propagate: success", "peer", p)
								lastErr = nil
								break
							}
							lastErr = err
							o.log.Info("propagate: share failed", "peer", p, "err", err, "attempt", i+1)
						}
						// backoff with jitter
						jitter := time.Duration(time.Now().UnixNano()%100) * time.Millisecond
						sleep := base<<uint(i) + jitter
						time.Sleep(sleep)
					}
					if lastErr != nil {
						o.log.Info("propagate: giving up after retries", "peer", p, "err", lastErr)
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// StartRegisterPropagation periodically calls ActorService.Register on peers to announce
// this node (serviceID/address) and its local actors. This provides peers with both
// a dialable address and the list of actor names hosted here.
func (o *Orchestrator) StartRegisterPropagation(ctx context.Context, peers []string, serviceID string, address string, interval time.Duration) {
	if interval <= 0 || len(peers) == 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				locals := o.reg.ListLocalActors()
				var req genpb.RegisterRequest
				req.ServiceId = serviceID
				req.Address = address
				for _, a := range locals {
					req.Actors = append(req.Actors, &genpb.ActorInfo{Name: string(a.ID), Address: a.Address})
				}
				for _, p := range peers {
					const attempts = 3
					base := 200 * time.Millisecond
					var lastErr error
					for i := 0; i < attempts; i++ {
						conn, err := o.pool.GetConn(ctx, p)
						if err != nil {
							lastErr = err
							o.log.Info("register-propagate: failed to get connection", "peer", p, "err", err, "attempt", i+1)
						} else {
							client := genpb.NewActorServiceClient(conn)
							callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
							_, err = client.Register(callCtx, &req)
							cancel()
							if err == nil {
								o.log.Info("register-propagate: success", "peer", p)
								lastErr = nil
								break
							}
							lastErr = err
							o.log.Info("register-propagate: register failed", "peer", p, "err", err, "attempt", i+1)
						}
						jitter := time.Duration(time.Now().UnixNano()%100) * time.Millisecond
						sleep := base<<uint(i) + jitter
						time.Sleep(sleep)
					}
					if lastErr != nil {
						o.log.Info("register-propagate: giving up after retries", "peer", p, "err", lastErr)
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Shutdown gracefully stops orchestrator background resources, including
// the router's grpc pool. It is safe to call multiple times.
func (o *Orchestrator) Shutdown(ctx context.Context) error {
	// stop propagation/pruner loops should be controlled by the contexts passed
	// into StartPropagation / StartRemotePruner. Here we close pooled conns.
	if o.pool != nil {
		o.pool.CloseAll()
	}
	if o.router != nil {
		o.router.Close()
	}
	return nil
}

// StartWorkflow uses the orchestrator's routing (local preferring, remote fallback)
// to execute the provided workflow. Execution runs asynchronously and progress is
// persisted using the orchestrator's persistence backend.
func (o *Orchestrator) StartWorkflow(ctx context.Context, wf *workflow.Workflow) error {
	mgr := workflow.NewManager(o.pers, func(ctx context.Context, id actor.PID, msg actor.Message) error {
		return o.Send(ctx, id, msg)
	}, o.log, o.tracer)
	// auto-select parallel scheduler when any step has explicit dependencies
	for _, s := range wf.Steps {
		if len(s.Depends) > 0 {
			return mgr.StartParallel(ctx, wf)
		}
	}
	return mgr.Start(ctx, wf)
}

// StartWorkflowByID loads a workflow by id from persistence and starts it.
func (o *Orchestrator) StartWorkflowByID(ctx context.Context, id string) error {
	if o.pers == nil {
		return nil
	}
	b, err := o.pers.LoadSnapshot(ctx, persistence.PID("workflow_def:"+id))
	if err != nil || len(b) == 0 {
		o.log.Info("start-workflow: definition not found", "id", id, "err", err)
		return nil
	}
	var wf workflow.Workflow
	if err := json.Unmarshal(b, &wf); err != nil {
		o.log.Info("start-workflow: invalid workflow data", "id", id, "err", err)
		return err
	}
	return o.StartWorkflow(ctx, &wf)
}

// StartRemotePruner starts the registry remote pruner with given ttl and check interval.
func (o *Orchestrator) StartRemotePruner(ctx context.Context, ttl time.Duration, interval time.Duration) {
	o.reg.StartRemotePruner(ctx, ttl, interval)
}

// HandleRegister handles an incoming RegisterRequest from a peer and updates the
// local registry with the provided remote actor information.
func (o *Orchestrator) HandleRegister(ctx context.Context, req *genpb.RegisterRequest) (*genpb.RegisterResponse, error) {
	var remoteActors []actor.RemoteActorRef
	now := time.Now()
	for _, a := range req.GetActors() {
		remoteActors = append(remoteActors, actor.RemoteActorRef{ID: actor.PID(a.GetName()), Address: req.GetAddress(), LastSeen: now})
	}
	o.reg.UpdateRemoteActors(ctx, remoteActors)
	return &genpb.RegisterResponse{Success: true}, nil
}
