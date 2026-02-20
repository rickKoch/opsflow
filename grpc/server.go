package grpcsrv

import (
	"context"
	"net"

	"github.com/rickKoch/opsflow/actor"
	"github.com/rickKoch/opsflow/grpc/gen"
	"google.golang.org/grpc"
)

type Server struct {
	srv *grpc.Server
	reg *actor.Registry
}

func NewServer(reg *actor.Registry) *Server {
	s := grpc.NewServer()
	srv := &Server{srv: s, reg: reg}
	// register generated server implementation
	gen.RegisterActorServiceServer(s, srv)
	return srv
}

func (s *Server) Send(ctx context.Context, msg *gen.ActorMessage) (*gen.SendResponse, error) {
	p := actor.PID(msg.Pid)
	m := actor.Message{Payload: msg.Payload, Type: msg.Typ}
	if err := s.reg.Send(ctx, p, m); err != nil {
		return &gen.SendResponse{Ok: false, Error: err.Error()}, nil
	}
	return &gen.SendResponse{Ok: true}, nil
}

func (s *Server) Serve(laddr string) error {
	lis, err := net.Listen("tcp", laddr)
	if err != nil {
		return err
	}
	return s.srv.Serve(lis)
}
