#!/bin/bash
#
# NVIDIA NIM Examples
#
# Shell examples for AI inference on stored objects

set -e

# Configuration
S3_ENDPOINT="${NEBULAIO_ENDPOINT:-http://localhost:9000}"
ADMIN_ENDPOINT="${NEBULAIO_ADMIN_ENDPOINT:-http://localhost:9001}"
ACCESS_KEY="${NEBULAIO_ACCESS_KEY:-minioadmin}"
SECRET_KEY="${NEBULAIO_SECRET_KEY:-minioadmin}"
NVIDIA_API_KEY="${NVIDIA_API_KEY:-}"

# Auth
AUTH="${ACCESS_KEY}:${SECRET_KEY}"

echo "NVIDIA NIM Inference Examples"
echo "============================="

if [ -z "$NVIDIA_API_KEY" ]; then
    echo ""
    echo "WARNING: NVIDIA_API_KEY not set."
    echo "Get your API key from https://build.nvidia.com"
    echo "Examples will show expected request format."
    echo ""
fi

# Setup test data
echo "1. Setting up test data..."
BUCKET="nim-shell-demo"

# Create bucket
aws --endpoint-url "$S3_ENDPOINT" s3 mb "s3://$BUCKET" 2>/dev/null || true

# Upload test document
cat << 'EOF' | aws --endpoint-url "$S3_ENDPOINT" s3 cp - "s3://$BUCKET/documents/article.txt"
Machine Learning in Production: Best Practices

Deploying machine learning models to production requires careful consideration
of multiple factors including model serving, monitoring, and maintenance.

Key considerations:
1. Model Serving: Choose between real-time inference or batch processing
2. Monitoring: Track model performance metrics and data drift
3. Version Control: Maintain model versions and enable rollbacks
4. Testing: Implement A/B testing for model updates
5. Scaling: Use horizontal scaling for high-throughput requirements

Production ML systems should prioritize reliability and observability.
Implement comprehensive logging and alerting to catch issues early.
EOF

echo "   Created test document"

# Text inference request
echo ""
echo "2. Text Inference (Document Summary):"
echo "   Request:"
cat << EOF
curl -X POST "$ADMIN_ENDPOINT/api/v1/nim/infer" \\
  -H "Authorization: Bearer $AUTH" \\
  -H "X-NIM-API-Key: \$NVIDIA_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "bucket": "$BUCKET",
    "key": "documents/article.txt",
    "model": "meta/llama-3.1-8b-instruct",
    "prompt": "Summarize this document in 3 bullet points.",
    "max_tokens": 200
  }'
EOF

if [ -n "$NVIDIA_API_KEY" ]; then
    echo ""
    echo "   Response:"
    curl -s -X POST "$ADMIN_ENDPOINT/api/v1/nim/infer" \
        -H "Authorization: Bearer $AUTH" \
        -H "X-NIM-API-Key: $NVIDIA_API_KEY" \
        -H "Content-Type: application/json" \
        -d "{
            \"bucket\": \"$BUCKET\",
            \"key\": \"documents/article.txt\",
            \"model\": \"meta/llama-3.1-8b-instruct\",
            \"prompt\": \"Summarize this document in 3 bullet points.\",
            \"max_tokens\": 200
        }" | jq .
fi

# Streaming inference
echo ""
echo "3. Streaming Inference:"
echo "   Request:"
cat << EOF
curl -N -X POST "$ADMIN_ENDPOINT/api/v1/nim/infer" \\
  -H "Authorization: Bearer $AUTH" \\
  -H "X-NIM-API-Key: \$NVIDIA_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "bucket": "$BUCKET",
    "key": "documents/article.txt",
    "model": "meta/llama-3.1-8b-instruct",
    "prompt": "What are the key considerations mentioned?",
    "stream": true
  }'
EOF

# Text embedding
echo ""
echo "4. Text Embeddings:"
echo "   Request:"
cat << EOF
curl -X POST "$ADMIN_ENDPOINT/api/v1/nim/infer" \\
  -H "Authorization: Bearer $AUTH" \\
  -H "X-NIM-API-Key: \$NVIDIA_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "bucket": "$BUCKET",
    "key": "documents/article.txt",
    "model": "nvidia/nv-embed-v2",
    "task": "embedding"
  }'
EOF

# Vision inference (example with placeholder)
echo ""
echo "5. Vision Inference (Object Detection):"
echo "   Request (requires image in bucket):"
cat << EOF
curl -X POST "$ADMIN_ENDPOINT/api/v1/nim/infer" \\
  -H "Authorization: Bearer $AUTH" \\
  -H "X-NIM-API-Key: \$NVIDIA_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "bucket": "$BUCKET",
    "key": "images/photo.jpg",
    "model": "nvidia/grounding-dino",
    "task": "detection",
    "labels": ["person", "car", "dog"]
  }'
EOF

# Audio transcription (example with placeholder)
echo ""
echo "6. Audio Transcription:"
echo "   Request (requires audio in bucket):"
cat << EOF
curl -X POST "$ADMIN_ENDPOINT/api/v1/nim/infer" \\
  -H "Authorization: Bearer $AUTH" \\
  -H "X-NIM-API-Key: \$NVIDIA_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "bucket": "$BUCKET",
    "key": "audio/recording.mp3",
    "model": "nvidia/parakeet-ctc-1.1b",
    "task": "transcription"
  }'
EOF

# List available models
echo ""
echo "7. Available NIM Models:"
echo "   LLM:"
echo "     - meta/llama-3.1-8b-instruct"
echo "     - meta/llama-3.1-70b-instruct"
echo "     - mistralai/mixtral-8x7b-instruct"
echo "     - microsoft/phi-3-medium-128k-instruct"
echo ""
echo "   Vision:"
echo "     - nvidia/grounding-dino"
echo "     - nvidia/clip"
echo "     - nvidia/segment-anything"
echo ""
echo "   Audio:"
echo "     - nvidia/parakeet-ctc-1.1b"
echo "     - nvidia/riva-asr"
echo ""
echo "   Embedding:"
echo "     - nvidia/nv-embed-v2"
echo "     - nvidia/embed-qa-4"

echo ""
echo "Done!"
echo ""
echo "Test Bucket: $BUCKET"
echo "Admin Endpoint: $ADMIN_ENDPOINT"
