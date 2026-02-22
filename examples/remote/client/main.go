package main

import (
	"context"
	"fmt"
	"log"
	"time"

	grpcsrv "github.com/rickKoch/opsflow/grpc"
)

func main() {
	c, err := grpcsrv.NewClientWithOpts("localhost:8081", 3, 50*time.Millisecond, 2*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()
	for i := 0; i < 3; i++ {
		_, err := c.Send(context.Background(), "echo", []byte(fmt.Sprintf("hello %d", i)), "text")
		if err != nil {
			log.Println("send error:", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
