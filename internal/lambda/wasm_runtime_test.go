package lambda

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestNewWASMRuntime tests the creation of a new WASM runtime.
func TestNewWASMRuntime(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		config  *WASMRuntimeConfig
		name    string
		wantErr bool
	}{
		{
			name:    "nil config uses defaults",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "custom config",
			config:  &WASMRuntimeConfig{MaxMemoryMB: 128, MaxExecutionTime: 10 * time.Second},
			wantErr: false,
		},
		{
			name:    "default config",
			config:  DefaultWASMRuntimeConfig(),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime, err := NewWASMRuntime(ctx, tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewWASMRuntime() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if runtime != nil {
				defer func() {
					if closeErr := runtime.Close(ctx); closeErr != nil {
						t.Errorf("Failed to close runtime: %v", closeErr)
					}
				}()

				if runtime.closed.Load() {
					t.Error("Runtime should not be closed after creation")
				}
			}
		})
	}
}

// TestDefaultWASMRuntimeConfig tests the default configuration.
func TestDefaultWASMRuntimeConfig(t *testing.T) {
	config := DefaultWASMRuntimeConfig()

	if config.MaxMemoryMB != 256 {
		t.Errorf("Expected MaxMemoryMB = 256, got %d", config.MaxMemoryMB)
	}

	if config.MaxExecutionTime != 30*time.Second {
		t.Errorf("Expected MaxExecutionTime = 30s, got %v", config.MaxExecutionTime)
	}

	if config.MaxInputSize != 100*1024*1024 {
		t.Errorf("Expected MaxInputSize = 100MB, got %d", config.MaxInputSize)
	}

	if config.MaxOutputSize != 100*1024*1024 {
		t.Errorf("Expected MaxOutputSize = 100MB, got %d", config.MaxOutputSize)
	}

	if !config.EnableCaching {
		t.Error("Expected EnableCaching = true")
	}

	if config.StreamingThreshold != 10*1024*1024 {
		t.Errorf("Expected StreamingThreshold = 10MB, got %d", config.StreamingThreshold)
	}
}

// TestWASMRuntimeClose tests closing the runtime.
func TestWASMRuntimeClose(t *testing.T) {
	ctx := context.Background()

	runtime, err := NewWASMRuntime(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to create runtime: %v", err)
	}

	// Close should succeed
	err = runtime.Close(ctx)
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Close again should be idempotent
	err = runtime.Close(ctx)
	if err != nil {
		t.Errorf("Second Close() error = %v", err)
	}

	// Runtime should be marked as closed
	if !runtime.closed.Load() {
		t.Error("Runtime should be marked as closed")
	}
}

// TestWASMRuntimeTransformClosed tests transformation on a closed runtime.
func TestWASMRuntimeTransformClosed(t *testing.T) {
	ctx := context.Background()

	runtime, err := NewWASMRuntime(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to create runtime: %v", err)
	}

	err = runtime.Close(ctx)
	if err != nil {
		t.Fatalf("Failed to close runtime: %v", err)
	}

	config := &WASMConfig{
		ModuleBucket: "test-bucket",
		ModuleKey:    "test.wasm",
		FunctionName: "transform",
	}

	_, _, err = runtime.Transform(ctx, config, strings.NewReader("test"), nil)
	if err == nil {
		t.Error("Expected error when transforming on closed runtime")
	}

	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("Expected 'closed' in error message, got: %v", err)
	}
}

// TestWASMRuntimeValidateConfig tests configuration validation.
func TestWASMRuntimeValidateConfig(t *testing.T) {
	ctx := context.Background()

	runtime, err := NewWASMRuntime(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to create runtime: %v", err)
	}

	defer func() {
		if closeErr := runtime.Close(ctx); closeErr != nil {
			t.Errorf("Failed to close runtime: %v", closeErr)
		}
	}()

	tests := []struct {
		config      *WASMConfig
		name        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "nil config",
			config:      nil,
			wantErr:     true,
			errContains: "nil",
		},
		{
			name:        "missing module bucket",
			config:      &WASMConfig{ModuleKey: "test.wasm", FunctionName: "transform"},
			wantErr:     true,
			errContains: "bucket",
		},
		{
			name:        "missing module key",
			config:      &WASMConfig{ModuleBucket: "bucket", FunctionName: "transform"},
			wantErr:     true,
			errContains: "key",
		},
		{
			name:        "missing function name",
			config:      &WASMConfig{ModuleBucket: "bucket", ModuleKey: "test.wasm"},
			wantErr:     true,
			errContains: "function",
		},
		{
			name: "memory limit exceeds max",
			config: &WASMConfig{
				ModuleBucket: "bucket",
				ModuleKey:    "test.wasm",
				FunctionName: "transform",
				MemoryLimit:  1024, // 1GB exceeds default 256MB
			},
			wantErr:     true,
			errContains: "memory",
		},
		{
			name: "valid config",
			config: &WASMConfig{
				ModuleBucket: "bucket",
				ModuleKey:    "test.wasm",
				FunctionName: "transform",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runtime.validateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if tt.wantErr && tt.errContains != "" {
				if !strings.Contains(strings.ToLower(err.Error()), tt.errContains) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errContains, err)
				}
			}
		})
	}
}

