package shutdown_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/piwi3910/nebulaio/internal/shutdown"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := shutdown.DefaultConfig()

	assert.Equal(t, 30*time.Second, cfg.TotalTimeout)
	assert.Equal(t, 15*time.Second, cfg.DrainTimeout)
	assert.Equal(t, 10*time.Second, cfg.WorkerTimeout)
	assert.Equal(t, 10*time.Second, cfg.HTTPTimeout)
	assert.Equal(t, 10*time.Second, cfg.MetadataTimeout)
	assert.Equal(t, 5*time.Second, cfg.StorageTimeout)
	assert.Equal(t, 5*time.Second, cfg.ForceTimeout)
}

func TestNewCoordinator(t *testing.T) {
	cfg := shutdown.DefaultConfig()
	coord := shutdown.NewCoordinator(cfg)

	require.NotNil(t, coord)
	assert.Equal(t, shutdown.PhaseNone, coord.Phase())
	assert.False(t, coord.IsShuttingDown())
	assert.Empty(t, coord.Errors())
}

func TestCoordinatorPhaseTransitions(t *testing.T) {
	cfg := shutdown.Config{
		TotalTimeout:    100 * time.Millisecond,
		DrainTimeout:    10 * time.Millisecond,
		WorkerTimeout:   10 * time.Millisecond,
		HTTPTimeout:     10 * time.Millisecond,
		MetadataTimeout: 10 * time.Millisecond,
		StorageTimeout:  10 * time.Millisecond,
		ForceTimeout:    50 * time.Millisecond,
	}
	coord := shutdown.NewCoordinator(cfg)

	components := shutdown.ShutdownComponents{}

	ctx := context.Background()
	err := coord.Shutdown(ctx, components)

	require.NoError(t, err)
	assert.Equal(t, shutdown.PhaseComplete, coord.Phase())
	assert.True(t, coord.IsShuttingDown())
}

func TestCoordinatorShutdownOnlyOnce(t *testing.T) {
	cfg := shutdown.Config{
		TotalTimeout:    100 * time.Millisecond,
		DrainTimeout:    10 * time.Millisecond,
		WorkerTimeout:   10 * time.Millisecond,
		HTTPTimeout:     10 * time.Millisecond,
		MetadataTimeout: 10 * time.Millisecond,
		StorageTimeout:  10 * time.Millisecond,
		ForceTimeout:    50 * time.Millisecond,
	}
	coord := shutdown.NewCoordinator(cfg)

	components := shutdown.ShutdownComponents{}
	ctx := context.Background()

	// First shutdown
	err := coord.Shutdown(ctx, components)
	require.NoError(t, err)

	// Second shutdown should return immediately
	err = coord.Shutdown(ctx, components)
	require.NoError(t, err)
}

func TestCoordinatorDoneChannel(t *testing.T) {
	cfg := shutdown.Config{
		TotalTimeout:    100 * time.Millisecond,
		DrainTimeout:    10 * time.Millisecond,
		WorkerTimeout:   10 * time.Millisecond,
		HTTPTimeout:     10 * time.Millisecond,
		MetadataTimeout: 10 * time.Millisecond,
		StorageTimeout:  10 * time.Millisecond,
		ForceTimeout:    50 * time.Millisecond,
	}
	coord := shutdown.NewCoordinator(cfg)

	components := shutdown.ShutdownComponents{}
	ctx := context.Background()

	// Start shutdown in goroutine
	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = coord.Shutdown(ctx, components)
	}()

	// Wait for done channel
	select {
	case <-coord.Done():
		// Expected
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Done channel was not closed")
	}
}

