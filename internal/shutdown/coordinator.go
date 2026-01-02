// Package shutdown provides graceful shutdown coordination for NebulaIO services.
//
// The shutdown coordinator manages the orderly shutdown of all server components,
// ensuring data integrity and proper resource cleanup. It implements a phased
// shutdown sequence:
//
//  1. Draining - Wait for in-flight requests to complete
//  2. Workers - Stop background workers (lifecycle manager)
//  3. HTTP Servers - Shutdown HTTP servers concurrently
//  4. Cluster - Stop cluster discovery and gossip
//  5. Tiering - Stop tiering service
//  6. Audit - Flush and stop audit logger
//  7. Metadata - Flush and close metadata store (Raft + BadgerDB)
//  8. Storage - Close storage backends
//
// The coordinator tracks shutdown progress with metrics and respects configurable
// timeouts to prevent hanging during shutdown.
package shutdown

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// Phase represents a shutdown phase.
type Phase string

// Shutdown phases in order of execution.
const (
	PhaseNone           Phase = "none"
	PhaseDraining       Phase = "draining"
	PhaseWorkers        Phase = "workers"
	PhaseHTTPServers    Phase = "http_servers"
	PhaseCluster        Phase = "cluster"
	PhaseMetadata       Phase = "metadata"
	PhaseStorage        Phase = "storage"
	PhaseTiering        Phase = "tiering"
	PhaseAudit          Phase = "audit"
	PhaseComplete       Phase = "complete"
	PhaseForcedShutdown Phase = "forced_shutdown"
)

// Config holds shutdown configuration.
type Config struct {
	// TotalTimeout is the maximum time allowed for the entire shutdown sequence.
	// Default: 30 seconds
	TotalTimeout time.Duration

	// DrainTimeout is the time to wait for in-flight requests to complete.
	// Default: 15 seconds
	DrainTimeout time.Duration

	// WorkerTimeout is the time to wait for background workers to stop.
	// Default: 10 seconds
	WorkerTimeout time.Duration

	// HTTPTimeout is the time to wait for HTTP servers to shutdown.
	// Default: 10 seconds
	HTTPTimeout time.Duration

	// MetadataTimeout is the time to wait for metadata store to close.
	// Default: 10 seconds
	MetadataTimeout time.Duration

	// StorageTimeout is the time to wait for storage backends to close.
	// Default: 5 seconds
	StorageTimeout time.Duration

	// ForceTimeout is the time after which shutdown is forced.
	// Default: 5 seconds after TotalTimeout
	ForceTimeout time.Duration
}

// DefaultConfig returns the default shutdown configuration.
func DefaultConfig() Config {
	return Config{
		TotalTimeout:    30 * time.Second,
		DrainTimeout:    15 * time.Second,
		WorkerTimeout:   10 * time.Second,
		HTTPTimeout:     10 * time.Second,
		MetadataTimeout: 10 * time.Second,
		StorageTimeout:  5 * time.Second,
		ForceTimeout:    5 * time.Second,
	}
}

// Component represents a component that can be shutdown.
type Component interface {
	// Name returns the component name for logging.
	Name() string
}

// Closeable represents a component with a Close method.
type Closeable interface {
	Component
	Close() error
}

// CloseableWithContext represents a component with a context-aware Close method.
type CloseableWithContext interface {
	Component
	Close(ctx context.Context) error
}

// Stoppable represents a component with a Stop method.
type Stoppable interface {
	Component
	Stop() error
}

// StoppableWithContext represents a component with a context-aware Stop method.
type StoppableWithContext interface {
	Component
	Stop(ctx context.Context) error
}

// ShutdownHook is a function called during shutdown.
type ShutdownHook func(ctx context.Context) error

// Coordinator manages graceful shutdown of all server components.
type Coordinator struct {
	config   Config
	mu       sync.RWMutex
	phase    Phase
	started  time.Time
	errors   []error
	hooks    map[Phase][]ShutdownHook
	doneCh   chan struct{}
	shutdown atomic.Bool
}

// NewCoordinator creates a new shutdown coordinator with the given configuration.
func NewCoordinator(cfg Config) *Coordinator {
	return &Coordinator{
		config: cfg,
		phase:  PhaseNone,
		hooks:  make(map[Phase][]ShutdownHook),
		doneCh: make(chan struct{}),
	}
}

