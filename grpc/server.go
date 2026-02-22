package grpcsrv

import (
	"context"
	"net"
	"time"

	"github.com/rickKoch/opsflow/actor"
	genpb "github.com/rickKoch/opsflow/grpc/gen"
	"google.golang.org/grpc"
)

type Server struct {
	srv *grpc.Server
	reg *actor.Registry
}

// Server implements generated ActorServiceServer
type ServerImpl struct {
	genpb.UnimplementedActorServiceServer
	srv  *grpc.Server
	orch OrchestratorHandler
}

// OrchestratorHandler defines the methods the gRPC server needs from the orchestrator.
// We declare it here to avoid a circular import: orchestrator implements these
// methods and can be passed directly to NewServer.
type OrchestratorHandler interface {
	Send(ctx context.Context, id actor.PID, msg actor.Message) error
	HandleRegister(ctx context.Context, req *genpb.RegisterRequest) (*genpb.RegisterResponse, error)
}

func NewServer(orch OrchestratorHandler) *ServerImpl {
	s := grpc.NewServer()
	srv := &ServerImpl{srv: s, orch: orch}
	// register generated ActorService server implementation
	genpb.RegisterActorServiceServer(s, srv)
	return srv
}

func (s *ServerImpl) Send(ctx context.Context, msg *genpb.ActorMessage) (*genpb.SendResponse, error) {
	p := actor.PID(msg.Pid)
	m := actor.Message{Payload: msg.Payload, Type: msg.Typ}
	if err := s.orch.Send(ctx, p, m); err != nil {
		return &genpb.SendResponse{Ok: false, Error: err.Error()}, nil
	}
	return &genpb.SendResponse{Ok: true}, nil
}

// Register receives a registration from a peer node announcing its actors.
// It updates the local registry with remote actor refs (address is dialable).
func (s *ServerImpl) Register(ctx context.Context, req *genpb.RegisterRequest) (*genpb.RegisterResponse, error) {
	var remoteActors []actor.RemoteActorRef
	now := time.Now()
	for _, a := range req.GetActors() {
		remoteActors = append(remoteActors, actor.RemoteActorRef{ID: actor.PID(a.GetName()), Address: req.GetAddress(), LastSeen: now})
	}
	return s.orch.HandleRegister(ctx, req)
}

func (s *ServerImpl) Serve(laddr string) error {
	lis, err := net.Listen("tcp", laddr)
	if err != nil {
		return err
	}
	return s.srv.Serve(lis)
}
