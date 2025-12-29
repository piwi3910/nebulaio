# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Core Policies

### Always Use Latest Tool Versions

**CRITICAL POLICY**: When updating tools and dependencies, ALWAYS use the latest stable version. NEVER downgrade versions to avoid fixing compatibility issues.

- **golangci-lint**: Always use `version: latest` in CI workflows
- **Dependencies**: Keep all tools current and fix compatibility issues in configuration
- **Rationale**: Using outdated versions accumulates technical debt and misses bug fixes, performance improvements, and security patches

When tool updates introduce breaking changes:

1. Update the configuration to match the new schema/requirements
2. Fix any compatibility issues
3. Document the changes
4. NEVER revert to older versions as a workaround

## Build and Development Commands

### Backend (Go)

```bash

# Build the binary
make build

# Run with hot reload (requires air)
make dev

# Run tests
make test                    # All tests
go test -v ./internal/...    # Specific package
go test -v -run TestName ./internal/security/...  # Single test

# Lint and format
make lint                    # Run golangci-lint
make fmt                     # Format code
make vet                     # Run go vet

# Build for specific platforms
make build-linux             # Linux amd64
make build-darwin            # macOS arm64

```bash

### Frontend (React/TypeScript)

```bash

cd web

npm install                  # Install dependencies
npm run dev                  # Development server (Vite)
npm run build                # Production build
npm run lint                 # ESLint
npm run type-check           # TypeScript check
npm run test                 # Vitest tests
npm run test:run             # Run tests once

```bash

### Docker

```bash

docker-compose up -d                    # Start with docker-compose
docker-compose -f docker-compose.yml -f docker-compose.production.yml up -d  # Production

```bash

### Running the Server

```bash

./bin/nebulaio --debug                  # Debug mode
./bin/nebulaio --data ./data --s3-port 9000 --admin-port 9001

```text

**Note:** TLS is enabled by default. Access via HTTPS (e.g., `https://localhost:9001`). Certificates are auto-generated in `data/certs/`.

## Architecture Overview

NebulaIO is an S3-compatible object storage system with distributed consensus.

### Core Components

1. **Server (`internal/server/`)** - Main entry point, initializes all services and HTTP servers:
   - S3 API (port 9000) - AWS S3-compatible API
   - Admin API (port 9001) - Management REST API + Prometheus metrics
   - Console (port 9002) - React web UI static files

2. **Metadata Store (`internal/metadata/`)** - Distributed state via Dragonboat (Raft consensus):
   - `raft.go` - Dragonboat NodeHost and state machine
   - Uses BadgerDB as the underlying KV store
   - All bucket/object metadata operations go through Raft for consistency

3. **Storage Layer (`internal/storage/`)** - Pluggable backends:
   - `fs/` - Filesystem backend (default)
   - `volume/` - High-performance block-based storage with pre-allocated volume files
   - `erasure/` - Reed-Solomon erasure coding for data durability

4. **Authentication (`internal/auth/`)** - Multi-provider auth:
   - JWT-based session management
   - LDAP integration (`ldap/`)
   - OIDC/SSO support (`oidc/`)
   - IAM policies and access keys

5. **Security (`internal/security/`)** - TLS and mTLS:
   - Auto-generates self-signed certificates by default
   - `tls.go` - TLS manager with certificate lifecycle
   - `mtls.go` - Mutual TLS for cluster communication

### Request Flow

```text

Client Request
     │
     ▼
S3 API Handler (internal/api/s3/)
     │
     ▼
Object Service (internal/object/) ─── validates IAM/policies
     │
     ▼
Metadata Store (internal/metadata/) ─── Raft consensus
     │
     ▼
Storage Backend (internal/storage/) ─── actual data I/O

```bash

### Web Console (`web/`)

React 19 + TypeScript + Mantine UI + TanStack Query:

- `src/pages/` - Route components
- `src/components/` - Reusable UI components
- `src/api/` - API client (axios)
- `src/store/` - Zustand state management

