# NVIDIA NIM Integration

NebulaIO integrates with NVIDIA NIM (NVIDIA Inference Microservices) to run AI inference directly on stored objects without downloading them first.

## Overview

NVIDIA NIM integration provides:

- **Object Inference** - Run AI models on stored data
- **Multiple Model Types** - LLM, Vision, Audio, Multimodal, Embedding
- **Streaming Responses** - Real-time inference output
- **Caching** - Reduce repeated inference costs

## Requirements

### NVIDIA API Access

- NVIDIA API key from [build.nvidia.com](https://build.nvidia.com)
- Or self-hosted NIM deployment

### Supported Models

| Type | Examples |
|------|----------|
| LLM | Llama 3.1, Mixtral, Phi-3 |
| Vision | Grounding DINO, CLIP, SAM |
| Audio | Whisper, RIVA |
| Multimodal | LLaVA, CogVLM |
| Embedding | NV-Embed, E5 |

## Configuration

### Basic Setup

```yaml
nim:
  enabled: true
  api_key: nvapi-XXXXXXXXXXXX
  default_model: meta/llama-3.1-8b-instruct
```

### Advanced Configuration

```yaml
nim:
  enabled: true
  api_key: ${NIM_API_KEY}
  endpoint: https://integrate.api.nvidia.com/v1
  default_model: meta/llama-3.1-8b-instruct
  enable_streaming: true
  timeout: 300               # Request timeout in seconds
  max_retries: 3             # Retry failed requests
  cache_responses: true      # Cache inference results
  cache_ttl: 3600            # Cache TTL in seconds
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `NEBULAIO_NIM_ENABLED` | Enable NIM integration | `false` |
| `NEBULAIO_NIM_API_KEY` | NVIDIA API key | - |
| `NEBULAIO_NIM_DEFAULT_MODEL` | Default model | `meta/llama-3.1-8b-instruct` |

## Usage Examples

### Python SDK

```python
from nebulaio import NIMClient

client = NIMClient(
    endpoint="http://localhost:9000",
    nim_endpoint="https://integrate.api.nvidia.com/v1",
    api_key="nvapi-XXXX"
)

# Text inference on document
result = client.infer_text(
    bucket="documents",
    key="report.txt",
    prompt="Summarize this document",
    model="meta/llama-3.1-70b-instruct"
)

# Vision inference on image
result = client.infer_vision(
    bucket="images",
    key="photo.jpg",
    task="detection",
    model="nvidia/grounding-dino"
)

# Audio transcription
result = client.infer_audio(
    bucket="audio",
    key="meeting.mp3",
    model="nvidia/parakeet-ctc-1.1b"
)
```

### REST API

```bash
# Text inference
curl -X POST "http://localhost:9001/api/v1/nim/infer" \
  -H "Content-Type: application/json" \
  -d '{
    "bucket": "documents",
    "key": "report.txt",
    "model": "meta/llama-3.1-8b-instruct",
    "prompt": "Summarize this document in 3 sentences",
    "max_tokens": 200
  }'

# Vision inference
curl -X POST "http://localhost:9001/api/v1/nim/infer" \
  -H "Content-Type: application/json" \
  -d '{
    "bucket": "images",
    "key": "photo.jpg",
    "model": "nvidia/grounding-dino",
    "task": "detection",
    "labels": ["person", "car", "dog"]
  }'
```

### Streaming Inference

```python
# Stream LLM response
for chunk in client.infer_stream(
    bucket="documents",
    key="article.txt",
    prompt="Analyze this article",
    model="meta/llama-3.1-70b-instruct"
):
    print(chunk, end="", flush=True)
```

### Batch Inference

```python
# Process multiple objects
results = client.infer_batch(
    bucket="images",
    prefix="photos/",
    model="nvidia/clip",
    task="embedding"
)

for key, embedding in results.items():
    print(f"{key}: {len(embedding)} dimensions")
```

## Model Categories

### Large Language Models (LLM)

```python
# Chat completion
result = client.chat(
    bucket="docs",
    key="context.txt",
    messages=[
        {"role": "system", "content": "You are a helpful assistant."},
        {"role": "user", "content": "Based on the document, what are the key points?"}
    ],
    model="meta/llama-3.1-8b-instruct"
)
```

Supported models:

- `meta/llama-3.1-8b-instruct`
- `meta/llama-3.1-70b-instruct`
- `mistralai/mixtral-8x7b-instruct`
- `microsoft/phi-3-medium-128k-instruct`

### Vision Models

```python
# Object detection
result = client.detect_objects(
    bucket="images",
    key="photo.jpg",
    labels=["person", "car", "dog"],
    model="nvidia/grounding-dino"
)

