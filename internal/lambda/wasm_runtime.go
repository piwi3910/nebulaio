package lambda

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"golang.org/x/sync/singleflight"
)

// WASM runtime constants.
const (
	// WASMPageSize is the size of a single WASM memory page (64KB).
	WASMPageSize = 64 * 1024

	// WASMPagesPerMB is the number of WASM pages per megabyte.
	WASMPagesPerMB = 16

	// WASMMaxMemoryPages is the maximum number of WASM memory pages (4GB limit / 64KB).
	WASMMaxMemoryPages = 65535

	// DefaultWASMMaxMemoryMB is the default maximum memory for WASM modules.
	DefaultWASMMaxMemoryMB = 256

	// DefaultWASMMaxExecutionTime is the default maximum execution time.
	DefaultWASMMaxExecutionTime = 30 * time.Second

	// DefaultWASMMaxInputSize is the default maximum input size (100 MB).
	DefaultWASMMaxInputSize = 100 * 1024 * 1024

	// DefaultWASMMaxOutputSize is the default maximum output size (100 MB).
	DefaultWASMMaxOutputSize = 100 * 1024 * 1024

	// DefaultWASMStreamingThreshold is the default streaming threshold (10 MB).
	DefaultWASMStreamingThreshold = 10 * 1024 * 1024

	// DefaultWASMMaxModuleSize is the default maximum WASM module size (50 MB).
	DefaultWASMMaxModuleSize = 50 * 1024 * 1024

	// DefaultWASMMaxCacheEntries is the default maximum number of cached modules.
	DefaultWASMMaxCacheEntries = 100
)

// Prometheus metrics for WASM runtime.
var (
	wasmTransformationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nebulaio_wasm_transformations_total",
			Help: "Total number of WASM transformations",
		},
		[]string{"status", "module"},
	)

	wasmTransformationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nebulaio_wasm_transformation_duration_seconds",
			Help:    "Duration of WASM transformations in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"module"},
	)

	wasmActiveExecutions = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_wasm_active_executions",
			Help: "Number of currently active WASM executions",
		},
	)

	wasmModuleCacheSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "nebulaio_wasm_module_cache_size",
			Help: "Number of compiled WASM modules in cache",
		},
	)

	wasmModuleCacheHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_wasm_module_cache_hits_total",
			Help: "Total number of WASM module cache hits",
		},
	)

	wasmModuleCacheMisses = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_wasm_module_cache_misses_total",
			Help: "Total number of WASM module cache misses",
		},
	)

	wasmInputBytes = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_wasm_input_bytes_total",
			Help: "Total bytes of input data processed by WASM transformations",
		},
	)

	wasmOutputBytes = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_wasm_output_bytes_total",
			Help: "Total bytes of output data produced by WASM transformations",
		},
	)

	wasmModuleCacheEvictions = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "nebulaio_wasm_module_cache_evictions_total",
			Help: "Total number of WASM modules evicted from cache",
		},
	)
)

func init() {
	prometheus.MustRegister(
		wasmTransformationsTotal,
		wasmTransformationDuration,
		wasmActiveExecutions,
		wasmModuleCacheSize,
		wasmModuleCacheHits,
		wasmModuleCacheMisses,
		wasmInputBytes,
		wasmOutputBytes,
		wasmModuleCacheEvictions,
	)
}

// WASMRuntime manages WebAssembly module execution for Object Lambda transformations.
// It provides sandboxed execution with configurable resource limits.
type WASMRuntime struct {
	runtime       wazero.Runtime
	moduleCache   map[string]wazero.CompiledModule
	cacheOrder    []string // LRU order tracking: oldest first
	moduleLoader  ModuleLoader
	mu            sync.RWMutex
	closed        atomic.Bool
	config        *WASMRuntimeConfig
	metricsActive atomic.Int64
	compileGroup  singleflight.Group // Prevents concurrent compilation of same module
}

