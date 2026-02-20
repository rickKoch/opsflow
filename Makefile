all: build

build:
	go build ./...

# Protobuf / gRPC generation
PROTOC := $(if $(wildcard ./bin/protoc),./bin/protoc,$(shell command -v protoc 2>/dev/null))
PROTO_FILES := $(wildcard grpc/*.proto)
GEN_DIR := grpc/gen

.PHONY: proto proto-check proto-tools install-proto-tools clean build all

proto: proto-check proto-tools
	@mkdir -p $(GEN_DIR)
	@echo "generating grpc code to $(GEN_DIR)"
	$(PROTOC) --proto_path=grpc \
		--go_out=$(GEN_DIR) --go_opt=paths=source_relative \
		--go-grpc_out=$(GEN_DIR) --go-grpc_opt=paths=source_relative \
		$(PROTO_FILES)

proto-check:
	@if [ -z "$(PROTOC)" ]; then \
		echo "protoc not found. Run 'make download-protoc' to download a local protoc into ./bin, or install protoc system-wide: https://grpc.io/docs/protoc-installation/"; exit 1; \
	fi

proto-tools:
	@echo "installing Go protoc plugins (protoc-gen-go, protoc-gen-go-grpc)"
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest


# Download a local protoc binary into ./bin (no sudo)
PROTOC_VERSION ?= 23.4
PROTOC_OS := $(shell uname -s | tr '[:upper:]' '[:lower:]' | sed -e 's/darwin/osx/')
PROTOC_ARCH := $(shell uname -m | sed -e 's/x86_64/x86_64/' -e 's/amd64/x86_64/' -e 's/aarch64/aarch_64/' -e 's/arm64/aarch_64/')
PROTOC_ZIP := protoc-$(PROTOC_VERSION)-$(PROTOC_OS)-$(PROTOC_ARCH).zip

download-protoc:
	@set -e; \
	mkdir -p ./bin; \
	printf "Downloading protoc %s for %s-%s\n" "$(PROTOC_VERSION)" "$(PROTOC_OS)" "$(PROTOC_ARCH)"; \
	URL="https://github.com/protocolbuffers/protobuf/releases/download/v$(PROTOC_VERSION)/$(PROTOC_ZIP)"; \
	if command -v curl >/dev/null 2>&1; then \
		curl -L -f -s -o /tmp/$(PROTOC_ZIP) "$$URL" || (echo "download failed" && exit 1); \
	else \
		wget -q -O /tmp/$(PROTOC_ZIP) "$$URL" || (echo "download failed" && exit 1); \
	fi; \
	mkdir -p ./bin/tmp_unzip; \
	unzip -o /tmp/$(PROTOC_ZIP) -d ./bin/tmp_unzip >/dev/null; \
	DEST=./bin/protoc-$(PROTOC_VERSION)-$(PROTOC_OS)-$(PROTOC_ARCH); \
	mkdir -p "$${DEST}"; \
	mv ./bin/tmp_unzip/bin "$${DEST}/bin" >/dev/null 2>&1 || true; \
	mv ./bin/tmp_unzip/include "$${DEST}/include" >/dev/null 2>&1 || true; \
	rm -rf ./bin/tmp_unzip; \
	cat > ./bin/protoc <<'EOF'
	#!/usr/bin/env sh
	DIR="$(cd "$(dirname "$0")" && pwd)"
	exec "$${DIR}/protoc-$(PROTOC_VERSION)-$(PROTOC_OS)-$(PROTOC_ARCH)/bin/protoc" "$@"
	EOF
	chmod +x ./bin/protoc; \
	rm -f /tmp/$(PROTOC_ZIP); \
	echo "protoc installed to ./bin/protoc"

install-proto-tools: proto-tools
	@echo "go bin directory: $(shell go env GOPATH 2>/dev/null)/bin"

clean:
	@rm -rf $(GEN_DIR)
