#!/bin/bash
# NebulaIO Security Features Shell Examples
#
# This script demonstrates security features using curl commands:
# - Access Analytics and Anomaly Detection
# - Encryption Key Rotation
# - mTLS Certificate Management
# - Distributed Tracing

set -e

# Configuration
ADMIN_ENDPOINT="${NEBULAIO_ADMIN_ENDPOINT:-http://localhost:9001}"
TOKEN="${NEBULAIO_TOKEN:-}"

# Headers
AUTH_HEADER=""
if [ -n "$TOKEN" ]; then
    AUTH_HEADER="Authorization: Bearer $TOKEN"
fi

echo "=========================================="
echo "NebulaIO Security Features Examples"
echo "=========================================="
echo "Admin Endpoint: $ADMIN_ENDPOINT"
echo ""

# =============================================================================
# Access Analytics
# =============================================================================
echo "--- Access Analytics ---"
echo ""

# Get high severity anomalies from the last 7 days
echo "1. Getting high severity anomalies..."
curl -s -X GET "$ADMIN_ENDPOINT/api/v1/analytics/anomalies?severity=HIGH&limit=10" \
    -H "$AUTH_HEADER" \
    -H "Content-Type: application/json" | jq '.anomalies[:3]' 2>/dev/null || echo "Error fetching anomalies"
echo ""

# Get analytics statistics
echo "2. Getting analytics statistics..."
curl -s -X GET "$ADMIN_ENDPOINT/api/v1/analytics/stats" \
    -H "$AUTH_HEADER" | jq '.' 2>/dev/null || echo "Error fetching stats"
echo ""

# Get user baseline
echo "3. Getting user baseline for 'admin'..."
curl -s -X GET "$ADMIN_ENDPOINT/api/v1/analytics/baselines/admin" \
    -H "$AUTH_HEADER" | jq '.' 2>/dev/null || echo "Error fetching baseline"
echo ""

# Add custom detection rule
echo "4. Adding custom detection rule..."
curl -s -X POST "$ADMIN_ENDPOINT/api/v1/analytics/rules" \
    -H "$AUTH_HEADER" \
    -H "Content-Type: application/json" \
    -d '{
        "id": "sensitive-bucket-access",
        "name": "Sensitive Bucket Access",
        "type": "SENSITIVE_ACCESS",
        "enabled": true,
        "severity": "HIGH",
        "threshold": 1,
        "window": "1h",
        "conditions": [
            {"field": "bucket", "operator": "in", "value": ["secrets", "credentials"]}
        ]
    }' | jq '.' 2>/dev/null || echo "Error adding rule"
echo ""

# Generate analytics report
echo "5. Generating analytics report..."
START_TIME=$(date -u -v-7d +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date -u -d "7 days ago" +"%Y-%m-%dT%H:%M:%SZ")
END_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
curl -s -X POST "$ADMIN_ENDPOINT/api/v1/analytics/reports" \
    -H "$AUTH_HEADER" \
    -H "Content-Type: application/json" \
    -d "{
        \"start_time\": \"$START_TIME\",
        \"end_time\": \"$END_TIME\",
        \"format\": \"json\"
    }" | jq '.summary' 2>/dev/null || echo "Error generating report"
echo ""

# =============================================================================
# Encryption Key Rotation
# =============================================================================
echo "--- Encryption Key Rotation ---"
echo ""

# Create a new encryption key
echo "1. Creating encryption key..."
KEY_RESPONSE=$(curl -s -X POST "$ADMIN_ENDPOINT/api/v1/keys" \
    -H "$AUTH_HEADER" \
    -H "Content-Type: application/json" \
    -d '{
        "type": "BUCKET",
        "algorithm": "AES-256-GCM",
        "alias": "example-bucket-key",
        "metadata": {"environment": "example"}
    }')
echo "$KEY_RESPONSE" | jq '.' 2>/dev/null || echo "Error creating key"
KEY_ID=$(echo "$KEY_RESPONSE" | jq -r '.id' 2>/dev/null)
echo ""

# List keys
echo "2. Listing data keys..."
curl -s -X GET "$ADMIN_ENDPOINT/api/v1/keys?type=DATA" \
    -H "$AUTH_HEADER" | jq '.keys[:3]' 2>/dev/null || echo "Error listing keys"
echo ""

# Rotate the key
if [ -n "$KEY_ID" ] && [ "$KEY_ID" != "null" ]; then
    echo "3. Rotating key $KEY_ID..."
    curl -s -X POST "$ADMIN_ENDPOINT/api/v1/keys/$KEY_ID/rotate" \
        -H "$AUTH_HEADER" \
        -H "Content-Type: application/json" \
        -d '{"trigger": "manual"}' | jq '.' 2>/dev/null || echo "Error rotating key"
    echo ""

    # Get rotation history
    echo "4. Getting rotation history..."
    curl -s -X GET "$ADMIN_ENDPOINT/api/v1/keys/$KEY_ID/rotations" \
        -H "$AUTH_HEADER" | jq '.events' 2>/dev/null || echo "Error fetching history"
    echo ""

    # Start re-encryption job
    echo "5. Starting re-encryption job..."
    curl -s -X POST "$ADMIN_ENDPOINT/api/v1/keys/$KEY_ID/reencrypt" \
        -H "$AUTH_HEADER" \
        -H "Content-Type: application/json" \
        -d '{
            "old_version": 1,
            "new_version": 2,
            "buckets": ["my-bucket"],
            "concurrency": 10
        }' | jq '.' 2>/dev/null || echo "Error starting job"
    echo ""
