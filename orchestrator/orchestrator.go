package orchestrator

import (
	"context"
	"time"

	"github.com/rickKoch/opsflow/actor"
	genpb "github.com/rickKoch/opsflow/grpc/gen"
	"github.com/rickKoch/opsflow/internal/grpcpool"
	"github.com/rickKoch/opsflow/logging"
	"github.com/rickKoch/opsflow/persistence"
	"github.com/rickKoch/opsflow/router"
	"github.com/rickKoch/opsflow/tracing"
)

// Orchestrator coordinates actor lifecycles and workflows.
type Orchestrator struct {
	reg    *actor.Registry
	pers   persistence.Persistence
	log    logging.Logger
	tracer tracing.Tracer
	router *router.Router
	pool   *grpcpool.Pool
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
	return &Orchestrator{
		reg: cfg.Registry, pers: cfg.Persistence, log: log, tracer: tracer, router: r, pool: grpcpool.NewWithConfig(poolCfg),
	}
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
				var req genpb.ShareActorsRequest
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
							client := genpb.NewActorRegistryClient(conn)
							// per-call timeout
							callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
							_, err = client.ShareActors(callCtx, &req)
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

// HandleShareActors handles ActorRegistry.ShareActors calls from peers.
func (o *Orchestrator) HandleShareActors(ctx context.Context, req *genpb.ShareActorsRequest) (*genpb.ShareActorsResponse, error) {
	var remoteActors []actor.RemoteActorRef
	now := time.Now()
	for _, a := range req.GetActors() {
		remoteActors = append(remoteActors, actor.RemoteActorRef{ID: actor.PID(a.GetName()), Address: a.GetAddress(), LastSeen: now})
	}
	o.reg.UpdateRemoteActors(ctx, remoteActors)
	return &genpb.ShareActorsResponse{Success: true}, nil
}
