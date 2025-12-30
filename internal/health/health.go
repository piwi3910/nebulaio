// Package health provides health check endpoints for NebulaIO.
//
// The package implements Kubernetes-compatible health checks:
//
//   - /health/live: Liveness probe (is the process running?)
//   - /health/ready: Readiness probe (is the server ready for traffic?)
//   - /health/startup: Startup probe (has initialization completed?)
//
// Each check returns JSON status with component health details:
//
//	{
//	  "status": "healthy",
//	  "checks": {
//	    "metadata": "healthy",
//	    "storage": "healthy",
//	    "raft": "healthy"
//	  }
//	}
//
// Use these endpoints with container orchestrators for automatic restart
// and traffic routing based on service health.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/internal/storage/backend"
)

// Status represents the overall health status.
type Status string

const (
	// StatusHealthy indicates all checks passed.
	StatusHealthy Status = "healthy"
	// StatusDegraded indicates some checks failed but core functionality works.
	StatusDegraded Status = "degraded"
	// StatusUnhealthy indicates critical failures.
	StatusUnhealthy Status = "unhealthy"
)

// Check represents a single health check result.
type Check struct {
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

// HealthStatus represents the complete health status of the system.
type HealthStatus struct {
	Timestamp time.Time        `json:"timestamp"`
	Checks    map[string]Check `json:"checks"`
	Status    Status           `json:"status"`
}

// Checker performs health checks on the system.
type Checker struct {
	cacheExpiry  time.Time
	store        metadata.Store
	storage      backend.Backend
	cachedStatus *HealthStatus
	cacheTTL     time.Duration
	mu           sync.RWMutex
}

// NewChecker creates a new health checker.
func NewChecker(store metadata.Store, storage backend.Backend) *Checker {
	return &Checker{
		store:    store,
		storage:  storage,
		cacheTTL: 5 * time.Second, // Cache health checks for 5 seconds
	}
}

// Check performs all health checks and returns the overall status.
func (c *Checker) Check(ctx context.Context) *HealthStatus {
	// Check cache first
	c.mu.RLock()

	if c.cachedStatus != nil && time.Now().Before(c.cacheExpiry) {
		status := c.cachedStatus
		c.mu.RUnlock()

		return status
	}

	c.mu.RUnlock()

	// Perform checks
	checks := make(map[string]Check)

	// Run checks in parallel
	var (
		wg       sync.WaitGroup
		checksMu sync.Mutex
	)

	wg.Add(3)

	go func() {
		defer wg.Done()

		check := c.CheckRaft(ctx)

		checksMu.Lock()

		checks["raft"] = check

		checksMu.Unlock()
	}()

	go func() {
		defer wg.Done()

		check := c.CheckStorage(ctx)

		checksMu.Lock()

		checks["storage"] = check

		checksMu.Unlock()
	}()

	go func() {
		defer wg.Done()

		check := c.CheckMetadata(ctx)

		checksMu.Lock()

		checks["metadata"] = check

		checksMu.Unlock()
	}()

	wg.Wait()

	// Determine overall status
	status := c.determineOverallStatus(checks)

	healthStatus := &HealthStatus{
		Status:    status,
		Checks:    checks,
		Timestamp: time.Now(),
	}

	// Cache the result
	c.mu.Lock()
	c.cachedStatus = healthStatus
	c.cacheExpiry = time.Now().Add(c.cacheTTL)
	c.mu.Unlock()

	return healthStatus
}

// CheckRaft checks the Raft consensus state.
func (c *Checker) CheckRaft(ctx context.Context) Check {
	if c.store == nil {
		return Check{
			Status:  StatusUnhealthy,
			Message: "metadata store not initialized",
		}
	}

	// Check if we can get cluster info
	clusterInfo, err := c.store.GetClusterInfo(ctx)
	if err != nil {
		return Check{
			Status:  StatusDegraded,
			Message: "unable to get cluster info: " + err.Error(),
		}
	}

	// Check Raft state
	if clusterInfo.RaftState == "" {
		return Check{
			Status:  StatusDegraded,
			Message: "Raft state unknown",
		}
	}

	// Check if we have a leader
	if clusterInfo.LeaderAddress == "" {
		return Check{
			Status:  StatusDegraded,
			Message: "no Raft leader elected",
		}
	}

	return Check{
		Status:  StatusHealthy,
		Message: "Raft is " + clusterInfo.RaftState + ", leader: " + clusterInfo.LeaderAddress,
	}
}

// CheckStorage checks the storage backend health.
func (c *Checker) CheckStorage(ctx context.Context) Check {
	if c.storage == nil {
		return Check{
			Status:  StatusUnhealthy,
			Message: "storage backend not initialized",
		}
	}

	// Try to get storage info
	info, err := c.storage.GetStorageInfo(ctx)
	if err != nil {
		return Check{
			Status:  StatusUnhealthy,
			Message: "storage check failed: " + err.Error(),
		}
	}

	// Check if storage is nearly full (>90%)
	if info.TotalBytes > 0 {
		usagePercent := float64(info.UsedBytes) / float64(info.TotalBytes) * 100
		if usagePercent > 95 {
			return Check{
				Status:  StatusUnhealthy,
				Message: "storage critically full (>95%)",
			}
		}

		if usagePercent > 90 {
			return Check{
				Status:  StatusDegraded,
				Message: "storage nearly full (>90%)",
			}
		}
	}

	return Check{
		Status:  StatusHealthy,
		Message: "storage is operational",
	}
}

// CheckMetadata checks the metadata store health.
func (c *Checker) CheckMetadata(ctx context.Context) Check {
	if c.store == nil {
		return Check{
			Status:  StatusUnhealthy,
			Message: "metadata store not initialized",
		}
	}

	// Try a simple read operation
	_, err := c.store.ListBuckets(ctx, "")
	if err != nil {
		return Check{
			Status:  StatusUnhealthy,
			Message: "metadata store check failed: " + err.Error(),
		}
	}

	return Check{
		Status:  StatusHealthy,
		Message: "metadata store is operational",
	}
}

// IsReady checks if the service is ready to accept requests.
func (c *Checker) IsReady(ctx context.Context) bool {
	if c.store == nil {
		return false
	}

	// Check if we're the leader or have a leader
	leaderAddr, _ := c.store.LeaderAddress()

	return c.store.IsLeader() || leaderAddr != ""
}

// IsLive checks if the service is alive.
func (c *Checker) IsLive(ctx context.Context) bool {
	// Basic liveness check - if we can execute this, we're alive
	return true
}

// determineOverallStatus determines the overall health status based on individual checks.
func (c *Checker) determineOverallStatus(checks map[string]Check) Status {
	hasUnhealthy := false
	hasDegraded := false

	for _, check := range checks {
		switch check.Status {
		case StatusUnhealthy:
			hasUnhealthy = true
		case StatusDegraded:
			hasDegraded = true
		}
	}

	if hasUnhealthy {
		return StatusUnhealthy
	}

	if hasDegraded {
		return StatusDegraded
	}

	return StatusHealthy
}

// Handler creates HTTP handlers for health endpoints.
type Handler struct {
	checker *Checker
}

// NewHandler creates a new health handler.
func NewHandler(checker *Checker) *Handler {
	return &Handler{checker: checker}
}

// HealthHandler handles basic health check requests (for load balancers).
func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	status := h.checker.Check(ctx)

	w.Header().Set("Content-Type", "application/json")

	if status.Status == StatusUnhealthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	json.NewEncoder(w).Encode(map[string]string{
		"status": string(status.Status),
	})
}

// LivenessHandler handles Kubernetes liveness probe requests.
func (h *Handler) LivenessHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.checker.IsLive(ctx) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"not ok"}`))
	}
}

// ReadinessHandler handles Kubernetes readiness probe requests.
func (h *Handler) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.checker.IsReady(ctx) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready"}`))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"not ready"}`))
	}
}

// DetailedHandler handles detailed health check requests.
func (h *Handler) DetailedHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	status := h.checker.Check(ctx)

	w.Header().Set("Content-Type", "application/json")

	switch status.Status {
	case StatusUnhealthy:
		w.WriteHeader(http.StatusServiceUnavailable)
	case StatusDegraded:
		w.WriteHeader(http.StatusOK) // Return 200 for degraded but include status in body
	default:
		w.WriteHeader(http.StatusOK)
	}

	json.NewEncoder(w).Encode(status)
}
