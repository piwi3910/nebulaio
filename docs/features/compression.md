# Compression

NebulaIO provides transparent object compression to reduce storage costs and improve transfer speeds. Three algorithms are supported with automatic content-type detection to skip already-compressed files.

## Supported Algorithms

### Zstandard (zstd) - Recommended

Zstandard offers the best balance of compression ratio and speed, making it ideal for most workloads.

### LZ4

LZ4 prioritizes speed over compression ratio. Suitable for high-throughput workloads where CPU overhead must be minimized.

### Gzip

Gzip provides wide compatibility with existing tools and systems. Preferred for interoperability requirements.

## Algorithm Comparison

| Algorithm | Compression Ratio | Compression Speed | Decompression Speed | Best For |
| ----------- | ------------------- | ------------------- | --------------------- | ---------- |
| Zstandard | Excellent (3-5x) | Fast (500 MB/s) | Very Fast (1500 MB/s) | General purpose, default |
| LZ4 | Good (2-3x) | Very Fast (800 MB/s) | Extremely Fast (4000 MB/s) | Real-time workloads |
| Gzip | Good (3-4x) | Moderate (100 MB/s) | Fast (400 MB/s) | Legacy compatibility |

## Configuration Options

```yaml

compression:
  algorithm: zstd           # none, zstd, lz4, gzip
  level: 3                  # 1=fastest, 3=default, 9=best ratio
  min_size: 1024            # Minimum object size to compress (bytes)
  exclude_types:
    - image/jpeg
    - image/png
    - video/mp4
    - application/zip

```bash

### Compression Levels

NebulaIO uses an abstract level scale (1-9) that maps to algorithm-specific ranges:

| Level | Name | Description | Use Case |
| ------- | ------ | ------------- | ---------- |
| 1 | Fastest | Minimal compression, maximum speed | Real-time streaming |
| 3 | Default | Balanced speed and compression | General workloads |
| 9 | Best | Maximum compression, slower speed | Archival storage |

**Algorithm-Specific Level Mapping:**

| Abstract Level | Zstd (1-19) | LZ4 (1-16) | Gzip (1-9) |
| ---------------- | ------------- | ------------ | ------------ |
| 1 | 1 | 1 | 1 |
| 3 | 3 | 4 | 3 |
| 5 | 6 | 8 | 5 |
| 7 | 10 | 12 | 7 |
| 9 | 15 | 16 | 9 |

> **Note:** The abstract level provides a consistent interface across algorithms. NebulaIO automatically translates to the optimal algorithm-specific level. For advanced use cases, you can specify native levels directly using `level_native` in the configuration.

## Per-Bucket Compression Settings

Configure compression per bucket:

```bash

# Set bucket compression via CLI
nebulaio bucket set-compression my-bucket --algorithm zstd --level 5

# Disable compression for media bucket
nebulaio bucket set-compression media-bucket --algorithm none

```bash

### Recommended Settings by Bucket Type

| Bucket Type | Algorithm | Level | Min Size |
| ------------- | ----------- | ------- | ---------- |
| Log archives | zstd | 9 | 1KB |
| Hot data cache | lz4 | 1 | 4KB |
| Media storage | none | - | - |
| JSON/API data | zstd | 3 | 512B |
| Backup archives | zstd | 7 | 1KB |

## Content-Type Exclusions

NebulaIO automatically skips compression for already-compressed content types:

**Images**: `image/jpeg`, `image/png`, `image/gif`, `image/webp`

**Video**: `video/mp4`, `video/webm`, `video/mpeg`

**Audio**: `audio/mpeg`, `audio/mp4`, `audio/ogg`

**Archives**: `application/zip`, `application/gzip`, `application/x-bzip2`, `application/x-xz`, `application/x-7z-compressed`, `application/x-rar-compressed`

### Custom Exclusions

```yaml

compression:
  exclude_types:
    - application/x-custom-compressed
    - application/octet-stream

```bash

## Performance Benchmarks

### Compression Ratios by Data Type

| Data Type | Zstd Ratio | LZ4 Ratio | Gzip Ratio |
| ----------- | ------------ | ----------- | ------------ |
| JSON logs | 5.5x | 3.5x | 5.0x |
| CSV data | 4.5x | 3.1x | 4.2x |
| XML documents | 6.6x | 4.0x | 5.9x |
| Text files | 2.8x | 2.2x | 2.6x |
| Binary data | 1.3x | 1.2x | 1.3x |

### Throughput Benchmarks

| Algorithm | Level | Compress | Decompress | CPU Usage |
| ----------- | ------- | ---------- | ------------ | ----------- |
| Zstd | 1 | 650 MB/s | 1600 MB/s | Low |
| Zstd | 3 | 450 MB/s | 1500 MB/s | Medium |
| Zstd | 9 | 80 MB/s | 1400 MB/s | High |
| LZ4 | 1 | 850 MB/s | 4200 MB/s | Very Low |
| Gzip | 6 | 85 MB/s | 420 MB/s | High |

**Note:** Benchmarks performed on Intel Xeon 8375C, single thread

## Best Practices

### Algorithm Selection

1. **Use Zstandard by default**: Best balance of ratio and speed
2. **Choose LZ4 for real-time**: When latency is critical
3. **Use Gzip for compatibility**: When data must be readable by external tools

### Configuration Guidelines

1. **Set appropriate minimum size**: Objects under 1KB rarely benefit from compression
2. **Exclude pre-compressed content**: Always exclude media and archive formats
3. **Match level to workload**: Hot data (1-3), warm data (3-5), cold data (7-9)

### Monitor Compression Effectiveness

```bash

nebulaio admin compression-stats

# Output:
# Algorithm: zstd
# Objects compressed: 1,234,567
# Original size: 5.2 TB
# Compressed size: 1.8 TB
# Space saved: 3.4 TB (65%)

```

### Storage Cost Savings

| Data Type | Storage Saved | Annual Savings (per TB) |
| ----------- | --------------- | ------------------------- |
| Log data | 70-80% | $150-200 |
| JSON/API | 60-75% | $120-180 |
| Text files | 50-65% | $100-150 |

## Troubleshooting

### Low Compression Ratios

- Verify content is not already compressed
- Check if min_size is filtering small objects
- Review content-type detection accuracy

### High CPU Usage

- Lower compression level (1-3 instead of 7-9)
- Switch to LZ4 for high-throughput workloads
- Enable compression only for cold data buckets

### Decompression Errors

- Ensure compression metadata is intact
- Verify algorithm compatibility across nodes
- Check for corrupted objects in storage backend
