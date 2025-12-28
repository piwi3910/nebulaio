# Suggested Commands for NebulaIO Development

## Build Commands

```bash
# Build the binary
make build

# Build for Linux
make build-linux

# Build for macOS
make build-darwin

# Build all platforms
make build-all

# Clean build artifacts
make clean
```

## Development Commands

```bash
# Download dependencies
make deps

# Update dependencies
make deps-update

# Run the server (builds first)
make run

# Run in dev mode with hot reload (requires air)
make dev
```

## Code Quality Commands

```bash
# Format code
make fmt

# Run linter
make lint

# Run go vet
make vet

# Run tests
make test

# Run tests with coverage
make test-coverage
```

## Docker Commands

```bash
# Build Docker image
make docker-build

# Run Docker container
make docker-run

# Start with docker-compose
make docker-compose-up

# Stop docker-compose
make docker-compose-down
```

## Web Console Commands

```bash
# Install web dependencies
make web

# Build web console
make web-build

# Run web dev server
make web-dev
```

## Utility Commands

```bash
# Initialize data directory
make init-data

# Show help
make help
```

## Direct Go Commands

```bash
# Build all packages
go build ./...

# Build specific package
go build ./internal/auth/...

# Run tests for package
go test ./internal/auth/...

# Format check without changes
gofmt -d .

# Check syntax only
gofmt -e ./internal/auth/*.go
```