func TestCoordinatorWithHTTPServers(t *testing.T) {
	cfg := shutdown.Config{
		TotalTimeout:    100 * time.Millisecond,
		DrainTimeout:    10 * time.Millisecond,
		WorkerTimeout:   10 * time.Millisecond,
		HTTPTimeout:     50 * time.Millisecond,
		MetadataTimeout: 10 * time.Millisecond,
		StorageTimeout:  10 * time.Millisecond,
		ForceTimeout:    50 * time.Millisecond,
	}
	coord := shutdown.NewCoordinator(cfg)

	server1 := &mockHTTPServer{name: "server1"}
	server2 := &mockHTTPServer{name: "server2"}

	components := shutdown.ShutdownComponents{
		HTTPServers: []shutdown.HTTPServerShutdown{server1, server2},
	}

	ctx := context.Background()
	err := coord.Shutdown(ctx, components)

	require.NoError(t, err)
	assert.True(t, server1.shutdownCalled)
	assert.True(t, server2.shutdownCalled)
}

func TestCoordinatorWithHTTPServerError(t *testing.T) {
	cfg := shutdown.Config{
		TotalTimeout:    100 * time.Millisecond,
		DrainTimeout:    10 * time.Millisecond,
		WorkerTimeout:   10 * time.Millisecond,
		HTTPTimeout:     50 * time.Millisecond,
		MetadataTimeout: 10 * time.Millisecond,
		StorageTimeout:  10 * time.Millisecond,
		ForceTimeout:    50 * time.Millisecond,
	}
	coord := shutdown.NewCoordinator(cfg)

	expectedErr := errors.New("shutdown error")
	server := &mockHTTPServer{name: "failing-server", err: expectedErr}

	components := shutdown.ShutdownComponents{
		HTTPServers: []shutdown.HTTPServerShutdown{server},
	}

	ctx := context.Background()
	err := coord.Shutdown(ctx, components)

	require.NoError(t, err) // Shutdown itself doesn't return error
	assert.True(t, server.shutdownCalled)
	assert.Len(t, coord.Errors(), 1)
	assert.Equal(t, expectedErr, coord.Errors()[0])
}

func TestCoordinatorWithMetadataStore(t *testing.T) {
	cfg := shutdown.Config{
		TotalTimeout:    100 * time.Millisecond,
		DrainTimeout:    10 * time.Millisecond,
		WorkerTimeout:   10 * time.Millisecond,
		HTTPTimeout:     10 * time.Millisecond,
		MetadataTimeout: 50 * time.Millisecond,
		StorageTimeout:  10 * time.Millisecond,
		ForceTimeout:    50 * time.Millisecond,
	}
	coord := shutdown.NewCoordinator(cfg)

	store := &mockCloseable{name: "metadata"}

	components := shutdown.ShutdownComponents{
		MetadataStore: store,
	}

	ctx := context.Background()
	err := coord.Shutdown(ctx, components)

	require.NoError(t, err)
	assert.True(t, store.closeCalled)
}

func TestCoordinatorWithStorageBackend(t *testing.T) {
	cfg := shutdown.Config{
		TotalTimeout:    100 * time.Millisecond,
		DrainTimeout:    10 * time.Millisecond,
		WorkerTimeout:   10 * time.Millisecond,
		HTTPTimeout:     10 * time.Millisecond,
		MetadataTimeout: 10 * time.Millisecond,
		StorageTimeout:  50 * time.Millisecond,
		ForceTimeout:    50 * time.Millisecond,
	}
	coord := shutdown.NewCoordinator(cfg)

	storage := &mockCloser{}

	components := shutdown.ShutdownComponents{
		StorageBackend: storage,
	}

	ctx := context.Background()
	err := coord.Shutdown(ctx, components)

	require.NoError(t, err)
	assert.True(t, storage.closeCalled)
}

func TestCoordinatorWithDiscovery(t *testing.T) {
	cfg := shutdown.Config{
		TotalTimeout:    100 * time.Millisecond,
		DrainTimeout:    10 * time.Millisecond,
		WorkerTimeout:   50 * time.Millisecond,
		HTTPTimeout:     10 * time.Millisecond,
		MetadataTimeout: 10 * time.Millisecond,
		StorageTimeout:  10 * time.Millisecond,
		ForceTimeout:    50 * time.Millisecond,
	}
	coord := shutdown.NewCoordinator(cfg)

	discovery := &mockStoppable{name: "discovery"}

	components := shutdown.ShutdownComponents{
		Discovery: discovery,
	}

	ctx := context.Background()
	err := coord.Shutdown(ctx, components)

	require.NoError(t, err)
	assert.True(t, discovery.stopCalled)
}

