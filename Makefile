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

REMOTE_HOST=mattermost-notify@terraform
REMOTE_PATH=/home/mattermost-notify/mmtools

deploy:
	cd web && npm run build
	CGO_ENABLED=0 GOARCH=arm64 GOOS=linux go build -o $(BUILD_DIR)/$(BINARY)_arm64 ./cmd/mmtools
	@echo "Stopping mmtools service..."
	ssh $(REMOTE_HOST) "systemctl --user stop mmtools"
	@echo "Waiting 5 seconds..."
	@sleep 5
	@echo "Uploading binary..."
	scp $(BUILD_DIR)/$(BINARY)_arm64 $(REMOTE_HOST):$(REMOTE_PATH)
	@echo "Starting mmtools service..."
	ssh $(REMOTE_HOST) "systemctl --user start mmtools"
	@echo "Deploy complete!"
