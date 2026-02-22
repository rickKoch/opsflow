package orchestrator

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/rickKoch/opsflow/actor"
	grpcsrv "github.com/rickKoch/opsflow/grpc"
	genpb "github.com/rickKoch/opsflow/grpc/gen"
	"github.com/rickKoch/opsflow/internal/grpcpool"
	"github.com/rickKoch/opsflow/logging"
	"github.com/rickKoch/opsflow/persistence"
	"google.golang.org/grpc"
)

// TestRemoteForwarding verifies that when an actor is not local the router forwards
// the message to a remote ActorService endpoint.
func TestRemoteForwarding(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := persistence.NewFileStore(dir)
	reg := actor.NewRegistry(store, logging.StdLogger{}, nil)

	// Start a test ActorService gRPC server that captures received messages.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	recvCh := make(chan *genpb.ActorMessage, 4)
	// implement ActorServiceServer
	genpb.RegisterActorServiceServer(srv, &testActorServiceServer{recv: recvCh})
	go srv.Serve(lis)
	defer func() { srv.Stop(); lis.Close() }()

	// register remote actor in registry
	addr := lis.Addr().String()
	reg.UpdateRemoteActors(ctx, []actor.RemoteActorRef{{ID: actor.PID("remote1"), Address: addr, LastSeen: time.Now()}})

	// orchestrator with router
	orch := NewOrchestratorWithRouterConfig(Config{Registry: reg, Persistence: store, Logger: logging.StdLogger{}, Tracer: nil}, grpcpool.PoolConfig{DialTimeout: 2 * time.Second, MaxConnsPerAddr: 1})

	// send to remote actor (not local)
	if err := orch.Send(ctx, actor.PID("remote1"), actor.Message{Payload: []byte("ping")}); err != nil {
		t.Fatalf("orch send failed: %v", err)
	}

	select {
	case m := <-recvCh:
		if string(m.Payload) != "ping" {
			t.Fatalf("unexpected payload: %s", string(m.Payload))
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("remote server did not receive message")
	}
}

// TestPropagation ensures StartPropagation sends local actor list to peers via ActorRegistry.ShareActors
func TestPropagation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := persistence.NewFileStore(dir)
	reg := actor.NewRegistry(store, logging.StdLogger{}, nil)

	// start a test ActorRegistry server to capture ShareActors requests
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	recvCh := make(chan *genpb.RegisterRequest, 4)
	genpb.RegisterActorServiceServer(srv, &testRegisterHandler{reg: reg, recv: recvCh})
	go srv.Serve(lis)
	defer func() { srv.Stop(); lis.Close() }()

	orch := NewOrchestratorWithRouterConfig(Config{Registry: reg, Persistence: store, Logger: logging.StdLogger{}, Tracer: nil}, grpcpool.PoolConfig{DialTimeout: 2 * time.Second, MaxConnsPerAddr: 1})

	// spawn a local actor so there is something to propagate
	_, err = reg.Spawn(ctx, actor.PID("local2"), &simpleActor{}, 4)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	// start propagation to the test registry
	orch.StartPropagation(ctx, []string{lis.Addr().String()}, 200*time.Millisecond)

	select {
	case req := <-recvCh:
		found := false
		for _, a := range req.GetActors() {
			if a.GetName() == "local2" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("propagation did not include local2")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no propagation received")
	}
}

// TestPruner validates stale remote actors are pruned.
func TestPruner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	store := persistence.NewFileStore(dir)
	reg := actor.NewRegistry(store, logging.StdLogger{}, nil)

	// add remote actor with old LastSeen
	old := time.Now().Add(-10 * time.Second)
	reg.UpdateRemoteActors(ctx, []actor.RemoteActorRef{{ID: actor.PID("old"), Address: "127.0.0.1:1234", LastSeen: old}})

	// start pruner with ttl 1s and interval 100ms
	reg.StartRemotePruner(ctx, 1*time.Second, 100*time.Millisecond)

	// wait up to 2s for pruning
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("pruner did not remove stale actor")
		default:
			if _, ok := reg.GetRemote(actor.PID("old")); !ok {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// simpleActor used in propagation test
type simpleActor struct{}

func (simpleActor) Receive(context.Context, actor.Message) {}

// testActorServiceServer implements ActorServiceServer for testing
type testActorServiceServer struct {
	genpb.UnimplementedActorServiceServer
	recv chan *genpb.ActorMessage
}

func (s *testActorServiceServer) Send(ctx context.Context, msg *genpb.ActorMessage) (*genpb.SendResponse, error) {
	s.recv <- msg
	return &genpb.SendResponse{Ok: true}, nil
}

// testRegisterHandler implements ActorServiceServer for registering remote actors
type testRegisterHandler struct {
	genpb.UnimplementedActorServiceServer
	reg  *actor.Registry
	recv chan *genpb.RegisterRequest
}

func (h *testRegisterHandler) Send(ctx context.Context, msg *genpb.ActorMessage) (*genpb.SendResponse, error) {
	// B does not host the actor; Send here is not used in this test
	return &genpb.SendResponse{Ok: false, Error: "not hosted"}, nil
}

func (h *testRegisterHandler) Register(ctx context.Context, req *genpb.RegisterRequest) (*genpb.RegisterResponse, error) {
	// deliver the request to the test harness so assertions can observe it
	if h.recv != nil {
		select {
		case h.recv <- req:
		default:
			// non-blocking send to avoid blocking propagation loop in tests
		}
	}
	var remoteActors []actor.RemoteActorRef
	now := time.Now()
	for _, a := range req.GetActors() {
		remoteActors = append(remoteActors, actor.RemoteActorRef{ID: actor.PID(a.GetName()), Address: a.GetAddress(), LastSeen: now})
	}
	h.reg.UpdateRemoteActors(ctx, remoteActors)
	return &genpb.RegisterResponse{Success: true}, nil
}

// TestRegisterRPCPropagation verifies that when a peer calls ActorService.Register
// the registry on the receiving node is updated and the router/orchestrator will
// forward messages to the remote actor by dialing the provided address.
func TestRegisterRPCPropagation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := persistence.NewFileStore(dir)
	reg := actor.NewRegistry(store, logging.StdLogger{}, nil)

	// Start a test ActorService gRPC server (peer A) that captures received messages.
	lisA, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen A: %v", err)
	}
	srvA := grpc.NewServer()
	recvCh := make(chan *genpb.ActorMessage, 4)
	genpb.RegisterActorServiceServer(srvA, &testActorServiceServer{recv: recvCh})
	go srvA.Serve(lisA)
	defer func() { srvA.Stop(); lisA.Close() }()

	// Start ActorService on node B (our orchestrator/registry) and listen
	lisB, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen B: %v", err)
	}
	srvB := grpc.NewServer()
	genpb.RegisterActorServiceServer(srvB, &testRegisterHandler{reg: reg})
	go srvB.Serve(lisB)
	defer func() { srvB.Stop(); lisB.Close() }()

	// Build a client to call Register on node B, advertising actor on A
	bAddr := lisB.Addr().String()
	aAddr := lisA.Addr().String()
	c, err := grpcsrv.NewClientWithOpts(bAddr, 3, 50*time.Millisecond, 2*time.Second)
	if err != nil {
		t.Fatalf("client dial B: %v", err)
	}
	defer c.Close()

	infos := []*genpb.ActorInfo{{Name: "echo", Address: aAddr}}
	_, err = c.Register(ctx, "serviceA", aAddr, infos)
	if err != nil {
		t.Fatalf("register rpc failed: %v", err)
	}

	// create orchestrator on B and send message to remote actor
	orch := NewOrchestratorWithRouterConfig(Config{Registry: reg, Persistence: store, Logger: logging.StdLogger{}, Tracer: nil}, grpcpool.PoolConfig{DialTimeout: 2 * time.Second, MaxConnsPerAddr: 1})

	if err := orch.Send(ctx, actor.PID("echo"), actor.Message{Payload: []byte("ping via register")}); err != nil {
		t.Fatalf("orch send failed: %v", err)
	}

	select {
	case m := <-recvCh:
		if string(m.Payload) != "ping via register" {
			t.Fatalf("unexpected payload: %s", string(m.Payload))
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("peer A did not receive message")
	}
}