// TestWASMRuntimeInputSizeLimit tests input size limit enforcement.
func TestWASMRuntimeInputSizeLimit(t *testing.T) {
	ctx := context.Background()

	config := &WASMRuntimeConfig{
		MaxMemoryMB:      256,
		MaxExecutionTime: 30 * time.Second,
		MaxInputSize:     1024, // 1KB limit for testing
		MaxOutputSize:    1024,
		EnableCaching:    true,
	}

	runtime, err := NewWASMRuntime(ctx, config)
	if err != nil {
		t.Fatalf("Failed to create runtime: %v", err)
	}

	defer func() {
		if closeErr := runtime.Close(ctx); closeErr != nil {
			t.Errorf("Failed to close runtime: %v", closeErr)
		}
	}()

	tests := []struct {
		name      string
		inputSize int
		wantErr   bool
	}{
		{
			name:      "input within limit",
			inputSize: 512,
			wantErr:   false,
		},
		{
			name:      "input at limit",
			inputSize: 1024,
			wantErr:   false,
		},
		{
			name:      "input exceeds limit",
			inputSize: 2048,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := bytes.Repeat([]byte("A"), tt.inputSize)
			_, err := runtime.readInputWithLimit(bytes.NewReader(input))

			if (err != nil) != tt.wantErr {
				t.Errorf("readInputWithLimit() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && err != nil {
				if !strings.Contains(err.Error(), "exceeds maximum") {
					t.Errorf("Expected 'exceeds maximum' in error, got: %v", err)
				}
			}
		})
	}
}

// TestInMemoryModuleLoader tests the in-memory module loader.
func TestInMemoryModuleLoader(t *testing.T) {
	loader := NewInMemoryModuleLoader()
	ctx := context.Background()

	// Add a module
	moduleData := []byte("test wasm module data")
	loader.AddModule("test-bucket", "test.wasm", moduleData)

	tests := []struct {
		name    string
		bucket  string
		key     string
		wantErr bool
	}{
		{
			name:    "existing module",
			bucket:  "test-bucket",
			key:     "test.wasm",
			wantErr: false,
		},
		{
			name:    "non-existent module",
			bucket:  "other-bucket",
			key:     "other.wasm",
			wantErr: true,
		},
		{
			name:    "wrong bucket",
			bucket:  "wrong-bucket",
			key:     "test.wasm",
			wantErr: true,
		},
		{
			name:    "wrong key",
			bucket:  "test-bucket",
			key:     "wrong.wasm",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := loader.LoadModule(ctx, tt.bucket, tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadModule() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if !tt.wantErr {
				if !bytes.Equal(data, moduleData) {
					t.Error("Loaded module data does not match")
				}
			}
		})
	}
}

// TestInMemoryModuleLoaderConcurrency tests concurrent access to the loader.
func TestInMemoryModuleLoaderConcurrency(t *testing.T) {
	loader := NewInMemoryModuleLoader()
	ctx := context.Background()

	const numModules = 10
	const numReaders = 100

	// Add modules concurrently
	var wg sync.WaitGroup

	for i := range numModules {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()

			bucket := "bucket"
			key := string(rune('a' + idx))
			data := bytes.Repeat([]byte{byte(idx)}, 100)
			loader.AddModule(bucket, key, data)
		}(i)
	}

	wg.Wait()

	// Read modules concurrently
	for range numReaders {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := range numModules {
				bucket := "bucket"
				key := string(rune('a' + i))

				data, err := loader.LoadModule(ctx, bucket, key)
				if err != nil {
					t.Errorf("LoadModule failed: %v", err)

					return
				}

				if len(data) != 100 {
					t.Errorf("Expected data length 100, got %d", len(data))
				}
			}
		}()
	}

	wg.Wait()
}

