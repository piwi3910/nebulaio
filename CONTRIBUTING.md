# Contributing to NebulaIO

Welcome to NebulaIO! We are thrilled that you are interested in contributing to this S3-compatible object storage system. Whether you are fixing a bug, adding a feature, improving documentation, or suggesting ideas, your contributions are valued and appreciated.

This document provides guidelines to help you contribute effectively.

## Code of Conduct

We are committed to providing a welcoming and inclusive environment for everyone. By participating in this project, you agree to:

- Be respectful and considerate in all interactions
- Welcome newcomers and help them get started
- Focus on constructive feedback and collaboration
- Accept responsibility for your mistakes and learn from them
- Prioritize the health of the community over individual interests

Harassment, discrimination, and disrespectful behavior will not be tolerated. If you witness or experience unacceptable behavior, please report it to the project maintainers.

## How to Contribute

### Reporting Issues

If you find a bug or have a feature request:

1. **Search existing issues** to avoid duplicates
2. **Create a new issue** with a clear, descriptive title
3. **Provide context**:
   - For bugs: steps to reproduce, expected vs. actual behavior, environment details
   - For features: use case, proposed solution, alternatives considered
4. **Add appropriate labels** (bug, enhancement, documentation, etc.)

### Submitting Pull Requests

1. **Fork the repository** and create a feature branch from `main`
2. **Use descriptive branch names**: `fix-multipart-upload-timeout` or `add-bucket-policy-support`
3. **Make focused changes** - one logical change per PR
4. **Write tests** for new functionality
5. **Update documentation** if your changes affect usage or APIs
6. **Ensure all checks pass** before requesting review

## Development Setup

### Prerequisites

- **Go 1.23+** - Download from [golang.org](https://golang.org/dl/)
- **Node.js 20+** - For web console development
- **Make** - For running build commands
- **Git** - For version control
- **Docker** (optional) - For containerized development

### Building from Source

```bash
# Clone the repository
git clone https://github.com/piwi3910/nebulaio.git
cd nebulaio

# Download Go dependencies
make deps

# Build the binary
make build

# The binary will be at ./bin/nebulaio
```

### Building the Web Console

```bash
# Install Node.js dependencies
make web

# Build the web console
make web-build

# Or for development with hot reload
make web-dev
```

### Running the Server

```bash
# Run in debug mode
make run

# Or run the binary directly
./bin/nebulaio --debug
```

The server exposes three ports:

- **9000**: S3 API
- **9001**: Admin API
- **9002**: Web Console

### Running Tests

```bash
# Run all Go tests
make test

# Run tests with coverage report
make test-coverage

# Run web console tests
cd web && npm test

# Run web console tests with coverage
cd web && npm run test:coverage
```

### Linting and Formatting

```bash
# Run Go linter
make lint

# Format Go code
make fmt

# Run go vet
make vet

# Lint web console
cd web && npm run lint

# Fix linting issues in web console
cd web && npm run lint:fix

# Type check web console
cd web && npm run type-check
```

## Code Style Guidelines

### Go Code Style

We follow standard Go conventions with these specifics:

- **Package names**: lowercase, single word when possible
- **Exported types**: PascalCase (`Handler`, `BucketService`)
- **Unexported**: camelCase (`parseRequest`, `validateInput`)
- **Constants**: PascalCase or SCREAMING_SNAKE_CASE for groups
- **Interfaces**: Use -er suffix for behaviors (`Store`, `Validator`)

**Handler pattern:**

```go
type Handler struct {
    service *bucket.Service
    store   metadata.Store
}

func NewHandler(service *bucket.Service, store metadata.Store) *Handler {
    return &Handler{service: service, store: store}
}

func (h *Handler) CreateBucket(w http.ResponseWriter, r *http.Request) {
    // Implementation
}
```

**Error handling:**

```go
if err != nil {
    return fmt.Errorf("failed to create bucket: %w", err)
}
```

### TypeScript/React Code Style

- **Function components**: Always use typed props with interfaces
- **Hooks**: Use React Query for server state, Zustand for client state
- **Naming**: PascalCase for components, camelCase for functions/variables
- **Imports**: Group external imports, then internal, then types

**Component pattern:**

```typescript
interface BucketListProps {
  onSelect: (bucket: Bucket) => void;
  filter?: string;
}

export function BucketList({ onSelect, filter }: BucketListProps) {
  // Implementation
}
```

## Commit Message Format

We follow a structured commit message format:

```
[Type] Short summary (50 chars or less)

More detailed explanation if needed. Wrap at 72 characters.
Explain the what and why, not the how.

Resolves #123
```

**Types:**

- `[Feature]` - New functionality
- `[Fix]` - Bug fixes
- `[Refactor]` - Code restructuring without behavior change
- `[Docs]` - Documentation updates
- `[Test]` - Test additions or modifications
- `[Chore]` - Maintenance tasks, dependency updates

**Examples:**

```
[Feature] Add bucket versioning support

Implements S3-compatible versioning for buckets. Users can now
enable versioning per bucket and retrieve previous object versions.

Resolves #42
```

```
[Fix] Correct multipart upload part ordering

Parts were being assembled in upload order rather than part number
order, causing corrupted files for parallel uploads.

Resolves #87
```

## Pull Request Process

1. **Create your PR** with a clear title and description
2. **Fill out the PR template** with:
   - Summary of changes
   - Related issue numbers
   - Testing performed
   - Screenshots for UI changes
3. **Ensure CI passes** - all tests and linting must succeed
4. **Request review** from maintainers
5. **Address feedback** - respond to comments and make requested changes
6. **Squash commits** if requested for a cleaner history

### PR Checklist

Before submitting, verify:

- [ ] Code compiles without errors
- [ ] All tests pass (`make test` and `cd web && npm test`)
- [ ] Linting passes (`make lint` and `cd web && npm run lint`)
- [ ] New code has appropriate test coverage
- [ ] Documentation is updated if needed
- [ ] Commit messages follow the format
- [ ] PR description explains the changes

## Release Process

NebulaIO follows semantic versioning (MAJOR.MINOR.PATCH):

1. **MAJOR**: Breaking changes to S3 API compatibility
2. **MINOR**: New features, backward compatible
3. **PATCH**: Bug fixes, backward compatible

### Creating a Release

Releases are created by maintainers:

```bash
# Create release binaries
make release

# Tag the release
git tag -a v1.2.0 -m "Release v1.2.0"
git push origin v1.2.0
```

Release binaries are built for:

- Linux (amd64)
- macOS (arm64)

Docker images are published to the container registry with each release.

## Getting Help

If you need assistance:

- **GitHub Issues**: For bugs and feature requests
- **GitHub Discussions**: For questions and general discussion
- **Documentation**: Check the `/docs` directory for guides

### Tips for New Contributors

1. Start with issues labeled `good first issue`
2. Read through existing code to understand patterns
3. Ask questions early - we are happy to help
4. Small, focused PRs are easier to review and merge

## Recognition

Contributors are recognized in:

- Release notes for significant contributions
- The project README for major features

Thank you for contributing to NebulaIO!
