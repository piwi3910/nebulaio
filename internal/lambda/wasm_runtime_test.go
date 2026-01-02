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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, runtime)

			defer func() {
				require.NoError(t, runtime.Close(ctx))
			}()

			assert.False(t, runtime.closed.Load(), "Runtime should not be closed after creation")
		})
	}
}

// TestDefaultWASMRuntimeConfig tests the default configuration.
func TestDefaultWASMRuntimeConfig(t *testing.T) {
	config := DefaultWASMRuntimeConfig()

	assert.Equal(t, 256, config.MaxMemoryMB)
	assert.Equal(t, 30*time.Second, config.MaxExecutionTime)
	assert.Equal(t, int64(100*1024*1024), config.MaxInputSize)
	assert.Equal(t, int64(100*1024*1024), config.MaxOutputSize)
	assert.True(t, config.EnableCaching)
	assert.Equal(t, int64(10*1024*1024), config.StreamingThreshold)
}

// TestWASMRuntimeClose tests closing the runtime.
func TestWASMRuntimeClose(t *testing.T) {
	ctx := context.Background()

	runtime, err := NewWASMRuntime(ctx, nil)
	require.NoError(t, err)

	// Close should succeed
	require.NoError(t, runtime.Close(ctx))

	// Close again should be idempotent
	require.NoError(t, runtime.Close(ctx))

	// Runtime should be marked as closed
	assert.True(t, runtime.closed.Load(), "Runtime should be marked as closed")
}

// TestWASMRuntimeTransformClosed tests transformation on a closed runtime.
func TestWASMRuntimeTransformClosed(t *testing.T) {
	ctx := context.Background()

	runtime, err := NewWASMRuntime(ctx, nil)
	require.NoError(t, err)

	require.NoError(t, runtime.Close(ctx))

	config := &WASMConfig{
		ModuleBucket: "test-bucket",
		ModuleKey:    "test.wasm",
		FunctionName: "transform",
	}

	_, _, err = runtime.Transform(ctx, config, strings.NewReader("test"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

// TestWASMRuntimeValidateConfig tests configuration validation.
func TestWASMRuntimeValidateConfig(t *testing.T) {
	ctx := context.Background()

	runtime, err := NewWASMRuntime(ctx, nil)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, runtime.Close(ctx))
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
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, strings.ToLower(err.Error()), tt.errContains)
			} else {
				require.NoError(t, err)
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
	require.NoError(t, err)

	defer func() {
		require.NoError(t, runtime.Close(ctx))
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

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "exceeds maximum")
			} else {
				require.NoError(t, err)
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
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, moduleData, data)
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
	errCh := make(chan error, numReaders)

	for range numReaders {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := range numModules {
				bucket := "bucket"
				key := string(rune('a' + i))

				data, err := loader.LoadModule(ctx, bucket, key)
				if err != nil {
					errCh <- err

					return
				}

				if len(data) != 100 {
					errCh <- errors.New("expected data length 100")

					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)

	// Check for any errors from goroutines
	for err := range errCh {
		require.NoError(t, err)
	}
}

// TestDefaultModuleLoader tests the default module loader.
func TestDefaultModuleLoader(t *testing.T) {
	loader := &DefaultModuleLoader{}
	ctx := context.Background()

	_, err := loader.LoadModule(ctx, "bucket", "key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
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
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, data)
			}
		})
	}
}

// TestWASMRuntimeSetModuleLoader tests setting a module loader.
func TestWASMRuntimeSetModuleLoader(t *testing.T) {
	ctx := context.Background()

	runtime, err := NewWASMRuntime(ctx, nil)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, runtime.Close(ctx))
	}()

	loader := NewInMemoryModuleLoader()
	runtime.SetModuleLoader(loader)

	// Verify the loader was set by trying to load a non-existent module
	_, err = runtime.getOrCompileModule(ctx, "bucket", "key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestWASMRuntimeClearCache tests clearing the module cache.
func TestWASMRuntimeClearCache(t *testing.T) {
	ctx := context.Background()

	runtime, err := NewWASMRuntime(ctx, nil)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, runtime.Close(ctx))
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

	assert.Equal(t, 0, afterClearLen, "Expected empty cache after clear")

	// Verify it started empty (sanity check)
	if initialLen != 0 {
		t.Log("Note: Cache had entries before clear")
	}
}