func TestCoordinatorWithLifecycleManager(t *testing.T) {
	cfg := shutdown.Config{
		TotalTimeout:    100 * time.Millisecond,
		DrainTimeout:    10 * time.Millisecond,
		WorkerTimeout:   50 * time.Millisecond,
		HTTPTimeout:     10 * time.Millisecond,
		MetadataTimeout: 10 * time.Millisecond,
		StorageTimeout:  10 * time.Millisecond,
		ForceTimeout:    50 * time.Millisecond,
	}
	coord := shutdown.NewCoordinator(cfg)

	lifecycle := &mockStoppableNoError{}

	components := shutdown.ShutdownComponents{
		LifecycleManager: lifecycle,
	}

	ctx := context.Background()
	err := coord.Shutdown(ctx, components)

	require.NoError(t, err)
	assert.True(t, lifecycle.stopCalled)
}

func TestCoordinatorWithAuditLogger(t *testing.T) {
	cfg := shutdown.Config{
		TotalTimeout:    100 * time.Millisecond,
		DrainTimeout:    10 * time.Millisecond,
		WorkerTimeout:   10 * time.Millisecond,
		HTTPTimeout:     10 * time.Millisecond,
		MetadataTimeout: 10 * time.Millisecond,
		StorageTimeout:  10 * time.Millisecond,
		ForceTimeout:    50 * time.Millisecond,
	}
	coord := shutdown.NewCoordinator(cfg)

	auditLogger := &mockStoppableNoError{}

	components := shutdown.ShutdownComponents{
		AuditLogger: auditLogger,
	}

	ctx := context.Background()
	err := coord.Shutdown(ctx, components)

	require.NoError(t, err)
	assert.True(t, auditLogger.stopCalled)
}

func TestCoordinatorWithTieringService(t *testing.T) {
	cfg := shutdown.Config{
		TotalTimeout:    100 * time.Millisecond,
		DrainTimeout:    10 * time.Millisecond,
		WorkerTimeout:   10 * time.Millisecond,
		HTTPTimeout:     10 * time.Millisecond,
		MetadataTimeout: 10 * time.Millisecond,
		StorageTimeout:  50 * time.Millisecond,
		ForceTimeout:    50 * time.Millisecond,
	}
	coord := shutdown.NewCoordinator(cfg)

	tiering := &mockStoppable{name: "tiering"}

	components := shutdown.ShutdownComponents{
		TieringService: tiering,
	}

	ctx := context.Background()
	err := coord.Shutdown(ctx, components)

	require.NoError(t, err)
	assert.True(t, tiering.stopCalled)
}

func TestCoordinatorWithInFlightTracker(t *testing.T) {
	cfg := shutdown.Config{
		TotalTimeout:    200 * time.Millisecond,
		DrainTimeout:    50 * time.Millisecond,
		WorkerTimeout:   10 * time.Millisecond,
		HTTPTimeout:     10 * time.Millisecond,
		MetadataTimeout: 10 * time.Millisecond,
		StorageTimeout:  10 * time.Millisecond,
		ForceTimeout:    50 * time.Millisecond,
	}
	coord := shutdown.NewCoordinator(cfg)

	tracker := &mockInFlightTracker{count: 5}

	components := shutdown.ShutdownComponents{
		InFlightTracker: tracker,
	}

	ctx := context.Background()
	err := coord.Shutdown(ctx, components)

	require.NoError(t, err)
	assert.True(t, tracker.waitCalled)
}

