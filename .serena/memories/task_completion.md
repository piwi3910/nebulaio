# Task Completion Checklist

When completing a task, ensure the following:

1. **Run linter**: `make lint`
2. **Run vet**: `make vet`
3. **Run tests**: `make test`
4. **Format code**: `make fmt`

## Before Committing

- Ensure all linting passes
- Ensure all tests pass
- Code is properly formatted
- XML types in `pkg/s3types/types.go` match S3 API spec
- Metadata types in `internal/metadata/store.go` are consistent

## Key Files to Update When Adding Features

- `internal/metadata/store.go` - Store interface and types
- `internal/metadata/raft_store.go` - Raft store implementation
- `internal/metadata/raft.go` - Raft FSM commands
- `pkg/s3types/types.go` - XML types for API responses
- `internal/api/s3/handler.go` - S3 API handlers
