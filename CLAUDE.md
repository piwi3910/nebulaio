# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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
```

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
```

### Docker

```bash
docker-compose up -d                    # Start with docker-compose
docker-compose -f docker-compose.yml -f docker-compose.production.yml up -d  # Production
```

### Running the Server

```bash
./bin/nebulaio --debug                  # Debug mode
./bin/nebulaio --data ./data --s3-port 9000 --admin-port 9001
```

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

```
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
```

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
```

## Frontend Page Template

New features requiring configuration should have a settings page:

```
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