func TestCoordinatorRegisterHook(t *testing.T) {
	cfg := shutdown.Config{
		TotalTimeout:    100 * time.Millisecond,
		DrainTimeout:    10 * time.Millisecond,
		WorkerTimeout:   10 * time.Millisecond,
		HTTPTimeout:     10 * time.Millisecond,
		MetadataTimeout: 10 * time.Millisecond,
		StorageTimeout:  10 * time.Millisecond,
		ForceTimeout:    50 * time.Millisecond,
	}
	coord := shutdown.NewCoordinator(cfg)

	hookCalled := false
	coord.RegisterHook(shutdown.PhaseDraining, func(ctx context.Context) error {
		hookCalled = true

		return nil
	})

	components := shutdown.ShutdownComponents{}
	ctx := context.Background()
	err := coord.Shutdown(ctx, components)

	require.NoError(t, err)
	assert.True(t, hookCalled)
}

func TestCoordinatorHookError(t *testing.T) {
	cfg := shutdown.Config{
		TotalTimeout:    100 * time.Millisecond,
		DrainTimeout:    10 * time.Millisecond,
		WorkerTimeout:   10 * time.Millisecond,
		HTTPTimeout:     10 * time.Millisecond,
		MetadataTimeout: 10 * time.Millisecond,
		StorageTimeout:  10 * time.Millisecond,
		ForceTimeout:    50 * time.Millisecond,
	}
	coord := shutdown.NewCoordinator(cfg)

	expectedErr := errors.New("hook error")
	coord.RegisterHook(shutdown.PhaseDraining, func(ctx context.Context) error {
		return expectedErr
	})

	components := shutdown.ShutdownComponents{}
	ctx := context.Background()
	err := coord.Shutdown(ctx, components)

	require.NoError(t, err)
	assert.Len(t, coord.Errors(), 1)
	assert.Equal(t, expectedErr, coord.Errors()[0])
}

func TestCoordinatorConcurrentHTTPServerShutdown(t *testing.T) {
	cfg := shutdown.Config{
		TotalTimeout:    500 * time.Millisecond,
		DrainTimeout:    10 * time.Millisecond,
		WorkerTimeout:   10 * time.Millisecond,
		HTTPTimeout:     200 * time.Millisecond,
		MetadataTimeout: 10 * time.Millisecond,
		StorageTimeout:  10 * time.Millisecond,
		ForceTimeout:    50 * time.Millisecond,
	}
	coord := shutdown.NewCoordinator(cfg)

	// Create servers with different shutdown times
	server1 := &mockHTTPServer{name: "server1", delay: 50 * time.Millisecond}
	server2 := &mockHTTPServer{name: "server2", delay: 50 * time.Millisecond}
	server3 := &mockHTTPServer{name: "server3", delay: 50 * time.Millisecond}

	components := shutdown.ShutdownComponents{
		HTTPServers: []shutdown.HTTPServerShutdown{server1, server2, server3},
	}

	start := time.Now()
	ctx := context.Background()
	err := coord.Shutdown(ctx, components)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.True(t, server1.shutdownCalled)
	assert.True(t, server2.shutdownCalled)
	assert.True(t, server3.shutdownCalled)

	// Since servers shutdown concurrently, total time should be less than 3x individual delay
	assert.Less(t, elapsed, 150*time.Millisecond)
}

func TestCoordinatorTimeoutOnSlowComponent(t *testing.T) {
	cfg := shutdown.Config{
		TotalTimeout:    100 * time.Millisecond,
		DrainTimeout:    10 * time.Millisecond,
		WorkerTimeout:   10 * time.Millisecond,
		HTTPTimeout:     10 * time.Millisecond,
		MetadataTimeout: 10 * time.Millisecond,
		StorageTimeout:  20 * time.Millisecond,
		ForceTimeout:    50 * time.Millisecond,
	}
	coord := shutdown.NewCoordinator(cfg)

	// Create a slow storage backend that takes longer than the timeout
	storage := &mockCloser{delay: 100 * time.Millisecond}

	components := shutdown.ShutdownComponents{
		StorageBackend: storage,
	}

	ctx := context.Background()
	err := coord.Shutdown(ctx, components)

	require.NoError(t, err)
	// Should have a timeout error
	assert.Len(t, coord.Errors(), 1)
	assert.ErrorIs(t, coord.Errors()[0], context.DeadlineExceeded)
}