# Image classification
result = client.classify(
    bucket="images",
    key="photo.jpg",
    model="nvidia/clip"
)

# Segmentation
result = client.segment(
    bucket="images",
    key="photo.jpg",
    model="nvidia/segment-anything"
)
```

### Audio Models

```python
# Speech-to-text
result = client.transcribe(
    bucket="audio",
    key="recording.mp3",
    model="nvidia/parakeet-ctc-1.1b"
)

# Speaker diarization
result = client.diarize(
    bucket="audio",
    key="meeting.wav",
    model="nvidia/riva-diarization"
)
```

### Embedding Models

```python
# Generate embeddings
embedding = client.embed(
    bucket="documents",
    key="article.txt",
    model="nvidia/nv-embed-v2"
)

# Semantic search
results = client.semantic_search(
    bucket="documents",
    query="machine learning applications",
    model="nvidia/nv-embed-v2"
)
```

## Caching

### Enable Response Caching

```yaml
nim:
  enabled: true
  cache_responses: true
  cache_ttl: 3600  # 1 hour
```

### Cache Behavior

- Same object + same model + same prompt = cached result
- Object modification invalidates cache
- Manual cache invalidation via API

```bash
# Clear cache for specific object
curl -X DELETE "http://localhost:9001/api/v1/nim/cache?bucket=docs&key=file.txt"
```

## Self-Hosted NIM

### Deploy Local NIM

```yaml
nim:
  enabled: true
  endpoint: http://nim-server:8000/v1
  # No API key needed for self-hosted
```

### Docker Compose

```yaml
services:
  nebulaio:
    image: nebulaio:latest
    environment:
      NEBULAIO_NIM_ENABLED: "true"
      NEBULAIO_NIM_ENDPOINT: "http://nim:8000/v1"

  nim:
    image: nvcr.io/nim/meta/llama-3.1-8b-instruct:latest
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: 1
              capabilities: [gpu]
```

## Integration with Other Features

### With Apache Iceberg

```python
# Analyze Iceberg table data
result = client.analyze_table(
    bucket="warehouse",
    table="analytics.events",
    prompt="What are the trends in this data?",
    model="meta/llama-3.1-70b-instruct"
)
```

### With MCP Server

AI agents can use NIM through MCP:

```json
{
  "method": "tools/call",
  "params": {
    "name": "nim_inference",
    "arguments": {
      "bucket": "images",
      "key": "photo.jpg",
      "model": "nvidia/grounding-dino",
      "task": "detection"
    }
  }
}
```

## Performance

### Latency

| Model Type | Typical Latency |
|------------|-----------------|
| Embedding | 50-200ms |
| Vision | 100-500ms |
| LLM (short) | 200-1000ms |
| LLM (long) | 1-10s |

### Optimization

```yaml
nim:
  enabled: true
  # Use streaming for long responses
  enable_streaming: true
  # Increase timeout for large models
  timeout: 600
```

## Troubleshooting

### Common Issues

1. **API key invalid**
   - Verify key at [build.nvidia.com](https://build.nvidia.com)
   - Check for trailing spaces

2. **Model not found**
   - Verify model ID spelling
   - Check model availability in your region

3. **Timeout errors**
   - Increase timeout setting
   - Use streaming for long responses

### Logs

Enable debug logging:

```yaml
log_level: debug
```

Look for logs with `nim` tag.

## API Reference

### Inference Endpoint

```
POST /api/v1/nim/infer
Content-Type: application/json

{
  "bucket": "string",
  "key": "string",
  "model": "string",
  "prompt": "string",
  "max_tokens": 100,
  "temperature": 0.7,
  "stream": false
}
```

### Available Tasks

| Task | Description |
|------|-------------|
| `chat` | Conversational AI |
| `completion` | Text completion |
| `embedding` | Generate embeddings |
| `detection` | Object detection |
| `classification` | Image classification |
| `segmentation` | Image segmentation |
| `transcription` | Speech-to-text |

## See Also

- [MCP Server](mcp-server.md) - AI agent integration
- [Apache Iceberg](iceberg.md) - Structured data analysis
- [GPUDirect Storage](gpudirect.md) - Fast data access
