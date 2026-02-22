package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	grpcsrv "github.com/rickKoch/opsflow/grpc"
)

func main() {
	addr := flag.String("addr", "localhost:8081", "target actor service address")
	target := flag.String("target", "echo", "actor PID to send messages to")
	flag.Parse()

	c, err := grpcsrv.NewClientWithOpts(*addr, 3, 50*time.Millisecond, 2*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()
	for i := 0; i < 3; i++ {
		_, err := c.Send(context.Background(), *target, []byte(fmt.Sprintf("hello %d", i)), "text")
		if err != nil {
			log.Println("send error:", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
