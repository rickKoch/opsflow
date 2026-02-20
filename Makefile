all: build

build:
	go build ./...

proto:
	@echo "protoc generation currently requires protoc and the Go plugin."
	@echo "Install protoc and run: protoc --go_out=./grpc/gen --go-grpc_out=./grpc/gen grpc/actor.proto"
