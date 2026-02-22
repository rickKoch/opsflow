package grpcpool

import (
	"context"
	"sync"
	"time"

	"google.golang.org/grpc"
)

// Pool manages gRPC client connections by remote address and reuses them.
type PoolConfig struct {
	// DialTimeout is the per-dial timeout used when no deadline is present on ctx.
	DialTimeout time.Duration
	// IdleTimeout is reserved for future use (closing idle connections).
	IdleTimeout time.Duration
	// MaxConnsPerAddr reserved for future implementation
	MaxConnsPerAddr int
}

type connEntry struct {
	c        *grpc.ClientConn
	lastUsed time.Time
}

type Pool struct {
	mu    sync.Mutex
	conns map[string][]connEntry
	// idx provides a simple round-robin index per address
	idx map[string]int
	// inProgress tracks number of dials currently in progress per address
	inProgress map[string]int
	stop       chan struct{}
	closeOnce  sync.Once
	cfg        PoolConfig
}

// New creates a new Pool with default configuration.
func New() *Pool {
	return NewWithConfig(PoolConfig{DialTimeout: 2 * time.Second})
}

// NewWithConfig creates a new Pool using provided configuration.
func NewWithConfig(cfg PoolConfig) *Pool {
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 2 * time.Second
	}
	p := &Pool{conns: make(map[string][]connEntry), idx: make(map[string]int), inProgress: make(map[string]int), cfg: cfg, stop: make(chan struct{})}
	// start reaper if idle timeout configured
	if cfg.IdleTimeout > 0 {
		interval := cfg.IdleTimeout / 2
		if interval < time.Second {
			interval = time.Second
		}
		go p.startReaper(interval)
	}
	return p
}

// GetConn returns a shared grpc.ClientConn for the provided address. It will dial if necessary.
// ctx controls the dial timeout; callers should provide a context with timeout if desired.
func (p *Pool) GetConn(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	// fast path: if we have existing connections, return one (round-robin)
	p.mu.Lock()
	conns := p.conns[addr]
	if len(conns) > 0 {
		i := p.idx[addr] % len(conns)
		c := conns[i].c
		p.idx[addr] = (p.idx[addr] + 1) % len(conns)
		// update last used
		p.conns[addr][i].lastUsed = time.Now()
		p.mu.Unlock()
		return c, nil
	}

	// no existing connection: attempt to dial, but ensure we don't exceed MaxConnsPerAddr
	max := p.cfg.MaxConnsPerAddr
	if max <= 0 {
		max = 1
	}

	// mark dial in-progress to avoid too many concurrent dials exceeding max
	if p.inProgress[addr] >= max {
		// another goroutine is dialing up to max; wait a short time and retry
		p.mu.Unlock()
		// simple backoff loop
		deadline := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
			p.mu.Lock()
			conns = p.conns[addr]
			if len(conns) > 0 {
				i := p.idx[addr] % len(conns)
				c := conns[i].c
				p.idx[addr] = (p.idx[addr] + 1) % len(conns)
				p.mu.Unlock()
				return c, nil
			}
			if p.inProgress[addr] < max {
				// try to proceed with dialing
				p.inProgress[addr]++
				p.mu.Unlock()
				goto dial
			}
			p.mu.Unlock()
		}
		// timed out waiting; fall through and attempt to dial
		p.mu.Lock()
	}
	p.inProgress[addr]++
	p.mu.Unlock()

dial:
	// perform dial (use pool-configured dial timeout if ctx has none)
	dialCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		dialCtx, cancel = context.WithTimeout(ctx, p.cfg.DialTimeout)
		defer cancel()
	}
	conn, err := grpc.DialContext(dialCtx, addr, grpc.WithInsecure(), grpc.WithBlock())

	p.mu.Lock()
	// dialing finished
	p.inProgress[addr]--
	if err != nil {
		p.mu.Unlock()
		return nil, err
	}
	// if we still have capacity, append; otherwise close new conn and return an existing one
	conns = p.conns[addr]
	if len(conns) < max {
		p.conns[addr] = append(conns, connEntry{c: conn, lastUsed: time.Now()})
		// set idx if not present
		if _, ok := p.idx[addr]; !ok {
			p.idx[addr] = 0
		}
		p.mu.Unlock()
		return conn, nil
	}
	// no capacity: reuse existing connection
	existing := p.conns[addr][0].c
	p.mu.Unlock()
	conn.Close()
	return existing, nil
}

// CloseAll closes all pooled connections.
func (p *Pool) CloseAll() {
	// Ensure CloseAll is safe to call multiple times and stops the reaper.
	p.closeOnce.Do(func() {
		// signal reaper to stop
		close(p.stop)

		p.mu.Lock()
		defer p.mu.Unlock()
		for k, clist := range p.conns {
			for _, e := range clist {
				if e.c != nil {
					e.c.Close()
				}
			}
			delete(p.conns, k)
		}
		// reset indices and in-progress trackers
		p.idx = make(map[string]int)
		p.inProgress = make(map[string]int)
	})
}

// startReaper runs periodically and closes connections idle longer than cfg.IdleTimeout
func (p *Pool) startReaper(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cutoff := time.Now().Add(-p.cfg.IdleTimeout)
			p.mu.Lock()
			for addr, clist := range p.conns {
				var keep []connEntry
				for _, e := range clist {
					if e.lastUsed.Before(cutoff) {
						if e.c != nil {
							e.c.Close()
						}
						continue
					}
					keep = append(keep, e)
				}
				if len(keep) == 0 {
					delete(p.conns, addr)
					delete(p.idx, addr)
				} else {
					p.conns[addr] = keep
				}
			}
			p.mu.Unlock()
		case <-p.stop:
			return
		}
	}
}
