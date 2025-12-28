# Task Completion Checklist

## When Completing a Task

### Code Quality

- [ ] Run `make fmt` to format code
- [ ] Run `make lint` to check for linting issues
- [ ] Run `make vet` to check for common errors
- [ ] Run `make test` to verify tests pass

### Syntax Verification

```bash
# Quick syntax check for modified files
gofmt -e ./path/to/modified/*.go

# Build specific packages to verify compilation
go build ./internal/package/...
```

### Documentation

- [ ] Update godoc comments for new exported functions
- [ ] Update API documentation if endpoints changed
- [ ] Update README if major features added

### Version Control

- [ ] Stage only relevant changes
- [ ] Write clear commit messages
- [ ] Reference issue numbers if applicable

## Common Issues to Check

### Before Committing Auth/Security Changes

- [ ] Validate credentials are not exposed
- [ ] Check error messages don't leak sensitive info
- [ ] Verify authorization checks are in place

### Before Committing API Changes

- [ ] Endpoints return proper HTTP status codes
- [ ] Error responses are consistent
- [ ] Request validation is thorough

### Known Build Issues

The project may have existing issues in other packages. If you see errors in:

- `internal/object/service.go` - duplicate method declarations
- `internal/lifecycle/` - signature mismatches

These are pre-existing and not related to new changes. Focus on verifying your changes compile independently:

```bash
go build ./internal/auth/...
go build ./internal/api/middleware/...
```