### Key Patterns

- **Configuration**: Viper-based (`internal/config/`). Supports YAML files, env vars (`NEBULAIO_*`), and CLI flags
- **Logging**: zerolog throughout. Use `log.Info()`, `log.Error().Err(err)`, etc.
- **Error Handling**: Return errors up the stack; log at handler level
- **Testing**: `testify/assert` for assertions, table-driven tests preferred

### Cluster Communication

- **Dragonboat** (port 9003) - Raft consensus for metadata
- **Memberlist/Gossip** (port 9004) - Node discovery and health

## Important Conventions

- TLS is **enabled by default** - servers use HTTPS
- Go version: 1.24+
- Node version: 20+
- Use `make lint` before committing Go changes
- Frontend uses strict TypeScript (`npm run type-check`)

## Testing Standards

### Go Testing

**Framework:** Use [testify](https://github.com/stretchr/testify) for all Go tests

- **Assertions**: Use `assert` or `require` from testify, not plain Go conditionals
- **Mocking**: Use testify's mock package for interfaces
- **Common Mocks**: Use centralized mock implementations from `internal/testutil/mocks/`

**Example:**

```go
import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "github.com/yourusername/nebulaio/internal/testutil/mocks"
)

func TestBucketCreation(t *testing.T) {
    // Use common mock backend
    mockStorage := mocks.NewMockStorageBackend()
    mockMetadata := mocks.NewMockMetadataStore()

    // Setup expectations
    mockMetadata.On("CreateBucket", mock.Anything, "test-bucket").Return(nil)

    // Test assertions
    err := service.CreateBucket(ctx, "test-bucket")
    require.NoError(t, err)
    assert.True(t, mockStorage.BucketExists("test-bucket"))

    // Verify mock expectations
    mockMetadata.AssertExpectations(t)
}
```

**Don't:**

- Create duplicate mock implementations in each test file
- Use plain `if err != nil { t.Fatal() }` - use `require.NoError(t, err)`
- Skip mock verification - always call `AssertExpectations(t)`

**Related:** See Issue #101 for test standardization migration plan

## Error Handling Patterns

### Custom Error Types

Use error types from `internal/s3errors/` for S3-compatible responses:

```go
import "github.com/yourusername/nebulaio/internal/s3errors"

// Return S3-compatible errors
if bucket == "" {
    return s3errors.ErrInvalidBucketName
}

// Add context to errors
if err := store.CreateBucket(ctx, bucket); err != nil {
    return s3errors.ErrInternalError.WithMessage("failed to create bucket metadata")
}
```

### Error Wrapping

Wrap errors with context as they bubble up:

```go
if err := validateInput(data); err != nil {
    return fmt.Errorf("validation failed: %w", err)
}
```

### Logging Errors

- Log errors at the handler/boundary level (HTTP handlers, gRPC endpoints)
- Don't log the same error multiple times as it bubbles up
- Use structured logging with `zerolog`:

```go
log.Error().
    Err(err).
    Str("bucket", bucketName).
    Str("operation", "CreateBucket").
    Msg("Failed to create bucket")
```

**Don't:**

- Return AND log errors in the same function (double logging)
- Lose error context by returning `errors.New("error")` instead of wrapping
- Log errors in library/service code - return them to the caller

## Configuration Management

### Configuration Sources (Priority Order)

1. **CLI flags** (highest priority)
2. **Environment variables** (`NEBULAIO_*` prefix)
3. **YAML config file** (`config.yaml`)
4. **Defaults** (lowest priority)

### Adding New Configuration

```go
// internal/config/config.go
func loadConfig() *Config {
    // Set default
    v.SetDefault("feature.new_setting", "default_value")

    // Bind to env var (auto converts to NEBULAIO_FEATURE_NEW_SETTING)
    v.BindEnv("feature.new_setting")

    // Read from config
    cfg.Feature.NewSetting = v.GetString("feature.new_setting")
}
```

### Configuration Validation

Always validate configuration on startup:

```go
func (c *Config) Validate() error {
    if c.Feature.NewSetting == "" {
        return errors.New("feature.new_setting is required")
    }
    if c.Feature.MaxConnections < 1 {
        return errors.New("feature.max_connections must be >= 1")
    }
    return nil
}
```

**Documentation:** Update `docs/getting-started/configuration.md` for all new options

## Metrics and Observability

### Prometheus Metrics

Every feature must expose metrics following these patterns:

**Naming Convention:** `nebulaio_<component>_<metric>_<unit>`

```go
import "github.com/prometheus/client_golang/prometheus"

var (
    // Counter - for counting events
    requestsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "nebulaio_api_requests_total",
            Help: "Total number of API requests",
        },
        []string{"method", "endpoint", "status"},
    )

    // Histogram - for latencies/sizes
    requestDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "nebulaio_api_request_duration_seconds",
            Help:    "API request duration in seconds",
            Buckets: prometheus.DefBuckets,
        },
        []string{"method", "endpoint"},
    )

    // Gauge - for current values
    activeConnections = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "nebulaio_api_active_connections",
            Help: "Current number of active connections",
        },
    )
)

func init() {
    // Register metrics
    prometheus.MustRegister(requestsTotal)
    prometheus.MustRegister(requestDuration)
    prometheus.MustRegister(activeConnections)
}
```

### Structured Logging

Use `zerolog` for all logging:

```go
import "github.com/rs/zerolog/log"

// Info logging
log.Info().
    Str("bucket", bucketName).
    Int("objects", count).
    Msg("Bucket created successfully")

// Error logging
log.Error().
    Err(err).
    Str("operation", "DeleteObject").
    Str("key", objectKey).
    Msg("Failed to delete object")

// Debug logging (only in debug mode)
log.Debug().
    Interface("config", cfg).
    Msg("Configuration loaded")
```

**Don't:**

- Use `fmt.Println()` or `log.Printf()` - always use `zerolog`
- Log sensitive data (passwords, tokens, keys, user data)
- Log at high volume in hot paths - use sampling or debug level

### Health Checks

Add health checks for critical components in `internal/health/`:

```go
func (h *HealthChecker) CheckFeature(ctx context.Context) error {
    if err := h.feature.Ping(ctx); err != nil {
        return fmt.Errorf("feature unhealthy: %w", err)
    }
    return nil
}
```

### Audit Logging

Log security-relevant operations using `internal/audit/`:

```go
audit.Log(ctx, audit.Event{
    Action:   "bucket.create",
    Resource: bucketName,
    Principal: user.ID,
    Result:   "success",
    Metadata: map[string]interface{}{
        "region": region,
    },
})
```

## Performance Guidelines

### Context Usage

Always pass and respect context for cancellation and timeouts:

```go
func ProcessRequest(ctx context.Context, data []byte) error {
    // Check context before expensive operations
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }

    // Pass context to downstream calls
    result, err := service.Process(ctx, data)
    if err != nil {
        return err
    }

    return nil
}
```

### Concurrency Patterns

**Use buffered channels for async work:**

```go
// Worker pool pattern
jobs := make(chan Job, 100)
results := make(chan Result, 100)

for i := 0; i < numWorkers; i++ {
    go worker(jobs, results)
}
```

**Avoid global state and heavy mutex contention:**

```go
// Bad - global mutex
var mu sync.Mutex
var cache map[string]string

// Good - encapsulated with sync.Map or sharded locks
type Cache struct {
    data sync.Map
}
```

### Profiling

NebulaIO exposes pprof endpoints on the admin port (9001):

```bash
# CPU profile
go tool pprof http://localhost:9001/debug/pprof/profile?seconds=30

# Memory profile
go tool pprof http://localhost:9001/debug/pprof/heap

# Goroutine profile
go tool pprof http://localhost:9001/debug/pprof/goroutine
```

**Profile before optimizing** - don't guess where bottlenecks are.

## Code Organization

### Package Structure

Follow the existing structure:

```text
internal/
├── api/           # HTTP/gRPC handlers (thin, delegate to services)
├── service/       # Business logic
├── storage/       # Storage backend implementations
├── metadata/      # Metadata store (Raft)
├── auth/          # Authentication/authorization
├── config/        # Configuration management
├── metrics/       # Prometheus metrics
└── testutil/      # Shared test utilities and mocks
```

### Separation of Concerns

- **Handlers** (API layer): Parse requests, call services, format responses
- **Services**: Business logic, orchestration, validation
- **Storage**: Data persistence, low-level operations
- **Interfaces**: Define in consuming packages, implement in providing packages

**Example:**

```go
// internal/bucket/service.go
type StorageBackend interface {  // Interface in consumer
    CreateBucket(ctx context.Context, name string) error
}

type BucketService struct {
    storage StorageBackend  // Depend on interface
}

func (s *BucketService) CreateBucket(ctx context.Context, name string) error {
    // Business logic here
}

// internal/storage/fs/backend.go implements StorageBackend
```

### Circular Dependencies

**Don't:**

```go
// package A imports package B
// package B imports package A
```

**Do:**

- Extract shared interfaces to a third package
- Use dependency injection
- Consider if the dependency indicates wrong abstraction

## Git Workflow

### Branch Naming

- `feature/description` - New features
- `fix/description` - Bug fixes
- `docs/description` - Documentation updates
- `refactor/description` - Code refactoring
- `test/description` - Test improvements

### Commit Messages

Use conventional commit format:

```text
[Type] Short description (50 chars max)

Longer explanation of what and why (not how).
Can be multiple paragraphs.

Resolves #123
```

**Types:**

- `[Feature]` - New features
- `[Fix]` - Bug fixes
- `[Docs]` - Documentation
- `[Refactor]` - Code refactoring
- `[Test]` - Test additions/changes
- `[DevEx]` - Developer experience (tooling, CI/CD)
- `[Perf]` - Performance improvements
- `[Security]` - Security fixes/improvements

### Pull Request Process

1. Create feature branch from `main`
2. Make changes with frequent, logical commits
3. Pre-commit hooks run automatically (use `--no-verify` sparingly)
4. Push branch and create PR
5. Address review feedback
6. Squash merge to `main` (keeps history clean)

**PR Title:** Same format as commit messages

**PR Description:**

```markdown
## Summary
What this PR does and why

## Changes
- Bullet list of changes

## Testing
How to verify the changes work

Resolves #IssueNumber
```

## Dependency Management

### Principles

1. **Minimize dependencies**: Prefer standard library
2. **Vet dependencies**: Check maintenance, security, license
3. **Pin versions**: Use exact versions in `go.mod`
4. **Keep updated**: Regular security updates via `go get -u`

### Adding Dependencies

**Before adding a new dependency, ask:**

- Can this be done with the standard library?
- Is this dependency actively maintained?
- What's the license? (Prefer MIT, Apache 2.0, BSD)
- What's the security track record?
- How many transitive dependencies does it add?

### Keeping go.mod Clean

```bash
# After adding imports
go mod tidy

# Verify no changes (in CI or pre-commit)
git diff --exit-code go.mod go.sum
```

**Pre-commit hook enforces this** - `go mod tidy` runs on any .go file change

### Major Dependencies

Document why each major dependency exists:

- **Dragonboat**: Raft consensus for distributed metadata
- **Viper**: Configuration management (YAML, env, flags)
- **Zerolog**: Fast structured logging
- **Prometheus**: Metrics and monitoring
- **Testify**: Testing framework with mocks and assertions

## Feature Flags (Environment Variables)

Advanced features are disabled by default and enabled via config:

- `NEBULAIO_S3_EXPRESS_ENABLED` - S3 Express One Zone
- `NEBULAIO_ICEBERG_ENABLED` - Apache Iceberg tables
- `NEBULAIO_MCP_ENABLED` - MCP Server for AI agents
- `NEBULAIO_GPUDIRECT_ENABLED` - GPUDirect Storage
- `NEBULAIO_RDMA_ENABLED` - S3 over RDMA

## Development Checklist (MANDATORY)

After implementing or extending ANY feature, complete ALL of the following:

### 1. Documentation Updates

- [ ] `docs/getting-started/configuration.md` - Add new config options
- [ ] `docs/features/` - Create or update feature documentation
- [ ] `docs/architecture/` - Update if architectural changes
- [ ] `README.md` - Update feature list if user-facing

### 2. Deployment Configurations

- [ ] `deployments/helm/nebulaio/values.yaml` - Add Helm values
- [ ] `deployments/kubernetes/base/configmap.yaml` - Add K8s config
- [ ] `docker-compose.yml` - Add environment variables
- [ ] `docker-compose.production.yml` - Production settings

### 3. Frontend Integration (REQUIRED for all backend features)

- [ ] API client in `web/src/api/` - Add endpoint calls
- [ ] Page in `web/src/pages/` - Create configuration/management page
- [ ] Navigation - Add to sidebar if new section
- [ ] Settings page - Add configuration UI if feature has settings
- [ ] Dashboard integration - Show status/metrics if applicable

### 4. Telemetry & Observability

- [ ] Prometheus metrics (`internal/metrics/`) - Add counters, gauges, histograms
- [ ] Structured logging - Add relevant log events with context
- [ ] Health checks (`internal/health/`) - Include in health status if critical
- [ ] Audit logging (`internal/audit/`) - Log security-relevant operations

### 5. Testing

- [ ] Unit tests for new code
- [ ] Integration tests if cross-component
- [ ] Frontend tests (Vitest) for new UI

### 6. Security (Secure by Default)

- [ ] TLS/HTTPS - All new endpoints must use TLS (enabled by default)
- [ ] Authentication - All APIs require auth unless explicitly public
- [ ] Authorization - Check IAM policies for resource access
- [ ] Input validation - Validate and sanitize all inputs
- [ ] Audit logging - Log security-sensitive operations
- [ ] No secrets in code - Use config/env vars for sensitive data

## Security Principles

NebulaIO is **secure by default**:

1. **TLS Everywhere**: All servers use HTTPS. Certificates auto-generate if not provided.
2. **Auth Required**: All API endpoints require authentication by default.
3. **Least Privilege**: Users get minimal permissions; explicit grants required.
4. **Audit Everything**: Security events are logged with cryptographic integrity.
5. **Defense in Depth**: Multiple security layers (TLS, auth, policies, rate limiting).

When adding features:

- NEVER disable security for convenience
- NEVER log sensitive data (passwords, tokens, keys)
- ALWAYS validate user input
- ALWAYS check authorization before operations
- ALWAYS use parameterized queries/operations

## Metrics Standards

Every feature should expose metrics:

```go

// Counter - for events
requestsTotal := prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "nebulaio_feature_requests_total",
        Help: "Total feature requests",
    },
    []string{"operation", "status"},
)

// Histogram - for latencies
requestDuration := prometheus.NewHistogramVec(
    prometheus.HistogramOpts{
        Name:    "nebulaio_feature_request_duration_seconds",
        Help:    "Request duration",
        Buckets: prometheus.DefBuckets,
    },
    []string{"operation"},
)

// Gauge - for current state
activeConnections := prometheus.NewGauge(
    prometheus.GaugeOpts{
        Name: "nebulaio_feature_active_connections",
        Help: "Current active connections",
    },
)

```bash

## Frontend Page Template

New features requiring configuration should have a settings page:

```text

web/src/pages/
├── FeatureNamePage.tsx      # Main feature page
└── settings/
    └── FeatureSettings.tsx  # Configuration UI

```

Include:

- Enable/disable toggle
- Configuration options with validation
- Status indicators
- Metrics/statistics display
- Action buttons (test connection, apply, etc.)
