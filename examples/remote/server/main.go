package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/rickKoch/opsflow/actor"
	grpcsrv "github.com/rickKoch/opsflow/grpc"
	"github.com/rickKoch/opsflow/logging"
	"github.com/rickKoch/opsflow/orchestrator"
)

type EchoActor struct{}

func (EchoActor) Receive(ctx context.Context, msg actor.Message) {
	fmt.Printf("EchoActor received: %s\n", string(msg.Payload))
}

func main() {
	port := flag.String("port", "8081", "tcp port to listen on, e.g. 8081")
	actorName := flag.String("actor", "", "actor name to host on this node (empty = host none)")
	peers := flag.String("peers", "", "comma-separated peer addresses to propagate registration to (host:port)")
	flag.Parse()

	addr := ":" + *port
	reg := actor.NewRegistry(nil, logging.StdLogger{}, nil)

	// create orchestrator to manage actors and propagation
	orch := orchestrator.NewOrchestrator(orchestrator.Config{Registry: reg, Persistence: nil, Logger: logging.StdLogger{}, Tracer: nil})

	// spawn actor if requested via orchestrator
	if *actorName != "" {
		if err := orch.SpawnAndRegister(context.Background(), actor.PID(*actorName), EchoActor{}, 8); err != nil {
			log.Fatalf("spawn actor: %v", err)
		}
		log.Printf("spawned actor %s", *actorName)
	}

	srv := grpcsrv.NewServer(reg)
	go func() {
		if err := srv.Serve(addr); err != nil {
			log.Fatal(err)
		}
	}()
	log.Printf("server running %s", addr)

	// if peers provided, tell the orchestrator to propagate our actors via Register RPC
	if *peers != "" {
		peerList := strings.Split(*peers, ",")
		serviceID := addr
		dialAddr := "localhost:" + *port
		// start propagation using orchestrator helper
		orch.StartRegisterPropagation(context.Background(), peerList, serviceID, dialAddr, 1*time.Second)
	}

	select {}
}