// RegisterHook registers a shutdown hook for a specific phase.
func (c *Coordinator) RegisterHook(phase Phase, hook ShutdownHook) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hooks[phase] = append(c.hooks[phase], hook)
}

// Phase returns the current shutdown phase.
func (c *Coordinator) Phase() Phase {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.phase
}

// IsShuttingDown returns true if shutdown has been initiated.
func (c *Coordinator) IsShuttingDown() bool {
	return c.shutdown.Load()
}

// Done returns a channel that is closed when shutdown is complete.
func (c *Coordinator) Done() <-chan struct{} {
	return c.doneCh
}

// Errors returns any errors that occurred during shutdown.
func (c *Coordinator) Errors() []error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return append([]error{}, c.errors...)
}

// setPhase updates the current phase and logs the transition.
func (c *Coordinator) setPhase(phase Phase) {
	c.mu.Lock()
	oldPhase := c.phase
	c.phase = phase
	c.mu.Unlock()

	elapsed := time.Since(c.started)
	log.Info().
		Str("from_phase", string(oldPhase)).
		Str("to_phase", string(phase)).
		Dur("elapsed", elapsed).
		Msg("Shutdown phase transition")

	// Update metrics
	SetShutdownPhase(phase)
}

// addError records a shutdown error.
func (c *Coordinator) addError(err error) {
	c.mu.Lock()
	c.errors = append(c.errors, err)
	c.mu.Unlock()

	IncrementShutdownErrors()
}

// runHooks executes all hooks registered for the given phase.
func (c *Coordinator) runHooks(ctx context.Context, phase Phase) {
	c.mu.RLock()
	hooks := c.hooks[phase]
	c.mu.RUnlock()

	for _, hook := range hooks {
		if err := hook(ctx); err != nil {
			log.Error().Err(err).Str("phase", string(phase)).Msg("Shutdown hook failed")
			c.addError(err)
		}
	}
}

// Shutdown initiates graceful shutdown of all components.
func (c *Coordinator) Shutdown(ctx context.Context, components ShutdownComponents) error {
	// Ensure we only shutdown once
	if !c.shutdown.CompareAndSwap(false, true) {
		log.Warn().Msg("Shutdown already in progress")

		return nil
	}

	c.started = time.Now()
	log.Info().Msg("Initiating graceful shutdown")
	SetShutdownStartTime(c.started)

	// Create overall timeout context
	shutdownCtx, cancel := context.WithTimeout(ctx, c.config.TotalTimeout)
	defer cancel()

	// Start forced shutdown timer
	go c.watchForceTimeout(shutdownCtx)

	// Execute shutdown sequence
	c.executeShutdownSequence(shutdownCtx, components)

	// Mark completion
	c.setPhase(PhaseComplete)
	close(c.doneCh)

	duration := time.Since(c.started)
	SetShutdownDuration(duration)

	if len(c.errors) > 0 {
		log.Warn().
			Int("error_count", len(c.errors)).
			Dur("duration", duration).
			Msg("Shutdown completed with errors")
	} else {
		log.Info().
			Dur("duration", duration).
			Msg("Shutdown completed successfully")
	}

	return nil
}

// watchForceTimeout monitors for force timeout and triggers forced shutdown.
func (c *Coordinator) watchForceTimeout(ctx context.Context) {
	forceDeadline := c.config.TotalTimeout + c.config.ForceTimeout
	timer := time.NewTimer(forceDeadline)

	defer timer.Stop()

	select {
	case <-timer.C:
		c.setPhase(PhaseForcedShutdown)
		log.Warn().
			Dur("timeout", forceDeadline).
			Msg("Force timeout reached, forcing shutdown")
	case <-c.doneCh:
		// Shutdown completed normally, goroutine exits cleanly
	case <-ctx.Done():
		// Context cancelled, goroutine exits cleanly
	}
}

