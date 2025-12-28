# Future Improvements - TODO Implementation PR

These issues should be created in GitHub after merging PR from branch `claude/complete-todos-tYr1f`.

## Enhancement Issues

### 1. Make MaxTransformSize configurable for Lambda transformers

**Labels:** enhancement, lambda
**Description:**
The `MaxTransformSize` constant in `internal/lambda/object_lambda.go` is currently hardcoded at 100MB. This should be made configurable via the server configuration to allow operators to tune memory usage based on their deployment requirements.

**Acceptance Criteria:**

- Add `max_transform_size` config option under lambda/object_lambda section
- Default to 100MB for backwards compatibility
- Validate configuration at startup
- Document the option in configuration docs

---

### 2. Implement WASM runtime for Object Lambda transformations

**Labels:** enhancement, lambda, feature
**Description:**
The WASM transformation type in Object Lambda is currently a stub (`internal/lambda/object_lambda.go:555`). Implement full WASM runtime support using wasmtime or wasmer.

**Acceptance Criteria:**

- Integrate wasmtime-go or wasmer-go runtime
- Support loading WASM modules from S3
- Implement memory limits and execution timeouts
- Add sandboxing for security
- Add tests and documentation

---

### 3. Add streaming compression support for large files

**Labels:** enhancement, performance, lambda
**Description:**
The current compression/decompression transformers buffer entire files in memory. For large files approaching MaxTransformSize, this can cause memory pressure. Consider adding streaming support for files that exceed a threshold.

**Acceptance Criteria:**

- Implement streaming gzip/zstd compression for large files
- Add threshold configuration for when to use streaming
- Maintain backwards compatibility with current behavior
- Add performance benchmarks

---

### 4. Add observability metrics for compression operations

**Labels:** enhancement, observability
**Description:**
Add Prometheus metrics to track compression/decompression operations in the Lambda transformers for better operational visibility.

**Metrics to add:**

- `nebulaio_lambda_compression_operations_total` (counter by algorithm, status)
- `nebulaio_lambda_compression_ratio` (histogram)
- `nebulaio_lambda_compression_duration_seconds` (histogram by algorithm)
- `nebulaio_lambda_compression_bytes_processed` (counter)

---

### 5. Add test cases for decompression bomb protection

**Labels:** testing, security
**Description:**
Add specific unit tests that verify the decompression bomb protection in `DecompressTransformer`. Create test fixtures with high-compression-ratio data to ensure the size limits are properly enforced.

**Acceptance Criteria:**

- Create test compressed data that would expand beyond MaxTransformSize
- Verify proper error is returned
- Test both gzip and zstd algorithms
- Document the security protection in code comments

---

## Remaining TODOs from Codebase

These are existing TODOs that were not addressed in this PR:

1. **Parquet writing implementation** (`internal/catalog/catalog.go:785`)
2. **DragonboatStore full implementation** (`internal/server/server.go:129`)
3. **RDMA transport implementation** (`internal/transport/rdma/transport.go` - multiple locations)
4. **Presigned URL tests** (`internal/api/s3/handler_test.go:2950`)
