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

## Go Linting Rules - Write Compliant Code First Time

**CRITICAL**: ALL linters are enabled in `.golangci.yml`. Write code that complies with these rules from the start to avoid rework.

### HTTP Headers (canonicalheader)

**ALWAYS use canonical HTTP header format (Title-Case):**

```go
// ✅ CORRECT - Canonical format
req.Header.Set("Content-Type", "application/json")
req.Header.Set("X-User-Id", userID)              // "Id" not "ID"
req.Header.Set("X-Request-Id", requestID)        // "Id" not "ID"
req.Header.Set("X-Api-Key", apiKey)              // "Api" not "API"
req.Header.Set("X-Amz-Version-Id", versionID)    // AWS headers
req.Header.Get("Authorization")

// ❌ WRONG - Non-canonical
req.Header.Set("content-type", "application/json")     // lowercase
req.Header.Set("x-user-id", userID)                    // lowercase
req.Header.Set("X-User-ID", userID)                    // "ID" should be "Id"
req.Header.Set("X-API-Key", apiKey)                    // "API" should be "Api"
req.Header.Set("x-amz-version-id", versionID)          // lowercase
```

**Common patterns:**

- Content-Type, Content-Length, Content-Encoding, Content-Disposition
- Authorization, Accept, User-Agent, Referer
- X-User-Id, X-Request-Id, X-Api-Key (use "Id", "Api" not "ID", "API")
- X-Amz-* headers: X-Amz-Version-Id, X-Amz-Request-Id, X-Amz-Delete-Marker

### Line Length (lll)

Max line length: 120 characters

```go
// ✅ CORRECT
func CreateBucket(ctx context.Context, name string,
    region string, opts *Options) error {
    return bucketService.Create(ctx, name, region, opts)
}

// ❌ WRONG - line too long
func CreateBucket(ctx context.Context, name string, region string, opts *Options) error { return bucketService.Create(ctx, name, region, opts) }
```

### Variable Naming (varnamelen)

**Variable names must match their scope (min 2 chars, context-aware):**

```go
// ✅ CORRECT - standard short names allowed
for i := 0; i < len(items); i++ { }
for _, v := range items { }
ctx := context.Background()
mu := &sync.Mutex{}
wg := &sync.WaitGroup{}

// ✅ CORRECT - longer scope needs descriptive names
func ProcessUserData(ctx context.Context) {
    userID := getUserID(ctx)           // descriptive
    requestData := parseRequest()     // descriptive
}

// ❌ WRONG - single letter for long scope
func ProcessData(ctx context.Context) {
    u := getUserID(ctx)      // too short for this scope
    d := parseRequest()      // too short for this scope
}
```

### Struct Tag Format (tagliatelle)

Use correct case for struct tags:

- `json`: snake_case
- `yaml`: snake_case
- `xml`: camelCase
- `bson`: camelCase

```go
// ✅ CORRECT
type User struct {
    UserID   string `json:"user_id" yaml:"user_id" xml:"userId" bson:"userId"`
    FullName string `json:"full_name" yaml:"full_name"`
    Email    string `json:"email"`
}

// ❌ WRONG - wrong case
type User struct {
    UserID   string `json:"userId"`      // should be snake_case
    FullName string `json:"FullName"`    // should be snake_case
}
```

### Error Wrapping (wrapcheck)

**Wrap errors from external packages:**

```go
// ✅ CORRECT - wrap external errors
import "github.com/external/pkg"

func DoWork() error {
    if err := pkg.Execute(); err != nil {
        return fmt.Errorf("failed to execute: %w", err)
    }
    return nil
}

// ✅ CORRECT - internal package errors can be returned directly
import "github.com/piwi3910/nebulaio/internal/storage"

func DoWork() error {
    return storage.Save(data)  // internal, no wrap needed
}

// ❌ WRONG - returning external error unwrapped
func DoWork() error {
    return pkg.Execute()  // external package, should wrap
}
```

### Exhaustive Switch (exhaustive)

**Handle all enum cases in switch statements:**

```go
type Status int
const (
    StatusPending Status = iota
    StatusActive
    StatusInactive
)

// ✅ CORRECT - all cases handled
func ProcessStatus(s Status) string {
    switch s {
    case StatusPending:
        return "pending"
    case StatusActive:
        return "active"
    case StatusInactive:
        return "inactive"
    default:
        return "unknown"  // default signifies exhaustive
    }
}

// ❌ WRONG - missing case
func ProcessStatus(s Status) string {
    switch s {
    case StatusPending:
        return "pending"
    case StatusActive:
        return "active"
    // missing StatusInactive case
    }
    return ""
}
```

### Struct Initialization (exhaustruct)

**Initialize important struct fields explicitly (excluded in tests):**

