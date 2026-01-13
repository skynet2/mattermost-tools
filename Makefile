.PHONY: build test clean generate

BINARY=mmtools
BUILD_DIR=bin

build:
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/mmtools

test:
	go test -v ./...

clean:
	rm -rf $(BUILD_DIR)

generate:
	go generate ./...