// TestDefaultModuleLoader tests the default module loader.
func TestDefaultModuleLoader(t *testing.T) {
	loader := &DefaultModuleLoader{}
	ctx := context.Background()

	_, err := loader.LoadModule(ctx, "bucket", "key")
	if err == nil {
		t.Error("Expected error from default module loader")
	}

	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("Expected 'not configured' in error, got: %v", err)
	}
}

// TestS3ModuleLoader tests the S3 module loader.
func TestS3ModuleLoader(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		loader  *S3ModuleLoader
		name    string
		wantErr bool
	}{
		{
			name:    "nil GetObject function",
			loader:  &S3ModuleLoader{GetObject: nil},
			wantErr: true,
		},
		{
			name: "GetObject returns error",
			loader: &S3ModuleLoader{
				GetObject: func(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
					return nil, errors.New("S3 error")
				},
			},
			wantErr: true,
		},
		{
			name: "GetObject succeeds",
			loader: &S3ModuleLoader{
				GetObject: func(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
					return io.NopCloser(bytes.NewReader([]byte("wasm module"))), nil
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.loader.LoadModule(ctx, "bucket", "key")
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadModule() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if !tt.wantErr && data == nil {
				t.Error("Expected data to be returned")
			}
		})
	}
}

// TestWASMRuntimeSetModuleLoader tests setting a module loader.
func TestWASMRuntimeSetModuleLoader(t *testing.T) {
	ctx := context.Background()

	runtime, err := NewWASMRuntime(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to create runtime: %v", err)
	}

	defer func() {
		if closeErr := runtime.Close(ctx); closeErr != nil {
			t.Errorf("Failed to close runtime: %v", closeErr)
		}
	}()

	loader := NewInMemoryModuleLoader()
	runtime.SetModuleLoader(loader)

	// Verify the loader was set by trying to load a non-existent module
	_, err = runtime.getOrCompileModule(ctx, "bucket", "key")
	if err == nil {
		t.Error("Expected error when loading non-existent module")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' in error, got: %v", err)
	}
}

// TestWASMRuntimeClearCache tests clearing the module cache.
func TestWASMRuntimeClearCache(t *testing.T) {
	ctx := context.Background()

	runtime, err := NewWASMRuntime(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to create runtime: %v", err)
	}

	defer func() {
		if closeErr := runtime.Close(ctx); closeErr != nil {
			t.Errorf("Failed to close runtime: %v", closeErr)
		}
	}()

	// Add a mock entry to the cache (normally this would be a compiled module)
	runtime.mu.Lock()
	// We can't easily add a real compiled module, so we verify the map is cleared
	initialLen := len(runtime.moduleCache)
	runtime.mu.Unlock()

	// Clear the cache
	runtime.ClearCache(ctx)

	runtime.mu.RLock()
	afterClearLen := len(runtime.moduleCache)
	runtime.mu.RUnlock()

	if afterClearLen != 0 {
		t.Errorf("Expected empty cache after clear, got %d entries", afterClearLen)
	}

	// Verify it started empty (sanity check)
	if initialLen != 0 {
		t.Log("Note: Cache had entries before clear")
	}
}

// TestWASMRuntimeGetActiveExecutions tests the active executions counter.
func TestWASMRuntimeGetActiveExecutions(t *testing.T) {
	ctx := context.Background()

	runtime, err := NewWASMRuntime(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to create runtime: %v", err)
	}

	defer func() {
		if closeErr := runtime.Close(ctx); closeErr != nil {
			t.Errorf("Failed to close runtime: %v", closeErr)
		}
	}()

	// Initially should be 0
	if runtime.GetActiveExecutions() != 0 {
		t.Errorf("Expected 0 active executions, got %d", runtime.GetActiveExecutions())
	}

	// Simulate incrementing (normally done during Transform)
	runtime.metricsActive.Add(1)

	if runtime.GetActiveExecutions() != 1 {
		t.Errorf("Expected 1 active execution, got %d", runtime.GetActiveExecutions())
	}

	runtime.metricsActive.Add(-1)

	if runtime.GetActiveExecutions() != 0 {
		t.Errorf("Expected 0 active executions after decrement, got %d", runtime.GetActiveExecutions())
	}
}

