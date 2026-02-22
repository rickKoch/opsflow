package main

import (
	"context"
	"fmt"
	"log"

	"github.com/rickKoch/opsflow/actor"
	grpcsrv "github.com/rickKoch/opsflow/grpc"
	"github.com/rickKoch/opsflow/logging"
)

type EchoActor struct{}

func (EchoActor) Receive(ctx context.Context, msg actor.Message) {
	fmt.Printf("EchoActor: %s\n", string(msg.Payload))
}

func main() {
	reg := actor.NewRegistry(nil, logging.StdLogger{}, nil)
	_, _ = reg.Spawn(context.Background(), actor.PID("echo"), EchoActor{}, 8)
	srv := grpcsrv.NewServer(reg)
	go func() {
		if err := srv.Serve(":8081"); err != nil {
			log.Fatal(err)
		}
	}()
	fmt.Println("server running :8081")
	// block forever
	select {}
}
