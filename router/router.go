package router

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/rickKoch/opsflow/actor"
	genpb "github.com/rickKoch/opsflow/grpc/gen"
	"github.com/rickKoch/opsflow/internal/grpcpool"
	"github.com/rickKoch/opsflow/logging"
	"github.com/rickKoch/opsflow/tracing"
)

type Router struct {
	reg    *actor.Registry
	log    logging.Logger
	tracer tracing.Tracer
	pool   *grpcpool.Pool
}

// NewRouter creates a router with a default pool configuration.
func NewRouter(reg *actor.Registry, log logging.Logger, tracer tracing.Tracer) *Router {
	return NewRouterWithConfig(reg, log, tracer, grpcpool.PoolConfig{DialTimeout: 2 * time.Second, MaxConnsPerAddr: 1})
}

// NewRouterWithConfig creates a router and configures its gRPC connection pool.
func NewRouterWithConfig(reg *actor.Registry, log logging.Logger, tracer tracing.Tracer, cfg grpcpool.PoolConfig) *Router {
	if log == nil {
		log = logging.StdLogger{}
	}
	if tracer == nil {
		tracer = tracing.NoopTracer{}
	}
	return &Router{reg: reg, log: log, tracer: tracer, pool: grpcpool.NewWithConfig(cfg)}
}

// NewRouterWithPool creates a Router that uses an existing grpcpool.Pool instance.
func NewRouterWithPool(reg *actor.Registry, log logging.Logger, tracer tracing.Tracer, pool *grpcpool.Pool) *Router {
	if log == nil {
		log = logging.StdLogger{}
	}
	if tracer == nil {
		tracer = tracing.NoopTracer{}
	}
	if pool == nil {
		pool = grpcpool.New()
	}
	return &Router{reg: reg, log: log, tracer: tracer, pool: pool}
}

// Close shuts down resources used by the Router (closes its grpc pool).
func (r *Router) Close() {
	if r == nil {
		return
	}
	if r.pool != nil {
		r.pool.CloseAll()
	}
}

func (r *Router) Send(ctx context.Context, pid actor.PID, msg actor.Message) error {
	// try local first
	if err := r.reg.Send(ctx, pid, msg); err == nil {
		return nil
	}
	// if not found locally, try remote
	ra, ok := r.reg.GetRemote(pid)
	if !ok {
		return actor.ErrNotFound
	}
	// dial remote address and forward, with retries/backoff
	if ra.Address == "" {
		return actor.ErrNotFound
	}
	const attempts = 3
	base := 200 * time.Millisecond
	var lastErr error
	for i := 0; i < attempts; i++ {
		// get pooled connection (dial if needed)
		conn, err := r.pool.GetConn(ctx, ra.Address)
		if err != nil {
			lastErr = err
			r.log.Info("failed to get pooled conn", "addr", ra.Address, "err", err, "attempt", i+1)
		} else {
			client := genpb.NewActorServiceClient(conn)
			_, err = client.Send(ctx, &genpb.ActorMessage{Pid: string(pid), Payload: msg.Payload, Typ: msg.Type})
			if err == nil {
				return nil
			}
			lastErr = err
			r.log.Info("remote send failed", "addr", ra.Address, "err", err, "attempt", i+1)
		}
		// backoff with jitter
		jitter := time.Duration(rand.Int63n(100)) * time.Millisecond
		sleep := base<<uint(i) + jitter
		time.Sleep(sleep)
	}
	return fmt.Errorf("remote send failed after retries: %w", lastErr)
}