// TestObjectLambdaServiceWithWASM tests creating the service with WASM support.
func TestObjectLambdaServiceWithWASM(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		config  *WASMRuntimeConfig
		name    string
		wantErr bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "custom config",
			config:  &WASMRuntimeConfig{MaxMemoryMB: 128},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := NewObjectLambdaServiceWithWASM(ctx, tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewObjectLambdaServiceWithWASM() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if svc != nil {
				if svc.wasmRuntime == nil {
					t.Error("Expected WASM runtime to be initialized")
				}

				err = svc.Close(ctx)
				if err != nil {
					t.Errorf("Close() error = %v", err)
				}
			}
		})
	}
}

// TestObjectLambdaServiceSetWASMModuleLoader tests setting the WASM module loader.
func TestObjectLambdaServiceSetWASMModuleLoader(t *testing.T) {
	ctx := context.Background()

	t.Run("without WASM runtime", func(t *testing.T) {
		svc := NewObjectLambdaService()

		err := svc.SetWASMModuleLoader(NewInMemoryModuleLoader())
		if err == nil {
			t.Error("Expected error when WASM runtime not initialized")
		}
	})

	t.Run("with WASM runtime", func(t *testing.T) {
		svc, err := NewObjectLambdaServiceWithWASM(ctx, nil)
		if err != nil {
			t.Fatalf("Failed to create service: %v", err)
		}

		defer func() {
			if closeErr := svc.Close(ctx); closeErr != nil {
				t.Errorf("Close() error = %v", closeErr)
			}
		}()

		err = svc.SetWASMModuleLoader(NewInMemoryModuleLoader())
		if err != nil {
			t.Errorf("SetWASMModuleLoader() error = %v", err)
		}
	})
}

// TestObjectLambdaServiceInitWASMRuntime tests lazy WASM runtime initialization.
func TestObjectLambdaServiceInitWASMRuntime(t *testing.T) {
	ctx := context.Background()
	svc := NewObjectLambdaService()

	// WASM runtime should be nil initially
	if svc.wasmRuntime != nil {
		t.Error("Expected WASM runtime to be nil initially")
	}

	// Initialize the runtime
	err := svc.initWASMRuntime(ctx)
	if err != nil {
		t.Errorf("initWASMRuntime() error = %v", err)
	}

	// Should be initialized now
	if svc.wasmRuntime == nil {
		t.Error("Expected WASM runtime to be initialized")
	}

	// Calling again should be idempotent
	err = svc.initWASMRuntime(ctx)
	if err != nil {
		t.Errorf("Second initWASMRuntime() error = %v", err)
	}

	// Cleanup
	err = svc.Close(ctx)
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// TestObjectLambdaServiceClose tests closing the service.
func TestObjectLambdaServiceClose(t *testing.T) {
	ctx := context.Background()

	t.Run("without WASM runtime", func(t *testing.T) {
		svc := NewObjectLambdaService()

		err := svc.Close(ctx)
		if err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	t.Run("with WASM runtime", func(t *testing.T) {
		svc, err := NewObjectLambdaServiceWithWASM(ctx, nil)
		if err != nil {
			t.Fatalf("Failed to create service: %v", err)
		}

		err = svc.Close(ctx)
		if err != nil {
			t.Errorf("Close() error = %v", err)
		}

		// Verify runtime is nil after close
		if svc.wasmRuntime != nil {
			t.Error("Expected WASM runtime to be nil after close")
		}
	})
}

// TestWASMTransformWithAccessPoint tests WASM transformation through the access point.
func TestWASMTransformWithAccessPoint(t *testing.T) {
	ctx := context.Background()
	svc := NewObjectLambdaService()

	defer func() {
		if closeErr := svc.Close(ctx); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	}()

	cfg := &AccessPointConfig{
		Name:                  "wasm-ap",
		SupportingAccessPoint: "supporting",
		TransformationConfigurations: []TransformationConfig{
			{
				Actions: []string{"GetObject"},
				ContentTransformation: ContentTransformation{
					Type: TransformWASM,
					WASMConfig: &WASMConfig{
						ModuleBucket: "wasm-bucket",
						ModuleKey:    "transform.wasm",
						FunctionName: "transform",
					},
				},
			},
		},
	}

	err := svc.CreateAccessPoint(cfg)
	if err != nil {
		t.Fatalf("CreateAccessPoint failed: %v", err)
	}

	// Transformation should fail because no module loader is configured
	_, _, err = svc.TransformObject(
		ctx,
		"wasm-ap",
		"test-object",
		strings.NewReader("test input"),
		map[string]string{},
		UserIdentity{},
	)

	// We expect an error because we don't have a real WASM module loaded
	if err == nil {
		t.Error("Expected error when WASM module cannot be loaded")
	}
}
