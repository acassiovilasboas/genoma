.PHONY: build build-mcp run test test-int lint clean migrate-up migrate-down docker-up docker-down fmt vet

# Variables
BINARY_NAME=genoma
BUILD_DIR=bin
MAIN_PATH=./cmd/genoma
MCP_PATH=./cmd/mcp
GO=go
DOCKER_COMPOSE=docker compose

# Build
build:
	$(GO) build -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)

build-mcp:
	$(GO) build -o $(BUILD_DIR)/genoma-mcp $(MCP_PATH)

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -o $(BUILD_DIR)/$(BINARY_NAME)-linux $(MAIN_PATH)

# Run
run:
	$(GO) run $(MAIN_PATH)

# Test
test:
	$(GO) test ./internal/... -v -count=1

test-int:
	$(GO) test ./tests/integration/... -v -tags=integration -count=1

test-cover:
	$(GO) test ./internal/... -coverprofile=coverage.out
	$(GO) tool cover -html=coverage.out -o coverage.html

# Code quality
fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

lint:
	golangci-lint run ./...

# Database migrations
migrate-up:
	migrate -path internal/persistence/migrations -database "$${DATABASE_URL}" up

migrate-down:
	migrate -path internal/persistence/migrations -database "$${DATABASE_URL}" down

migrate-create:
	migrate create -ext sql -dir internal/persistence/migrations -seq $(name)

# Docker
docker-up:
	$(DOCKER_COMPOSE) up -d

docker-down:
	$(DOCKER_COMPOSE) down

docker-build:
	$(DOCKER_COMPOSE) build

docker-logs:
	$(DOCKER_COMPOSE) logs -f

# Clean
clean:
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html