// WASMRuntimeConfig holds configuration for the WASM runtime.
type WASMRuntimeConfig struct {
	// MaxMemoryMB is the maximum memory (in MB) a WASM module can use.
	// Default: 256 MB
	MaxMemoryMB int

	// MaxExecutionTime is the maximum time a WASM function can execute.
	// Default: 30 seconds
	MaxExecutionTime time.Duration

	// MaxInputSize is the maximum size of input data (in bytes).
	// Default: 100 MB
	MaxInputSize int64

	// MaxOutputSize is the maximum size of output data (in bytes).
	// Default: 100 MB
	MaxOutputSize int64

	// EnableCaching enables compiled module caching.
	// Default: true
	EnableCaching bool

	// StreamingThreshold is the size threshold (in bytes) above which
	// streaming mode is used for transformations.
	// Default: 10 MB
	StreamingThreshold int64

	// MaxModuleSize is the maximum size of a WASM module file (in bytes).
	// Default: 50 MB
	MaxModuleSize int64

	// MaxCacheEntries is the maximum number of compiled modules to cache.
	// When exceeded, the least recently used module is evicted.
	// Default: 100
	MaxCacheEntries int
}

// DefaultWASMRuntimeConfig returns a WASMRuntimeConfig with sensible defaults.
func DefaultWASMRuntimeConfig() *WASMRuntimeConfig {
	return &WASMRuntimeConfig{
		MaxMemoryMB:        DefaultWASMMaxMemoryMB,
		MaxExecutionTime:   DefaultWASMMaxExecutionTime,
		MaxInputSize:       DefaultWASMMaxInputSize,
		MaxOutputSize:      DefaultWASMMaxOutputSize,
		EnableCaching:      true,
		StreamingThreshold: DefaultWASMStreamingThreshold,
		MaxModuleSize:      DefaultWASMMaxModuleSize,
		MaxCacheEntries:    DefaultWASMMaxCacheEntries,
	}
}

// ModuleLoader defines the interface for loading WASM modules.
type ModuleLoader interface {
	// LoadModule loads a WASM module from the specified location.
	LoadModule(ctx context.Context, bucket, key string) ([]byte, error)
}

// DefaultModuleLoader is a placeholder module loader that returns an error.
// In production, this should be replaced with an implementation that
// loads modules from S3 or another storage backend.
type DefaultModuleLoader struct{}

// LoadModule implements ModuleLoader.
func (l *DefaultModuleLoader) LoadModule(ctx context.Context, bucket, key string) ([]byte, error) {
	return nil, fmt.Errorf("module loader not configured: cannot load %s/%s", bucket, key)
}

// NewWASMRuntime creates a new WASM runtime with the given configuration.
func NewWASMRuntime(ctx context.Context, config *WASMRuntimeConfig) (*WASMRuntime, error) {
	if config == nil {
		config = DefaultWASMRuntimeConfig()
	}

	// Create wazero runtime with memory limits
	// Calculate memory pages safely (1 page = 64KB, so MB * 16 = pages)
	// Limit to a safe maximum to prevent overflow
	memoryPages := config.MaxMemoryMB * WASMPagesPerMB
	if memoryPages > WASMMaxMemoryPages {
		memoryPages = WASMMaxMemoryPages
	}

	runtimeConfig := wazero.NewRuntimeConfig().
		WithMemoryLimitPages(uint32(memoryPages)) //nolint:gosec // memoryPages is bounded above

	runtime := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

	// Instantiate WASI for I/O operations
	_, err := wasi_snapshot_preview1.Instantiate(ctx, runtime)
	if err != nil {
		if closeErr := runtime.Close(ctx); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close runtime after WASI instantiation failure")
		}

		return nil, fmt.Errorf("failed to instantiate WASI: %w", err)
	}

	wasmRuntime := &WASMRuntime{
		runtime:      runtime,
		moduleCache:  make(map[string]wazero.CompiledModule),
		cacheOrder:   make([]string, 0, config.MaxCacheEntries),
		moduleLoader: &DefaultModuleLoader{},
		config:       config,
	}

	log.Info().
		Int("max_memory_mb", config.MaxMemoryMB).
		Dur("max_execution_time", config.MaxExecutionTime).
		Int64("max_input_size", config.MaxInputSize).
		Bool("caching_enabled", config.EnableCaching).
		Msg("WASM runtime initialized")

	return wasmRuntime, nil
}