func TestCoordinatorAllComponents(t *testing.T) {
	cfg := shutdown.Config{
		TotalTimeout:    500 * time.Millisecond,
		DrainTimeout:    50 * time.Millisecond,
		WorkerTimeout:   50 * time.Millisecond,
		HTTPTimeout:     50 * time.Millisecond,
		MetadataTimeout: 50 * time.Millisecond,
		StorageTimeout:  50 * time.Millisecond,
		ForceTimeout:    50 * time.Millisecond,
	}
	coord := shutdown.NewCoordinator(cfg)

	httpServer := &mockHTTPServer{name: "http"}
	discovery := &mockStoppable{name: "discovery"}
	lifecycle := &mockStoppableNoError{}
	auditLogger := &mockStoppableNoError{}
	metadataStore := &mockCloseable{name: "metadata"}
	storageBackend := &mockCloser{}
	tieringService := &mockStoppable{name: "tiering"}
	inFlightTracker := &mockInFlightTracker{count: 0}

	components := shutdown.ShutdownComponents{
		HTTPServers:      []shutdown.HTTPServerShutdown{httpServer},
		Discovery:        discovery,
		LifecycleManager: lifecycle,
		AuditLogger:      auditLogger,
		MetadataStore:    metadataStore,
		StorageBackend:   storageBackend,
		TieringService:   tieringService,
		InFlightTracker:  inFlightTracker,
	}

	ctx := context.Background()
	err := coord.Shutdown(ctx, components)

	require.NoError(t, err)
	assert.True(t, httpServer.shutdownCalled)
	assert.True(t, discovery.stopCalled)
	assert.True(t, lifecycle.stopCalled)
	assert.True(t, auditLogger.stopCalled)
	assert.True(t, metadataStore.closeCalled)
	assert.True(t, storageBackend.closeCalled)
	assert.True(t, tieringService.stopCalled)
	assert.Empty(t, coord.Errors())
}

// Mock implementations.

type mockHTTPServer struct {
	name           string
	shutdownCalled bool
	err            error
	delay          time.Duration
	mu             sync.Mutex
}

func (m *mockHTTPServer) Name() string {
	return m.name
}

func (m *mockHTTPServer) Shutdown(_ context.Context) error {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}

	m.mu.Lock()
	m.shutdownCalled = true
	m.mu.Unlock()

	return m.err
}

type mockCloseable struct {
	name        string
	closeCalled bool
	err         error
}

func (m *mockCloseable) Name() string {
	return m.name
}

func (m *mockCloseable) Close() error {
	m.closeCalled = true

	return m.err
}

type mockCloser struct {
	closeCalled bool
	err         error
	delay       time.Duration
}

func (m *mockCloser) Close() error {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}

	m.closeCalled = true

	return m.err
}

type mockStoppable struct {
	name       string
	stopCalled bool
	err        error
}

func (m *mockStoppable) Name() string {
	return m.name
}

func (m *mockStoppable) Stop() error {
	m.stopCalled = true

	return m.err
}

type mockStoppableNoError struct {
	stopCalled bool
}

func (m *mockStoppableNoError) Stop() {
	m.stopCalled = true
}

type mockInFlightTracker struct {
	count      int64
	waitCalled bool
}

func (m *mockInFlightTracker) InFlightCount() int64 {
	return atomic.LoadInt64(&m.count)
}

func (m *mockInFlightTracker) WaitForDrain(_ context.Context) error {
	m.waitCalled = true
	atomic.StoreInt64(&m.count, 0)

	return nil
}
