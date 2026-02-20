package main

import (
	"context"
	"fmt"
	"time"

	"github.com/rickKoch/opsflow/actor"
	"github.com/rickKoch/opsflow/logging"
	"github.com/rickKoch/opsflow/orchestrator"
	"github.com/rickKoch/opsflow/persistence"
)

type HelloActor struct{}

func (HelloActor) Receive(ctx context.Context, msg actor.Message) {
	fmt.Printf("HelloActor received: %s\n", string(msg.Payload))
}

func main() {
	ctx := context.Background()
	store := persistence.NewFileStore("./data")
	reg := actor.NewRegistry(store, logging.StdLogger{}, nil)
	orch := orchestrator.NewOrchestrator(reg, store, logging.StdLogger{}, nil)
	_ = orch.SpawnAndRegister(ctx, actor.PID("hello"), HelloActor{}, 16)
	_ = reg.Send(ctx, actor.PID("hello"), actor.Message{Payload: []byte("world")})
	time.Sleep(500 * time.Millisecond)
}