// SetModuleLoader sets the module loader for loading WASM modules.
func (w *WASMRuntime) SetModuleLoader(loader ModuleLoader) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.moduleLoader = loader
}

// Transform executes a WASM transformation on the input data.
func (w *WASMRuntime) Transform(
	ctx context.Context,
	config *WASMConfig,
	input io.Reader,
	metadata map[string]string,
) (io.Reader, map[string]string, error) {
	startTime := time.Now()
	moduleKey := fmt.Sprintf("%s/%s", config.ModuleBucket, config.ModuleKey)

	if w.closed.Load() {
		wasmTransformationsTotal.WithLabelValues("error", moduleKey).Inc()

		return nil, nil, errors.New("WASM runtime is closed")
	}

	// Validate configuration
	if err := w.validateConfig(config); err != nil {
		wasmTransformationsTotal.WithLabelValues("error", moduleKey).Inc()

		return nil, nil, err
	}

	// Track active executions for metrics
	w.metricsActive.Add(1)
	wasmActiveExecutions.Inc()

	defer func() {
		w.metricsActive.Add(-1)
		wasmActiveExecutions.Dec()
	}()

	// Create execution context with timeout
	execCtx, cancel := context.WithTimeout(ctx, w.config.MaxExecutionTime)
	defer cancel()

	// Read input with size limit
	inputData, err := w.readInputWithLimit(input)
	if err != nil {
		wasmTransformationsTotal.WithLabelValues("error", moduleKey).Inc()

		return nil, nil, err
	}

	// Track input bytes
	wasmInputBytes.Add(float64(len(inputData)))

	// Log when input exceeds streaming threshold (currently processed in-memory)
	// Future optimization: implement true streaming for large inputs
	if int64(len(inputData)) > w.config.StreamingThreshold {
		log.Debug().
			Int("input_size", len(inputData)).
			Int64("streaming_threshold", w.config.StreamingThreshold).
			Msg("Processing large input for WASM transformation")
	}

	// Load and compile module
	compiledModule, err := w.getOrCompileModule(execCtx, config.ModuleBucket, config.ModuleKey)
	if err != nil {
		wasmTransformationsTotal.WithLabelValues("error", moduleKey).Inc()

		return nil, nil, fmt.Errorf("failed to load WASM module: %w", err)
	}

	// Execute transformation
	outputData, outputMetadata, err := w.executeTransformation(
		execCtx,
		compiledModule,
		config.FunctionName,
		inputData,
		metadata,
	)
	if err != nil {
		wasmTransformationsTotal.WithLabelValues("error", moduleKey).Inc()

		return nil, nil, err
	}

	// Track metrics
	duration := time.Since(startTime)
	wasmTransformationsTotal.WithLabelValues("success", moduleKey).Inc()
	wasmTransformationDuration.WithLabelValues(moduleKey).Observe(duration.Seconds())
	wasmOutputBytes.Add(float64(len(outputData)))

	return bytes.NewReader(outputData), outputMetadata, nil
}

// validateConfig validates the WASM configuration.
func (w *WASMRuntime) validateConfig(config *WASMConfig) error {
	if config == nil {
		return errors.New("WASM configuration is nil")
	}

	if config.ModuleBucket == "" {
		return errors.New("WASM module bucket is required")
	}

	if config.ModuleKey == "" {
		return errors.New("WASM module key is required")
	}

	if config.FunctionName == "" {
		return errors.New("WASM function name is required")
	}

	// Apply memory limit from config if specified
	if config.MemoryLimit > 0 && config.MemoryLimit > w.config.MaxMemoryMB {
		return fmt.Errorf(
			"requested memory limit %d MB exceeds maximum %d MB",
			config.MemoryLimit,
			w.config.MaxMemoryMB,
		)
	}

	return nil
}