```go
// ✅ CORRECT - critical fields initialized
user := &User{
    ID:        uuid.New(),
    Email:     email,
    CreatedAt: time.Now(),
    UpdatedAt: time.Now(),
    Active:    true,
}

// ✅ CORRECT - partial init OK for simple structs
opts := &Options{
    Timeout: 30 * time.Second,
    // other fields have sensible defaults
}

// Note: exhaustruct is excluded in test files
```

### Whitespace and Formatting (wsl_v5, nlreturn)

**Consistent whitespace and newlines:**

```go
// ✅ CORRECT - newline before return/branch
func Process() error {
    if err := validate(); err != nil {
        return err
    }

    result := compute()

    return save(result)
}

// ✅ CORRECT - logical grouping
func CreateUser(name, email string) error {
    user := &User{Name: name}
    user.Email = email

    if err := validate(user); err != nil {
        return err
    }

    return save(user)
}

// ❌ WRONG - no separation
func Process() error {
    if err := validate(); err != nil {
        return err
    }
    result := compute()
    return save(result)  // missing newline before return
}
```

### No Forbidden Functions (forbidigo)

**Use zerolog for logging, return errors instead of panic:**

```go
// ✅ CORRECT - use zerolog
import "github.com/rs/zerolog/log"

func Process() error {
    log.Info().Msg("Processing started")
    log.Error().Err(err).Msg("Processing failed")
    return err
}

// ✅ CORRECT - return errors
func Process() error {
    if invalid {
        return errors.New("invalid input")
    }
    return nil
}

// ❌ WRONG - fmt.Print
func Process() {
    fmt.Println("Processing...")  // use zerolog
}

// ❌ WRONG - panic
func Process() {
    panic("something went wrong")  // return error instead
}
```

### Function/Method Order (funcorder)

**Order functions logically:**

```go
// ✅ CORRECT order:
// 1. Exported functions
// 2. Unexported functions
// 3. Receivers grouped together
// 4. Helper functions at bottom

// Exported constructor
func NewService() *Service { }

// Exported methods
func (s *Service) Start() error { }
func (s *Service) Stop() error { }

// Unexported methods
func (s *Service) process() error { }

// Helper functions
func validate(data []byte) error { }
func compute(x int) int { }
```

### Magic Numbers (mnd)

**Define constants for magic numbers:**

```go
// ✅ CORRECT - constants
const (
    DefaultTimeout = 30 * time.Second
    MaxRetries     = 3
    BufferSize     = 1024
)

func Process() {
    timeout := DefaultTimeout
    retries := MaxRetries
}

// ❌ WRONG - magic numbers
func Process() {
    timeout := 30 * time.Second  // what's 30?
    retries := 3                 // what's 3?
    buffer := make([]byte, 1024) // what's 1024?
}
```

### Test Parallelization (paralleltest, tparallel)

**Use t.Parallel() appropriately (excluded in test files, but good practice):**

```go
// ✅ CORRECT - parallel tests where safe
func TestUserCreation(t *testing.T) {
    t.Parallel()

    // test logic
}

// ✅ CORRECT - don't parallelize when sharing state
func TestDatabaseOperations(t *testing.T) {
    // NOT parallel - shares database connection

    // test logic
}
```

### Test Package Separation (testpackage)

**Use separate test package for black-box testing:**

```go
// ✅ CORRECT - black-box test
// file: bucket_test.go
package bucket_test  // separate package

import (
    "testing"
    "github.com/piwi3910/nebulaio/internal/bucket"
)

func TestCreateBucket(t *testing.T) {
    // test using public API only
}

// ✅ ALSO CORRECT - white-box test when needed
// file: bucket_internal_test.go
package bucket  // same package

func TestInternalLogic(t *testing.T) {
    // test internal/private functions
}
```

### Global Variables (gochecknoglobals)

**Avoid global variables except where necessary:**

```go
// ✅ CORRECT - metrics, loggers allowed
var (
    requestsTotal = prometheus.NewCounterVec(...)
    log           = zerolog.New(os.Stderr)
)

// ✅ CORRECT - config package
package config
var globalConfig *Config  // allowed in config package

// ✅ CORRECT - CLI flags in cmd
package main
var (
    flagPort = flag.Int("port", 9000, "port")
    flagHost = flag.String("host", "localhost", "host")
)

// ❌ WRONG - global state in business logic
package service
var cache = make(map[string]string)  // use struct field instead
```

### Init Functions (gochecknoinits)

**Minimize use of init(), use explicit initialization:**

```go
// ✅ CORRECT - explicit initialization
func NewService() *Service {
    s := &Service{}
    s.initialize()
    return s
}

// ✅ ALLOWED - registration patterns
func init() {
    prometheus.MustRegister(requestsTotal)  // metrics registration
    sql.Register("postgres", &driver{})      // driver registration
}

// ❌ WRONG - complex logic in init
func init() {
    config = loadConfig()      // do this in main() or New()
    db = connectDatabase()     // do this in main() or New()
}
```