// TestWASMRuntimeGetActiveExecutions tests the active executions counter.
func TestWASMRuntimeGetActiveExecutions(t *testing.T) {
	ctx := context.Background()

	runtime, err := NewWASMRuntime(ctx, nil)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, runtime.Close(ctx))
	}()

	// Initially should be 0
	assert.Equal(t, int64(0), runtime.GetActiveExecutions())

	// Simulate incrementing (normally done during Transform)
	runtime.metricsActive.Add(1)
	assert.Equal(t, int64(1), runtime.GetActiveExecutions())

	runtime.metricsActive.Add(-1)
	assert.Equal(t, int64(0), runtime.GetActiveExecutions())
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
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, svc)
			assert.NotNil(t, svc.wasmRuntime, "Expected WASM runtime to be initialized")

			require.NoError(t, svc.Close(ctx))
		})
	}
}

// TestObjectLambdaServiceSetWASMModuleLoader tests setting the WASM module loader.
func TestObjectLambdaServiceSetWASMModuleLoader(t *testing.T) {
	ctx := context.Background()

	t.Run("without WASM runtime", func(t *testing.T) {
		svc := NewObjectLambdaService()

		err := svc.SetWASMModuleLoader(NewInMemoryModuleLoader())
		require.Error(t, err)
	})

	t.Run("with WASM runtime", func(t *testing.T) {
		svc, err := NewObjectLambdaServiceWithWASM(ctx, nil)
		require.NoError(t, err)

		defer func() {
			require.NoError(t, svc.Close(ctx))
		}()

		require.NoError(t, svc.SetWASMModuleLoader(NewInMemoryModuleLoader()))
	})
}

// TestObjectLambdaServiceInitWASMRuntime tests lazy WASM runtime initialization.
func TestObjectLambdaServiceInitWASMRuntime(t *testing.T) {
	ctx := context.Background()
	svc := NewObjectLambdaService()

	// WASM runtime should be nil initially
	assert.Nil(t, svc.wasmRuntime, "Expected WASM runtime to be nil initially")

	// Initialize the runtime
	require.NoError(t, svc.initWASMRuntime(ctx))

	// Should be initialized now
	assert.NotNil(t, svc.wasmRuntime, "Expected WASM runtime to be initialized")

	// Calling again should be idempotent
	require.NoError(t, svc.initWASMRuntime(ctx))

	// Cleanup
	require.NoError(t, svc.Close(ctx))
}

// TestObjectLambdaServiceClose tests closing the service.
func TestObjectLambdaServiceClose(t *testing.T) {
	ctx := context.Background()

	t.Run("without WASM runtime", func(t *testing.T) {
		svc := NewObjectLambdaService()

		require.NoError(t, svc.Close(ctx))
	})

	t.Run("with WASM runtime", func(t *testing.T) {
		svc, err := NewObjectLambdaServiceWithWASM(ctx, nil)
		require.NoError(t, err)

		require.NoError(t, svc.Close(ctx))

		// Verify runtime is nil after close
		assert.Nil(t, svc.wasmRuntime, "Expected WASM runtime to be nil after close")
	})
}

// TestWASMTransformWithAccessPoint tests WASM transformation through the access point.
func TestWASMTransformWithAccessPoint(t *testing.T) {
	ctx := context.Background()
	svc := NewObjectLambdaService()

	defer func() {
		require.NoError(t, svc.Close(ctx))
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

	require.NoError(t, svc.CreateAccessPoint(cfg))

	// Transformation should fail because no module loader is configured
	_, _, err := svc.TransformObject(
		ctx,
		"wasm-ap",
		"test-object",
		strings.NewReader("test input"),
		map[string]string{},
		UserIdentity{},
	)

	// We expect an error because we don't have a real WASM module loaded
	require.Error(t, err, "Expected error when WASM module cannot be loaded")
}
