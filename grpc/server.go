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

// mustEmbedUnimplementedActorServiceServer satisfies generated interface for forward compatibility.
// embed unimplemented to satisfy interface
type unimplementedEmbed struct{}

func (unimplementedEmbed) mustEmbedUnimplementedActorServiceServer() {}
func (unimplementedEmbed) testEmbeddedByValue()                      {}

// Server implements generated ActorServiceServer
type ServerImpl struct {
	genpb.UnimplementedActorServiceServer
	srv *grpc.Server
	reg *actor.Registry
}

func NewServer(reg *actor.Registry) *ServerImpl {
	s := grpc.NewServer()
	srv := &ServerImpl{srv: s, reg: reg}
	// register generated ActorService server implementation
	genpb.RegisterActorServiceServer(s, srv)
	// register ActorRegistry server for ShareActors propagation
	genpb.RegisterActorRegistryServer(s, &RegistryServer{reg: reg})
	return srv
}

func (s *ServerImpl) Send(ctx context.Context, msg *genpb.ActorMessage) (*genpb.SendResponse, error) {
	p := actor.PID(msg.Pid)
	m := actor.Message{Payload: msg.Payload, Type: msg.Typ}
	if err := s.reg.Send(ctx, p, m); err != nil {
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
	s.reg.UpdateRemoteActors(ctx, remoteActors)
	return &genpb.RegisterResponse{Success: true}, nil
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
	reg *actor.Registry
}

// ShareActors updates the local registry with remote actors provided by a peer.
func (rs *RegistryServer) ShareActors(ctx context.Context, req *genpb.ShareActorsRequest) (*genpb.ShareActorsResponse, error) {
	var remoteActors []actor.RemoteActorRef
	now := time.Now()
	for _, a := range req.GetActors() {
		remoteActors = append(remoteActors, actor.RemoteActorRef{ID: actor.PID(a.GetName()), Address: a.GetAddress(), LastSeen: now})
	}
	rs.reg.UpdateRemoteActors(ctx, remoteActors)
	return &genpb.ShareActorsResponse{Success: true}, nil
}
