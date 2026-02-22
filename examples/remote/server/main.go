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
	genpb "github.com/rickKoch/opsflow/grpc/gen"
	"github.com/rickKoch/opsflow/logging"
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

	// spawn actor if requested
	if *actorName != "" {
		_, err := reg.Spawn(context.Background(), actor.PID(*actorName), EchoActor{}, 8)
		if err != nil {
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

	// if peers provided, periodically call Register on them to announce our actors
	if *peers != "" {
		peerList := strings.Split(*peers, ",")
		// use serviceID as this host:port
		serviceID := addr
		// dialable address should be localhost:port for examples
		dialAddr := "localhost:" + *port
		// small initial delay to allow peers to start
		time.Sleep(500 * time.Millisecond)
		// use orchestrator-style propagation by creating a simple ticker that uses the generated client
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				locals := reg.ListLocalActors()
				var infos []*genpb.ActorInfo
				for _, a := range locals {
					infos = append(infos, &genpb.ActorInfo{Name: string(a.ID), Address: dialAddr})
				}
				for _, p := range peerList {
					c, err := grpcsrv.NewClientWithOpts(p, 3, 50*time.Millisecond, 2*time.Second)
					if err != nil {
						log.Printf("register: dial %s failed: %v", p, err)
						continue
					}
					_, err = c.Register(context.Background(), serviceID, dialAddr, infos)
					if err != nil {
						log.Printf("register: call to %s failed: %v", p, err)
					} else {
						log.Printf("registered with peer %s", p)
					}
					c.Close()
				}
			}
		}()
	}

	select {}
}
