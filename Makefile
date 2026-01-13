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

deploy:
	cd web && npm run build
	CGO_ENABLED=0 GOARCH=arm64 GOOS=linux go build -o $(BUILD_DIR)/$(BINARY)_arm64 ./cmd/mmtools
	scp $(BUILD_DIR)/$(BINARY)_arm64 mattermost-notify@terraform:/home/mattermost-notify/mmtools