fi

# =============================================================================
# mTLS Certificate Management
# =============================================================================
echo "--- mTLS Certificate Management ---"
echo ""

# Initialize CA
echo "1. Initializing Certificate Authority..."
curl -s -X POST "$ADMIN_ENDPOINT/api/v1/mtls/ca/init" \
    -H "$AUTH_HEADER" \
    -H "Content-Type: application/json" \
    -d '{"common_name": "NebulaIO Example CA"}' | jq '.' 2>/dev/null || echo "CA may already exist"
echo ""

# Get CA certificate
echo "2. Getting CA certificate..."
curl -s -X GET "$ADMIN_ENDPOINT/api/v1/mtls/ca/certificate" \
    -H "$AUTH_HEADER" | head -5
echo "..."
echo ""

# Issue server certificate
echo "3. Issuing server certificate..."
CERT_RESPONSE=$(curl -s -X POST "$ADMIN_ENDPOINT/api/v1/mtls/certificates" \
    -H "$AUTH_HEADER" \
    -H "Content-Type: application/json" \
    -d '{
        "type": "SERVER",
        "common_name": "example-node.cluster.local",
        "dns_names": ["example-node", "example-node.cluster.local"],
        "ip_addresses": ["10.0.0.100"]
    }')
echo "$CERT_RESPONSE" | jq '.' 2>/dev/null || echo "Error issuing certificate"
CERT_ID=$(echo "$CERT_RESPONSE" | jq -r '.id' 2>/dev/null)
echo ""

# List certificates
echo "4. Listing certificates..."
curl -s -X GET "$ADMIN_ENDPOINT/api/v1/mtls/certificates" \
    -H "$AUTH_HEADER" | jq '.certificates[:5]' 2>/dev/null || echo "Error listing certificates"
echo ""

# Get CRL
echo "5. Getting Certificate Revocation List..."
curl -s -X GET "$ADMIN_ENDPOINT/api/v1/mtls/crl" \
    -H "$AUTH_HEADER" | head -5
echo "..."
echo ""

# =============================================================================
# Distributed Tracing
# =============================================================================
echo "--- Distributed Tracing ---"
echo ""

# Get tracing configuration
echo "1. Getting tracing configuration..."
curl -s -X GET "$ADMIN_ENDPOINT/api/v1/tracing/config" \
    -H "$AUTH_HEADER" | jq '.' 2>/dev/null || echo "Error fetching config"
echo ""

# Update tracing configuration
echo "2. Updating tracing configuration..."
curl -s -X PUT "$ADMIN_ENDPOINT/api/v1/tracing/config" \
    -H "$AUTH_HEADER" \
    -H "Content-Type: application/json" \
    -d '{
        "enabled": true,
        "sampling": {"rate": 0.1},
        "exporter": {
            "type": "otlp",
            "endpoint": "http://otel-collector:4317"
        },
        "propagator": "w3c"
    }' | jq '.' 2>/dev/null || echo "Error updating config"
echo ""

# Get tracing statistics
echo "3. Getting tracing statistics..."
curl -s -X GET "$ADMIN_ENDPOINT/api/v1/tracing/stats" \
    -H "$AUTH_HEADER" | jq '.' 2>/dev/null || echo "Error fetching stats"
echo ""

# =============================================================================
# Integration Example: S3 Request with Tracing
# =============================================================================
echo "--- S3 Request with Tracing ---"
echo ""

# Make S3 request with trace context
TRACE_ID=$(uuidgen 2>/dev/null | tr -d '-' | cut -c1-32 || echo "00000000000000000000000000000001")
SPAN_ID=$(uuidgen 2>/dev/null | tr -d '-' | cut -c1-16 || echo "0000000000000001")

echo "Making S3 request with W3C Trace Context..."
echo "Trace ID: $TRACE_ID"
echo "Span ID: $SPAN_ID"

curl -s -X GET "http://localhost:9000/my-bucket" \
    -H "traceparent: 00-${TRACE_ID}-${SPAN_ID}-01" \
    -H "tracestate: nebulaio=example" \
    --aws-sigv4 "aws:amz:us-east-1:s3" \
    --user "ACCESS_KEY:SECRET_KEY" 2>/dev/null || echo "S3 request (example)"
echo ""

echo "=========================================="
echo "Security Features Examples Complete!"
echo "=========================================="
