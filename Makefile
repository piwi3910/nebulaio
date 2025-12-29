.PHONY: all build clean test run dev lint fmt deps web web-build help

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

# Go variables
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOMOD := $(GOCMD) mod
GOFMT := gofmt
GOVET := $(GOCMD) vet

# Binary name
BINARY := nebulaio
BINARY_DIR := bin

# Source directories
CMD_DIR := ./cmd/nebulaio
WEB_DIR := ./web

all: deps build ## Build everything

build: ## Build the binary
	@echo "Building $(BINARY)..."
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/$(BINARY) $(CMD_DIR)
	@echo "Binary built: $(BINARY_DIR)/$(BINARY)"

build-linux: ## Build for Linux (amd64)
	@echo "Building $(BINARY) for Linux..."
	@mkdir -p $(BINARY_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/$(BINARY)-linux-amd64 $(CMD_DIR)

build-darwin: ## Build for macOS (arm64)
	@echo "Building $(BINARY) for macOS..."
	@mkdir -p $(BINARY_DIR)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/$(BINARY)-darwin-arm64 $(CMD_DIR)

build-all: build-linux build-darwin ## Build for all platforms

clean: ## Clean build artifacts
	@echo "Cleaning..."
	@rm -rf $(BINARY_DIR)
	@rm -rf ./data
	@rm -rf $(WEB_DIR)/dist
	@rm -rf $(WEB_DIR)/node_modules

test: ## Run tests
	@echo "Running tests..."
	$(GOTEST) -v -race -cover ./...

test-coverage: ## Run tests with coverage
	@echo "Running tests with coverage..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

run: build ## Build and run
	@echo "Running $(BINARY)..."
	./$(BINARY_DIR)/$(BINARY) --debug

dev: ## Run in development mode with hot reload (requires air)
	@which air > /dev/null || (echo "Installing air..." && go install github.com/cosmtrek/air@latest)
	air

lint: ## Run linter
	@echo "Running linter..."
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

fmt: ## Format code
	@echo "Formatting code..."
	$(GOFMT) -s -w .

vet: ## Run go vet
	@echo "Running go vet..."
	$(GOVET) ./...

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

deps-update: ## Update dependencies
	@echo "Updating dependencies..."
	$(GOMOD) get -u ./...
	$(GOMOD) tidy

# Web console targets
web: ## Install web dependencies
	@echo "Installing web dependencies..."
	cd $(WEB_DIR) && npm install

web-build: web ## Build web console
	@echo "Building web console..."
	cd $(WEB_DIR) && npm run build

web-dev: web ## Run web console in development mode
	@echo "Starting web console dev server..."
	cd $(WEB_DIR) && npm run dev

# Docker targets
docker-build: ## Build Docker image
	@echo "Building Docker image..."
	docker build -t nebulaio:$(VERSION) .

docker-run: ## Run Docker container
	docker run -it --rm \
		-p 9000:9000 \
		-p 9001:9001 \
		-p 9002:9002 \
		-v nebulaio-data:/data \
		nebulaio:$(VERSION)

docker-compose-up: ## Start with docker-compose
	docker-compose up -d

docker-compose-down: ## Stop docker-compose
	docker-compose down

# Database/development helpers
init-data: ## Initialize data directory
	@mkdir -p ./data

# Pre-commit hooks
install-hooks: ## Install pre-commit hooks
	@echo "Installing pre-commit hooks..."
	@command -v pre-commit >/dev/null 2>&1 || \
		{ echo "Error: pre-commit is not installed. Install with: pip install pre-commit"; exit 1; }
	pre-commit install
	@echo "✓ Pre-commit hooks installed successfully"

uninstall-hooks: ## Uninstall pre-commit hooks
	@echo "Uninstalling pre-commit hooks..."
	@command -v pre-commit >/dev/null 2>&1 && pre-commit uninstall || true
	@echo "✓ Pre-commit hooks uninstalled"

run-hooks: ## Run pre-commit hooks on all files
	@echo "Running pre-commit hooks on all files..."
	@command -v pre-commit >/dev/null 2>&1 || \
		{ echo "Error: pre-commit is not installed. Install with: pip install pre-commit"; exit 1; }
	pre-commit run --all-files

# Release targets
release: clean build-all ## Create release binaries
	@echo "Creating release..."
	@mkdir -p release
	@cp $(BINARY_DIR)/$(BINARY)-linux-amd64 release/
	@cp $(BINARY_DIR)/$(BINARY)-darwin-arm64 release/
	@cd release && sha256sum * > checksums.txt
	@echo "Release files created in release/"

help: ## Show this help
	@echo "NebulaIO - S3-Compatible Object Storage"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
