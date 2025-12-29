# Contributing to NebulaIO

Thank you for your interest in contributing to NebulaIO! This guide will help you get started with development.

## Development Setup

### Prerequisites

- **Go**: 1.24 or later
- **Node.js**: 20 or later (for web console)
- **Docker**: For running with Docker Compose (optional)
- **pre-commit**: For automatic code quality checks (recommended)

### Initial Setup

1. **Clone the repository**:

   ```bash

   git clone https://github.com/piwi3910/nebulaio.git
   cd nebulaio

   ```text

2. **Install dependencies**:

   ```bash

   # Go dependencies
   make deps

   # Web console dependencies (optional)
   make web

   ```text

3. **Install pre-commit hooks** (highly recommended):

   ```bash

   # Install pre-commit (choose one):
   pip install pre-commit
   # OR on macOS:
   brew install pre-commit

   # Install the hooks
   make install-hooks

   ```bash

## Pre-commit Hooks

We use pre-commit hooks to catch issues **before** they're committed, saving you time and keeping the CI pipeline green.

### What gets checked?

The pre-commit hooks automatically run:

**Backend (Go):**

- **golangci-lint**: Go code linting (same as CI)
- **gofmt**: Go code formatting
- **go mod tidy**: Ensure go.mod/go.sum is clean (runs on any .go file change)

**Frontend (Web Console):**

- **ESLint**: JavaScript/TypeScript linting
- **TypeScript**: Type checking (only when .ts/.tsx files changed)

**Documentation:**

- **markdownlint**: Markdown formatting and style

**General:**

- **trailing whitespace**: Remove trailing spaces
- **YAML/JSON validation**: Check syntax
- **merge conflicts**: Detect conflict markers
- **large files**: Prevent files >1MB from being committed

### How it works

When you run `git commit`, the hooks automatically:

1. Run linting on **only the files you're committing** (fast!)
2. Auto-fix simple issues (formatting, trailing spaces)
3. Block the commit if there are errors
4. Show you exactly what needs to be fixed

### Example workflow

```bash

# Make your changes
vim internal/bucket/service.go

# Stage changes
git add internal/bucket/service.go

# Commit (hooks run automatically)
git commit -m "feat: add bucket validation"

# If hooks pass:
# ✓ All checks passed!
# Commit successful

# If hooks fail:
# ✗ golangci-lint found 2 issues
# Fix the issues, stage again, and recommit

```bash

### Commands

```bash

# Install hooks
make install-hooks

# Run hooks manually on all files (useful for testing)
make run-hooks

# Run hooks on staged files only
pre-commit run

# Skip hooks for urgent commits (use sparingly!)
git commit --no-verify

```bash

### Troubleshooting

**Hooks not running?**

- Ensure you ran `make install-hooks`
- Check `.git/hooks/pre-commit` exists and is executable

**Web hooks failing with "command not found"?**

- You need Node.js dependencies installed: `cd web && npm install`
- Web hooks (ESLint, TypeScript) only run when you modify web files
- If you don't work on the frontend, these hooks won't affect you

**Hooks too slow?**

- They only check staged files, not the entire codebase
- First run downloads tools (cached for future runs)
- Typical run time: 5-30 seconds
- Web hooks add ~5-10 seconds when modifying frontend files

**Need to bypass hooks temporarily?**

```bash

git commit --no-verify -m "your message"

```text

*Use sparingly - you'll still need to fix issues for CI to pass!*

**Large file size limit:**

- Files >1MB are blocked by default
- This prevents accidental commits of binaries, videos, or large datasets
- If you need to commit a legitimate large file, use `--no-verify` or adjust `.pre-commit-config.yaml`

## Code Quality Standards

### Go Code

- Follow standard Go formatting (`gofmt`)
- Pass all golangci-lint checks (no `//nolint` comments)
- Write tests for new functionality
- Maintain or improve test coverage
- Document exported functions and types

### Markdown Documentation

- Use proper heading hierarchy
- Wrap long lines for readability
- Include code examples where helpful
- Run `markdownlint --fix` to auto-format

### Git Commits

- Write clear, descriptive commit messages
- Use conventional commit format when possible:
  - `feat:` - New features
  - `fix:` - Bug fixes
  - `docs:` - Documentation changes
  - `refactor:` - Code refactoring
  - `test:` - Test additions/changes
  - `chore:` - Build/tooling changes

## Testing

```bash

# Run all tests
make test

# Run tests with coverage
make test-coverage

# Run specific package tests
go test -v ./internal/bucket/

# Run specific test
go test -v -run TestBucketValidation ./internal/bucket/

```bash

## Building

```bash

# Build binary
make build

# Build for specific platforms
make build-linux
make build-darwin
make build-all

# Run locally
make run

# Development mode with hot reload (requires air)
make dev

```

## Pull Request Process

1. **Create a feature branch**: `git checkout -b feature/your-feature`
2. **Make your changes** with frequent, logical commits
3. **Ensure hooks pass**: They run automatically on commit
4. **Run tests**: `make test` to ensure nothing broke
5. **Update documentation**: If you changed APIs or added features
6. **Push your branch**: `git push origin feature/your-feature`
7. **Create a Pull Request** with:
   - Clear description of changes
   - Link to related issues
   - Screenshots for UI changes
   - Test plan or steps to verify

## Getting Help

- **Documentation**: Check `/docs` directory
- **Issues**: Browse existing [GitHub issues](https://github.com/piwi3910/nebulaio/issues)
- **Questions**: Open a new issue with the `question` label

## Code of Conduct

Be respectful, inclusive, and collaborative. We're all here to build something great together!

---

Thank you for contributing to NebulaIO! 🚀
test