// readInputWithLimit reads input data with size limit enforcement.
func (w *WASMRuntime) readInputWithLimit(input io.Reader) ([]byte, error) {
	limitedReader := io.LimitReader(input, w.config.MaxInputSize+1)

	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}

	if int64(len(data)) > w.config.MaxInputSize {
		return nil, fmt.Errorf(
			"input size %d bytes exceeds maximum %d bytes",
			len(data),
			w.config.MaxInputSize,
		)
	}

	return data, nil
}

// getOrCompileModule retrieves a compiled module from cache or compiles it.
// Uses singleflight to prevent concurrent compilation of the same module.
func (w *WASMRuntime) getOrCompileModule(
	ctx context.Context,
	bucket, key string,
) (wazero.CompiledModule, error) {
	cacheKey := fmt.Sprintf("%s/%s", bucket, key)

	// Check context before proceeding
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Check cache first (with write lock to atomically update LRU order)
	if w.config.EnableCaching {
		w.mu.Lock()
		if cached, ok := w.moduleCache[cacheKey]; ok {
			// Update LRU order atomically with cache access
			w.updateCacheOrderLocked(cacheKey)
			w.mu.Unlock()
			wasmModuleCacheHits.Inc()

			log.Debug().
				Str("bucket", bucket).
				Str("key", key).
				Msg("Using cached WASM module")

			return cached, nil
		}
		w.mu.Unlock()
		wasmModuleCacheMisses.Inc()
	}

	// Use singleflight to prevent concurrent compilation of the same module
	result, err, _ := w.compileGroup.Do(cacheKey, func() (interface{}, error) {
		// Double-check cache after acquiring singleflight (another goroutine may have cached it)
		if w.config.EnableCaching {
			w.mu.RLock()
			if cached, ok := w.moduleCache[cacheKey]; ok {
				w.mu.RUnlock()

				return cached, nil
			}
			w.mu.RUnlock()
		}

		// Load module bytes
		w.mu.RLock()
		loader := w.moduleLoader
		w.mu.RUnlock()

		moduleBytes, err := loader.LoadModule(ctx, bucket, key)
		if err != nil {
			return nil, fmt.Errorf("failed to load module: %w", err)
		}

		// Validate module size
		if int64(len(moduleBytes)) > w.config.MaxModuleSize {
			return nil, fmt.Errorf(
				"WASM module size %d bytes exceeds maximum %d bytes",
				len(moduleBytes),
				w.config.MaxModuleSize,
			)
		}

		// Compile module
		compiled, err := w.runtime.CompileModule(ctx, moduleBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to compile WASM module: %w", err)
		}

		// Cache the compiled module with eviction if needed
		if w.config.EnableCaching {
			w.mu.Lock()
			// evictIfNeeded uses context.Background() internally to ensure cleanup
			// completes even if the request context is cancelled
			w.evictIfNeeded() //nolint:contextcheck // intentionally uses background context for cleanup
			w.moduleCache[cacheKey] = compiled
			w.cacheOrder = append(w.cacheOrder, cacheKey)
			wasmModuleCacheSize.Set(float64(len(w.moduleCache)))
			w.mu.Unlock()

			log.Debug().
				Str("bucket", bucket).
				Str("key", key).
				Int("cache_size", len(w.moduleCache)).
				Msg("Cached compiled WASM module")
		}

		return compiled, nil
	})

	if err != nil {
		return nil, err
	}

	// Safe type assertion with error handling
	compiled, ok := result.(wazero.CompiledModule)
	if !ok {
		return nil, errors.New("unexpected type from singleflight: expected CompiledModule")
	}

	return compiled, nil
}

