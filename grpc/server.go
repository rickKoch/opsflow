package grpcsrv

import (
	"context"
	"net"

	"github.com/rickKoch/opsflow/actor"
	genpb "github.com/rickKoch/opsflow/grpc/gen"
	"google.golang.org/grpc"
)

type Server struct {
	srv  *grpc.Server
	orch OrchestratorHandler
}

// mustEmbedUnimplementedActorServiceServer satisfies generated interface for forward compatibility.
// embed unimplemented to satisfy interface
type unimplementedEmbed struct{}

func (unimplementedEmbed) mustEmbedUnimplementedActorServiceServer() {}
func (unimplementedEmbed) testEmbeddedByValue()                      {}

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
	HandleShareActors(ctx context.Context, req *genpb.ShareActorsRequest) (*genpb.ShareActorsResponse, error)
}

func NewServer(orch OrchestratorHandler) *ServerImpl {
	s := grpc.NewServer()
	srv := &ServerImpl{srv: s, orch: orch}
	// register generated ActorService server implementation
	genpb.RegisterActorServiceServer(s, srv)
	// register ActorRegistry server for ShareActors propagation
	genpb.RegisterActorRegistryServer(s, &RegistryServer{orch: orch})
	return srv
}

func (s *ServerImpl) Send(ctx context.Context, msg *genpb.ActorMessage) (*genpb.SendResponse, error) {
	p := actor.PID(msg.Pid)
	m := actor.Message{Payload: msg.Payload, Type: msg.Typ}
	if s.orch != nil {
		if err := s.orch.Send(ctx, p, m); err != nil {
			return &genpb.SendResponse{Ok: false, Error: err.Error()}, nil
		}
	} else {
		return &genpb.SendResponse{Ok: false, Error: "orchestrator not configured"}, nil
	}
	return &genpb.SendResponse{Ok: true}, nil
}

// Register receives a registration from a peer node announcing its actors.
// It updates the local registry with remote actor refs (address is dialable).
func (s *ServerImpl) Register(ctx context.Context, req *genpb.RegisterRequest) (*genpb.RegisterResponse, error) {
	if s.orch == nil {
		return &genpb.RegisterResponse{Success: false, Error: "orchestrator not configured"}, nil
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

// RegistryServer implements generated ActorRegistryServer to receive shared actor lists.
type RegistryServer struct {
	genpb.UnimplementedActorRegistryServer
	orch OrchestratorHandler
}

// ShareActors updates the local registry with remote actors provided by a peer.
func (rs *RegistryServer) ShareActors(ctx context.Context, req *genpb.ShareActorsRequest) (*genpb.ShareActorsResponse, error) {
	if rs.orch == nil {
		return &genpb.ShareActorsResponse{Success: false, Error: "orchestrator not configured"}, nil
	}
	return rs.orch.HandleShareActors(ctx, req)
}
