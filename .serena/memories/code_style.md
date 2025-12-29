# NebulaIO Code Style and Conventions

## Go Code Style

### Package Organization

- Each feature domain has its own package under `internal/`
- API handlers are in `internal/api/{area}/handler.go`
- Middleware is in `internal/api/middleware/`
- Services encapsulate business logic
- Types and interfaces are defined close to their usage

### Naming Conventions

- **Packages**: lowercase, single word when possible
- **Types/Structs**: PascalCase (e.g., `Handler`, `PresignParams`)
- **Functions/Methods**: PascalCase for exported, camelCase for unexported
- **Variables**: camelCase
- **Constants**: PascalCase or SCREAMING_SNAKE_CASE for groups
- **Interfaces**: Use -er suffix when describing behavior (e.g., `Store`, `Service`)

### Struct Patterns

```go

// Handler pattern used across API packages
type Handler struct {
    auth   *auth.Service
    bucket *bucket.Service
    object *object.Service
    store  metadata.Store
}

// Constructor pattern
func NewHandler(...) *Handler {
    return &Handler{...}
}

```bash

### HTTP Handler Pattern

```go

func (h *Handler) SomeEndpoint(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    // Parse request
    var req RequestType
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, "Invalid request body", http.StatusBadRequest)
        return
    }

    // Validate
    if req.Field == "" {
        writeError(w, "Field is required", http.StatusBadRequest)
        return
    }

    // Business logic
    result, err := h.service.DoSomething(ctx, req)
    if err != nil {
        writeError(w, err.Error(), http.StatusInternalServerError)
        return
    }

    // Response
    writeJSON(w, http.StatusOK, result)
}

```bash

### Request/Response Types

```go

// Request types with json tags
type CreateSomethingRequest struct {
    Name        string `json:"name"`
    Description string `json:"description,omitempty"`
}

// Response types
type SomethingResponse struct {
    ID        string    `json:"id"`
    Name      string    `json:"name"`
    CreatedAt time.Time `json:"created_at"`
}

```bash

### Error Handling

- Return errors up the call stack
- Use `fmt.Errorf("context: %w", err)` for wrapping
- HTTP handlers convert errors to appropriate status codes
- Use sentinel errors for specific conditions

### Router Registration Pattern

```go

func (h *Handler) RegisterRoutes(r chi.Router) {
    r.Use(h.authMiddleware)

    r.Get("/resource", h.ListResources)
    r.Post("/resource", h.CreateResource)
    r.Get("/resource/{id}", h.GetResource)
    r.Put("/resource/{id}", h.UpdateResource)
    r.Delete("/resource/{id}", h.DeleteResource)
}

```bash

### Middleware Pattern

```go

func SomeMiddleware(config Config) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Pre-processing
            ctx := r.Context()

            // Add to context if needed
            ctx = context.WithValue(ctx, key, value)

            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

```bash

### Helper Functions

```go

// JSON response helper
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(v)
}

// Error response helper
func writeError(w http.ResponseWriter, message string, status int) {
    writeJSON(w, status, map[string]string{"error": message})
}

```

## Testing Conventions

- Test files: `*_test.go` in same package
- Use table-driven tests when appropriate
- Mock external dependencies

## Comments

- Use godoc-style comments for exported items
- Start with the name of the item
- Document behavior, not implementation