// ShutdownComponents holds all components that need to be shutdown.
type ShutdownComponents struct {
	// HTTPServers are HTTP servers to shutdown gracefully
	HTTPServers []HTTPServerShutdown

	// Discovery is the cluster discovery service
	Discovery Stoppable

	// LifecycleManager is the lifecycle manager
	LifecycleManager StoppableNoError

	// AuditLogger is the audit logger
	AuditLogger StoppableNoError

	// MetadataStore is the metadata store (Raft + BadgerDB)
	MetadataStore Closeable

	// StorageBackend is the storage backend
	StorageBackend io.Closer

	// TieringService is the tiering service
	TieringService Stoppable

	// InFlightTracker tracks in-flight requests for draining
	InFlightTracker InFlightTracker
}

// HTTPServerShutdown wraps an HTTP server for shutdown.
type HTTPServerShutdown interface {
	Name() string
	Shutdown(ctx context.Context) error
}

// StoppableNoError represents a component with a Stop method that doesn't return an error.
type StoppableNoError interface {
	Stop()
}

// InFlightTracker tracks in-flight requests.
type InFlightTracker interface {
	// InFlightCount returns the number of in-flight requests
	InFlightCount() int64
	// WaitForDrain waits for all in-flight requests to complete
	WaitForDrain(ctx context.Context) error
}

// executeShutdownSequence runs through all shutdown phases in order.
func (c *Coordinator) executeShutdownSequence(ctx context.Context, components ShutdownComponents) {
	// Phase 1: Draining
	c.executeDrainPhase(ctx, components)

	// Phase 2: Stop background workers
	c.executeWorkersPhase(ctx, components)

	// Phase 3: Stop HTTP servers
	c.executeHTTPServersPhase(ctx, components)

	// Phase 4: Stop cluster services
	c.executeClusterPhase(ctx, components)

	// Phase 5: Stop tiering service
	c.executeTieringPhase(ctx, components)

	// Phase 6: Stop audit logger
	c.executeAuditPhase(ctx, components)

	// Phase 7: Close metadata store
	c.executeMetadataPhase(ctx, components)

	// Phase 8: Close storage backend
	c.executeStoragePhase(ctx, components)
}

func (c *Coordinator) executeDrainPhase(ctx context.Context, components ShutdownComponents) {
	c.setPhase(PhaseDraining)
	c.runHooks(ctx, PhaseDraining)

	if components.InFlightTracker == nil {
		return
	}

	drainCtx, cancel := context.WithTimeout(ctx, c.config.DrainTimeout)
	defer cancel()

	inFlight := components.InFlightTracker.InFlightCount()
	SetInFlightRequests(inFlight)

	if inFlight > 0 {
		log.Info().Int64("in_flight_requests", inFlight).Msg("Waiting for in-flight requests to complete")

		if err := components.InFlightTracker.WaitForDrain(drainCtx); err != nil {
			log.Warn().
				Err(err).
				Int64("remaining", components.InFlightTracker.InFlightCount()).
				Msg("Drain timeout, proceeding with shutdown")
			c.addError(err)
		}
	}

	SetInFlightRequests(0)
}

func (c *Coordinator) executeWorkersPhase(ctx context.Context, components ShutdownComponents) {
	c.setPhase(PhaseWorkers)
	c.runHooks(ctx, PhaseWorkers)

	workerCtx, cancel := context.WithTimeout(ctx, c.config.WorkerTimeout)
	defer cancel()

	// Stop lifecycle manager
	if components.LifecycleManager != nil {
		c.stopComponentNoError(workerCtx, "lifecycle_manager", components.LifecycleManager)
		IncrementWorkersStopped()
	}
}

func (c *Coordinator) executeHTTPServersPhase(ctx context.Context, components ShutdownComponents) {
	c.setPhase(PhaseHTTPServers)
	c.runHooks(ctx, PhaseHTTPServers)

	httpCtx, cancel := context.WithTimeout(ctx, c.config.HTTPTimeout)
	defer cancel()

	// Shutdown HTTP servers concurrently
	var wg sync.WaitGroup

	for _, server := range components.HTTPServers {
		wg.Add(1)

		go func(srv HTTPServerShutdown) {
			defer wg.Done()

			if err := srv.Shutdown(httpCtx); err != nil {
				log.Error().Err(err).Str("server", srv.Name()).Msg("Error shutting down HTTP server")
				c.addError(err)
			} else {
				log.Info().Str("server", srv.Name()).Msg("HTTP server shutdown complete")
			}
		}(server)
	}

	wg.Wait()
}