### Nested If Statements (nestif)

Max nesting: 4 levels

```go
// ✅ CORRECT - early returns reduce nesting
func Process(data []byte) error {
    if data == nil {
        return errors.New("nil data")
    }

    user, err := parseUser(data)
    if err != nil {
        return err
    }

    if !user.Valid {
        return errors.New("invalid user")
    }

    return save(user)
}

// ❌ WRONG - deep nesting
func Process(data []byte) error {
    if data != nil {
        if user, err := parseUser(data); err == nil {
            if user.Valid {
                if err := save(user); err == nil {
                    return nil  // 4 levels deep
                }
            }
        }
    }
    return errors.New("failed")
}
```

### Cyclomatic Complexity (cyclop, gocyclo, gocognit)

Keep functions simple:

- Max cyclomatic complexity: 15
- Max cognitive complexity: 20

```go
// ✅ CORRECT - extract complex logic
func ProcessRequest(ctx context.Context, req *Request) error {
    if err := validateRequest(req); err != nil {
        return err
    }

    result, err := performOperation(ctx, req)
    if err != nil {
        return err
    }

    return saveResult(ctx, result)
}

func validateRequest(req *Request) error {
    // validation logic
}

func performOperation(ctx context.Context, req *Request) (*Result, error) {
    // operation logic
}

// ❌ WRONG - too complex
func ProcessRequest(ctx context.Context, req *Request) error {
    // 20+ if statements, switches, loops all in one function
}
```

### Context in Structs (containedctx)

**Don't store context in structs:**

```go
// ✅ CORRECT - pass context as parameter
type Worker struct {
    db *sql.DB
}

func (w *Worker) Process(ctx context.Context) error {
    return w.db.QueryContext(ctx, "...")
}

// ❌ WRONG - context in struct
type Worker struct {
    ctx context.Context  // DON'T DO THIS
    db  *sql.DB
}
```

### Context Propagation (contextcheck)

**Always pass context through call chains:**

```go
// ✅ CORRECT - context passed through
func ProcessRequest(ctx context.Context, data []byte) error {
    return processData(ctx, data)
}

func processData(ctx context.Context, data []byte) error {
    return saveToDatabase(ctx, data)
}

// ❌ WRONG - breaking context chain
func ProcessRequest(ctx context.Context, data []byte) error {
    return processData(data)  // missing ctx
}
```

### Unused Parameters (unparam)

**Remove or acknowledge unused parameters:**

```go
// ✅ CORRECT - all params used
func Process(ctx context.Context, data []byte) error {
    return db.QueryContext(ctx, string(data))
}

// ✅ CORRECT - use _ for intentionally unused
func Handler(w http.ResponseWriter, r *http.Request, _ context.Context) {
    // ctx not needed in this handler
}

// ❌ WRONG - unused parameter
func Process(ctx context.Context, data []byte) error {
    return db.Query(string(data))  // ctx never used
}
```

### Struct Tag Alignment (tagalign)

**Align struct tags for readability:**

```go
// ✅ CORRECT - aligned tags
type User struct {
    ID        string    `json:"id"         db:"id"`
    Name      string    `json:"name"       db:"name"`
    Email     string    `json:"email"      db:"email"`
    CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// ❌ WRONG - misaligned tags
type User struct {
    ID string `json:"id" db:"id"`
    Name string `json:"name" db:"name"`
    Email string `json:"email" db:"email"`
}
```

### Dependencies (depguard)

**Follow dependency rules (if configured):**

```go
// ✅ CORRECT - use allowed packages
import (
    "context"
    "github.com/rs/zerolog/log"
    "github.com/piwi3910/nebulaio/internal/storage"
)

// ❌ WRONG - if blocked by depguard
import "some/blocked/package"
```

### Pre-Commit Checklist

**Before writing ANY Go code, ensure you:**

1. ✅ Use canonical HTTP header format (Title-Case)
2. ✅ Keep lines under 120 characters
3. ✅ Use descriptive variable names (min 2 chars, scope-appropriate)
4. ✅ Use correct struct tag format (json: snake_case)
5. ✅ Wrap errors from external packages
6. ✅ Handle all enum cases in switches
7. ✅ Use zerolog for logging, not fmt.Print
8. ✅ Return errors, don't panic
9. ✅ Pass context through call chains
10. ✅ Keep function complexity low (max 15 cyclomatic)
11. ✅ Don't store context in structs
12. ✅ Define constants for magic numbers
13. ✅ Avoid global variables (except metrics, config, flags)
14. ✅ Remove unused parameters
15. ✅ Run `make lint` and fix all issues before committing

### Quick Reference

```bash
# Run linter
make lint

# All linters are enabled - write compliant code from the start!
```

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