// updateCacheOrderLocked moves the accessed key to the end of the LRU order.
// Must be called with w.mu held.
func (w *WASMRuntime) updateCacheOrderLocked(cacheKey string) {
	// Find and remove the key from its current position
	found := false

	for i, key := range w.cacheOrder {
		if key == cacheKey {
			w.cacheOrder = append(w.cacheOrder[:i], w.cacheOrder[i+1:]...)
			found = true

			break
		}
	}

	// Only add to the end if the key was found (avoid duplicates from missing keys)
	if found {
		w.cacheOrder = append(w.cacheOrder, cacheKey)
	}
}

// evictIfNeeded removes the least recently used module if cache is full.
// Must be called with w.mu held.
// Uses context.Background() for cleanup to ensure eviction completes even if
// the request context is cancelled.
func (w *WASMRuntime) evictIfNeeded() {
	// Use background context for cleanup operations
	cleanupCtx := context.Background()

	for len(w.moduleCache) >= w.config.MaxCacheEntries && len(w.cacheOrder) > 0 {
		// Evict the oldest (least recently used) entry
		oldestKey := w.cacheOrder[0]
		w.cacheOrder = w.cacheOrder[1:]

		if module, ok := w.moduleCache[oldestKey]; ok {
			if err := module.Close(cleanupCtx); err != nil {
				log.Warn().Err(err).Str("module", oldestKey).Msg("Failed to close evicted WASM module")
			}

			delete(w.moduleCache, oldestKey)
			wasmModuleCacheEvictions.Inc()

			log.Debug().
				Str("module", oldestKey).
				Int("cache_size", len(w.moduleCache)).
				Msg("Evicted WASM module from cache")
		}
	}
}

// executeTransformation runs the WASM transformation function.
// Note: metadata parameter is reserved for future use to pass object metadata to WASM modules.
func (w *WASMRuntime) executeTransformation(
	ctx context.Context,
	compiledModule wazero.CompiledModule,
	functionName string,
	inputData []byte,
	_ map[string]string, // metadata reserved for future use
) ([]byte, map[string]string, error) {
	startTime := time.Now()

	// Create module configuration with memory limits
	moduleConfig := wazero.NewModuleConfig().
		WithStartFunctions().
		WithStdin(bytes.NewReader(inputData)).
		WithStdout(io.Discard).
		WithStderr(io.Discard)

	// Instantiate the module
	instance, err := w.runtime.InstantiateModule(ctx, compiledModule, moduleConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to instantiate WASM module: %w", err)
	}

	defer func() {
		if closeErr := instance.Close(ctx); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close WASM module instance")
		}
	}()

	// Get the transformation function
	transformFunc := instance.ExportedFunction(functionName)
	if transformFunc == nil {
		return nil, nil, fmt.Errorf("function '%s' not found in WASM module", functionName)
	}

	// Allocate memory for input
	allocFunc := instance.ExportedFunction("allocate")
	freeFunc := instance.ExportedFunction("deallocate")

	if allocFunc == nil || freeFunc == nil {
		// Fall back to simple execution without memory management
		return w.executeSimpleTransformation(ctx, instance, transformFunc, inputData)
	}

	// Execute with proper memory management
	return w.executeWithMemoryManagement(
		ctx,
		instance,
		transformFunc,
		allocFunc,
		freeFunc,
		inputData,
		startTime,
	)
}

// safeUint64ToUint32 safely converts a uint64 to uint32, returning an error if overflow would occur.
func safeUint64ToUint32(value uint64) (uint32, error) {
	if value > uint64(^uint32(0)) {
		return 0, fmt.Errorf("value %d exceeds uint32 maximum", value)
	}

	return uint32(value), nil
}