func (c *Coordinator) executeClusterPhase(ctx context.Context, components ShutdownComponents) {
	c.setPhase(PhaseCluster)
	c.runHooks(ctx, PhaseCluster)

	if components.Discovery == nil {
		return
	}

	clusterCtx, cancel := context.WithTimeout(ctx, c.config.WorkerTimeout)
	defer cancel()

	c.stopComponent(clusterCtx, "cluster_discovery", components.Discovery)
	IncrementWorkersStopped()
}

func (c *Coordinator) executeTieringPhase(ctx context.Context, components ShutdownComponents) {
	c.setPhase(PhaseTiering)
	c.runHooks(ctx, PhaseTiering)

	if components.TieringService == nil {
		return
	}

	tieringCtx, cancel := context.WithTimeout(ctx, c.config.StorageTimeout)
	defer cancel()

	c.stopComponent(tieringCtx, "tiering_service", components.TieringService)
	IncrementWorkersStopped()
}

func (c *Coordinator) executeAuditPhase(ctx context.Context, components ShutdownComponents) {
	c.setPhase(PhaseAudit)
	c.runHooks(ctx, PhaseAudit)

	if components.AuditLogger == nil {
		return
	}

	c.stopComponentNoError(ctx, "audit_logger", components.AuditLogger)
	IncrementWorkersStopped()
	log.Info().Msg("Audit logger stopped")
}

func (c *Coordinator) executeMetadataPhase(ctx context.Context, components ShutdownComponents) {
	c.setPhase(PhaseMetadata)
	c.runHooks(ctx, PhaseMetadata)

	if components.MetadataStore == nil {
		return
	}

	metaCtx, cancel := context.WithTimeout(ctx, c.config.MetadataTimeout)
	defer cancel()

	c.closeComponent(metaCtx, "metadata_store", components.MetadataStore)
}

func (c *Coordinator) executeStoragePhase(ctx context.Context, components ShutdownComponents) {
	c.setPhase(PhaseStorage)
	c.runHooks(ctx, PhaseStorage)

	if components.StorageBackend == nil {
		return
	}

	storageCtx, cancel := context.WithTimeout(ctx, c.config.StorageTimeout)
	defer cancel()

	// Use a goroutine with timeout to close storage
	done := make(chan error, 1)

	go func() {
		done <- components.StorageBackend.Close()
	}()

	select {
	case err := <-done:
		if err != nil {
			log.Error().Err(err).Msg("Error closing storage backend")
			c.addError(err)
		} else {
			log.Info().Msg("Storage backend closed")
		}
	case <-storageCtx.Done():
		log.Warn().Msg("Timeout closing storage backend")
		c.addError(storageCtx.Err())
	}
}

func (c *Coordinator) stopComponent(ctx context.Context, name string, component Stoppable) {
	done := make(chan error, 1)

	go func() {
		done <- component.Stop()
	}()

	select {
	case err := <-done:
		if err != nil {
			log.Error().Err(err).Str("component", name).Msg("Error stopping component")
			c.addError(err)
		} else {
			log.Debug().Str("component", name).Msg("Component stopped")
		}
	case <-ctx.Done():
		log.Warn().Str("component", name).Msg("Timeout stopping component")
		c.addError(ctx.Err())
	}
}

func (c *Coordinator) stopComponentNoError(ctx context.Context, name string, component StoppableNoError) {
	done := make(chan struct{}, 1)

	go func() {
		component.Stop()
		done <- struct{}{}
	}()

	select {
	case <-done:
		log.Debug().Str("component", name).Msg("Component stopped")
	case <-ctx.Done():
		log.Warn().Str("component", name).Msg("Timeout stopping component (no error)")
		c.addError(ctx.Err())
	}
}

func (c *Coordinator) closeComponent(ctx context.Context, name string, component Closeable) {
	done := make(chan error, 1)

	go func() {
		done <- component.Close()
	}()

	select {
	case err := <-done:
		if err != nil {
			log.Error().Err(err).Str("component", name).Msg("Error closing component")
			c.addError(err)
		} else {
			log.Info().Str("component", name).Msg("Component closed")
		}
	case <-ctx.Done():
		log.Warn().Str("component", name).Msg("Timeout closing component")
		c.addError(ctx.Err())
	}
}
