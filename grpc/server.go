package grpcsrv

import (
	"context"
	"net"

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
	// register generated server implementation
	genpb.RegisterActorServiceServer(s, srv)
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

func (s *ServerImpl) Serve(laddr string) error {
	lis, err := net.Listen("tcp", laddr)
	if err != nil {
		return err
	}
	return s.srv.Serve(lis)
}