// executeSimpleTransformation executes a simple transformation without
// explicit memory management (for modules that handle their own memory).
func (w *WASMRuntime) executeSimpleTransformation(
	ctx context.Context,
	instance api.Module,
	transformFunc api.Function,
	inputData []byte,
) ([]byte, map[string]string, error) {
	// Call the function with input size
	results, err := transformFunc.Call(ctx, uint64(len(inputData)))
	if err != nil {
		return nil, nil, fmt.Errorf("WASM function execution failed: %w", err)
	}

	if len(results) < 2 {
		return nil, nil, errors.New("WASM function returned insufficient results")
	}

	// Results should be [output_ptr, output_len] (WASM32 uses 32-bit pointers)
	outputPtr, err := safeUint64ToUint32(results[0])
	if err != nil {
		return nil, nil, fmt.Errorf("invalid output pointer: %w", err)
	}

	outputLen, err := safeUint64ToUint32(results[1])
	if err != nil {
		return nil, nil, fmt.Errorf("invalid output length: %w", err)
	}

	// Validate output size
	if int64(outputLen) > w.config.MaxOutputSize {
		return nil, nil, fmt.Errorf(
			"output size %d bytes exceeds maximum %d bytes",
			outputLen,
			w.config.MaxOutputSize,
		)
	}

	// Read output from WASM memory
	memory := instance.Memory()
	if memory == nil {
		return nil, nil, errors.New("WASM module has no exported memory")
	}

	outputData, ok := memory.Read(outputPtr, outputLen)
	if !ok {
		return nil, nil, errors.New("failed to read output from WASM memory")
	}

	// Make a copy since memory may be invalidated
	output := make([]byte, len(outputData))
	copy(output, outputData)

	return output, nil, nil
}

// executeWithMemoryManagement executes a transformation with explicit
// memory allocation and deallocation.
func (w *WASMRuntime) executeWithMemoryManagement(
	ctx context.Context,
	instance api.Module,
	transformFunc api.Function,
	allocFunc api.Function,
	freeFunc api.Function,
	inputData []byte,
	startTime time.Time,
) ([]byte, map[string]string, error) {
	memory := instance.Memory()
	if memory == nil {
		return nil, nil, errors.New("WASM module has no exported memory")
	}

	// Allocate memory for input
	inputLen := uint64(len(inputData))
	allocResults, err := allocFunc.Call(ctx, inputLen)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to allocate input memory: %w", err)
	}

	inputPtr, err := safeUint64ToUint32(allocResults[0])
	if err != nil {
		return nil, nil, fmt.Errorf("invalid input pointer from allocator: %w", err)
	}

	defer func() {
		// Free input memory
		if _, freeErr := freeFunc.Call(ctx, uint64(inputPtr), inputLen); freeErr != nil {
			log.Warn().Err(freeErr).Msg("Failed to free WASM input memory")
		}
	}()

	// Write input to WASM memory
	if !memory.Write(inputPtr, inputData) {
		return nil, nil, errors.New("failed to write input to WASM memory")
	}

	// Call the transformation function
	results, err := transformFunc.Call(ctx, uint64(inputPtr), inputLen)
	if err != nil {
		return nil, nil, fmt.Errorf("WASM function execution failed: %w", err)
	}

	if len(results) < 2 {
		return nil, nil, errors.New("WASM function returned insufficient results")
	}

	// Results should be [output_ptr, output_len] (WASM32 uses 32-bit pointers)
	outputPtr, err := safeUint64ToUint32(results[0])
	if err != nil {
		return nil, nil, fmt.Errorf("invalid output pointer: %w", err)
	}

	outputLen, err := safeUint64ToUint32(results[1])
	if err != nil {
		return nil, nil, fmt.Errorf("invalid output length: %w", err)
	}

	// Validate output size
	if int64(outputLen) > w.config.MaxOutputSize {
		return nil, nil, fmt.Errorf(
			"output size %d bytes exceeds maximum %d bytes",
			outputLen,
			w.config.MaxOutputSize,
		)
	}

	// Read output from WASM memory
	outputData, ok := memory.Read(outputPtr, outputLen)
	if !ok {
		return nil, nil, errors.New("failed to read output from WASM memory")
	}

	// Make a copy since memory may be invalidated
	output := make([]byte, len(outputData))
	copy(output, outputData)

	// Free output memory
	if _, err := freeFunc.Call(ctx, uint64(outputPtr), uint64(outputLen)); err != nil {
		log.Warn().Err(err).Msg("Failed to free WASM output memory")
	}

	duration := time.Since(startTime)

	log.Debug().
		Int("input_size", len(inputData)).
		Int("output_size", len(output)).
		Dur("duration", duration).
		Msg("WASM transformation completed")

	return output, nil, nil
}

// ClearCache clears the compiled module cache, closing all cached modules.
func (w *WASMRuntime) ClearCache(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Close all cached compiled modules before clearing
	for key, module := range w.moduleCache {
		if err := module.Close(ctx); err != nil {
			log.Warn().Err(err).Str("module", key).Msg("Failed to close cached WASM module during cache clear")
		}
	}

	w.moduleCache = make(map[string]wazero.CompiledModule)
	w.cacheOrder = make([]string, 0, w.config.MaxCacheEntries)
	wasmModuleCacheSize.Set(0)

	log.Info().Msg("WASM module cache cleared")
}

// GetActiveExecutions returns the number of active WASM executions.
func (w *WASMRuntime) GetActiveExecutions() int64 {
	return w.metricsActive.Load()
}

// Close closes the WASM runtime and releases all resources.
func (w *WASMRuntime) Close(ctx context.Context) error {
	if !w.closed.CompareAndSwap(false, true) {
		return nil // Already closed
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Close all cached compiled modules
	for key, module := range w.moduleCache {
		if err := module.Close(ctx); err != nil {
			log.Warn().Err(err).Str("module", key).Msg("Failed to close cached WASM module")
		}
	}

	w.moduleCache = nil
	w.cacheOrder = nil

	// Close the runtime
	if err := w.runtime.Close(ctx); err != nil {
		return fmt.Errorf("failed to close WASM runtime: %w", err)
	}

	log.Info().Msg("WASM runtime closed")

	return nil
}

// S3ModuleLoader loads WASM modules from S3 storage.
// This is a placeholder interface that should be implemented
// with actual S3 client integration.
type S3ModuleLoader struct {
	// GetObject is a function that retrieves an object from S3
	GetObject func(ctx context.Context, bucket, key string) (io.ReadCloser, error)
}

// LoadModule implements ModuleLoader for S3 storage.
func (l *S3ModuleLoader) LoadModule(ctx context.Context, bucket, key string) ([]byte, error) {
	if l.GetObject == nil {
		return nil, errors.New("S3 GetObject function not configured")
	}

	reader, err := l.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get WASM module from S3: %w", err)
	}

	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close S3 object reader")
		}
	}()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read WASM module: %w", err)
	}

	log.Debug().
		Str("bucket", bucket).
		Str("key", key).
		Int("size", len(data)).
		Msg("Loaded WASM module from S3")

	return data, nil
}

// InMemoryModuleLoader loads WASM modules from an in-memory store.
// Useful for testing and development.
type InMemoryModuleLoader struct {
	modules map[string][]byte
	mu      sync.RWMutex
}

// NewInMemoryModuleLoader creates a new in-memory module loader.
func NewInMemoryModuleLoader() *InMemoryModuleLoader {
	return &InMemoryModuleLoader{
		modules: make(map[string][]byte),
	}
}

// AddModule adds a WASM module to the in-memory store.
func (l *InMemoryModuleLoader) AddModule(bucket, key string, data []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()

	moduleKey := fmt.Sprintf("%s/%s", bucket, key)
	l.modules[moduleKey] = data
}

// LoadModule implements ModuleLoader for in-memory storage.
func (l *InMemoryModuleLoader) LoadModule(ctx context.Context, bucket, key string) ([]byte, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	moduleKey := fmt.Sprintf("%s/%s", bucket, key)
	data, ok := l.modules[moduleKey]
	if !ok {
		return nil, fmt.Errorf("WASM module not found: %s/%s", bucket, key)
	}

	// Return a copy to prevent modification
	result := make([]byte, len(data))
	copy(result, data)

	return result, nil
}
